package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// installTimeout bounds the whole download-verify-swap sequence.
const installTimeout = 10 * time.Minute

// capabilityXattr is the Linux xattr set by `setcap cap_net_raw+ep`; a
// replaced binary keeps raw-socket access only if it is carried over.
const capabilityXattr = "security.capability"

// ErrNotUpdatable marks guards that make an in-place install impossible
// (source build, containerized runtime).
var ErrNotUpdatable = errors.New("installation cannot self-update")

// InstallResult reports a completed binary swap; the process restart is a
// separate step the handler triggers after answering.
type InstallResult struct {
	From string `json:"from"`
	To   string `json:"to"`
	Path string `json:"path"`
}

// Installer performs the admin-triggered self-update.
type Installer struct {
	Check   *Checker
	Client  *http.Client
	Restart func() // optional; injects the graceful shutdown + exec closure
	Logger  *slog.Logger
}

// NewInstaller builds an installer on top of a checker.
func NewInstaller(check *Checker) *Installer {
	return &Installer{
		Check:  check,
		Client: &http.Client{Timeout: installTimeout},
		Logger: slog.Default(),
	}
}

// TriggerRestart invokes the restart closure shortly after the HTTP response
// has been flushed, so the browser learns the result before the process
// image is replaced. No-op when no closure was injected (tests).
func (i *Installer) TriggerRestart() {
	if i.Restart == nil {
		return
	}
	go func() {
		time.Sleep(1200 * time.Millisecond)
		i.Restart()
	}()
}

// Install downloads the latest release archive for this platform, verifies it
// against sha256sums.txt, and swaps the running binary for the new one
// (keeping the previous binary as <exe>.old). The config file is never
// touched. Call TriggerRestart afterwards to apply the update.
func (i *Installer) Install(ctx context.Context) (InstallResult, error) {
	checker := i.Check
	if checker == nil || isDevVersion(checker.Current) {
		return InstallResult{}, fmt.Errorf("%w: source builds install updates by rebuilding", ErrNotUpdatable)
	}
	if runningInContainer() {
		return InstallResult{}, fmt.Errorf("%w: running in a container — upgrade the image instead", ErrNotUpdatable)
	}

	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	exe, err := os.Executable()
	if err != nil {
		return InstallResult{}, fmt.Errorf("locate running binary: %w", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return InstallResult{}, fmt.Errorf("resolve binary path: %w", err)
	}
	return i.installTo(ctx, exe)
}

// installTo performs the download-verify-extract-swap sequence against an
// explicit binary path (the running executable in production; a scratch file
// in tests). Release data is fetched fresh, bypassing the check throttle.
func (i *Installer) installTo(ctx context.Context, exe string) (InstallResult, error) {
	checker := i.Check
	state := checker.CheckFresh(ctx)
	if state.LastError != "" {
		return InstallResult{}, fmt.Errorf("update check failed: %s", state.LastError)
	}
	if !state.Available {
		if state.Latest == "" {
			return InstallResult{}, errors.New("no release published yet")
		}
		return InstallResult{}, errors.New("already up to date")
	}

	dir, err := os.MkdirTemp("", "labvory-update-")
	if err != nil {
		return InstallResult{}, err
	}
	defer os.RemoveAll(dir)

	asset := checker.AssetName(state.Latest)
	client := i.Client
	if client == nil {
		client = &http.Client{Timeout: installTimeout}
	}

	archivePath, digest, err := checker.download(ctx, client, checker.AssetURL(state.Latest), dir, asset)
	if err != nil {
		return InstallResult{}, fmt.Errorf("download %s: %w", asset, err)
	}
	sumsPath, _, err := checker.download(ctx, client, checker.DownloadURL+"/"+state.Latest+"/sha256sums.txt", dir, "sha256sums.txt")
	if err != nil {
		return InstallResult{}, fmt.Errorf("download sha256sums.txt: %w", err)
	}
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		return InstallResult{}, err
	}
	expected, err := expectedChecksum(sums, asset)
	if err != nil {
		return InstallResult{}, err
	}
	if digest != expected {
		return InstallResult{}, fmt.Errorf("%w: %s is %s, sha256sums.txt says %s", ErrChecksum, asset, digest, expected)
	}

	extracted, err := extractBinary(archivePath, filepath.Join(dir, "labvory.new"))
	if err != nil {
		return InstallResult{}, err
	}
	if err := copyCapability(exe, extracted); err != nil {
		return InstallResult{}, fmt.Errorf("preserve file capabilities: %w (update manually with setcap)", err)
	}

	if err := swapBinary(exe, extracted); err != nil {
		return InstallResult{}, err
	}
	i.Logger.Info("update installed, restarting", "from", checker.Current, "to", state.Latest, "binary", exe)
	return InstallResult{From: checker.Current, To: state.Latest, Path: exe}, nil
}

// runningInContainer detects the container runtimes the install path cannot
// serve: images are upgraded by pulling, not by swapping files.
func runningInContainer() bool {
	for _, marker := range []string{"/.dockerenv", "/.containerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

// extractBinary copies the labvory binary out of the release archive into
// dest. Only regular members whose base name matches are accepted, and the
// destination path is ours — archive entry names can never redirect writes.
func extractBinary(archivePath, dest string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", errors.New("release archive contains no labvory binary")
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != "labvory" {
			continue
		}
		mode := hdr.FileInfo().Mode()
		if mode&0o100 == 0 {
			mode |= 0o100 // owner must be able to exec what we swap in
		}
		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return "", err
		}
		n, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if n == 0 || (hdr.Size > 0 && n != hdr.Size) {
			return "", fmt.Errorf("extract %s: truncated (%d/%d bytes)", hdr.Name, n, hdr.Size)
		}
		return dest, nil
	}
}

// swapBinary replaces the running binary, keeping the previous one as
// <exe>.old for manual rollback. A failed second rename rolls the first back.
func swapBinary(exe, newPath string) error {
	old := exe + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("back up current binary: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		if restore := os.Rename(old, exe); restore != nil {
			return fmt.Errorf("install new binary: %v (rollback also failed: %v — previous binary kept as %s)", err, restore, old)
		}
		return fmt.Errorf("install new binary: %w", err)
	}
	return nil
}

// copyDigest streams r into w while hashing it.
func copyDigest(r io.Reader, w io.Writer) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
