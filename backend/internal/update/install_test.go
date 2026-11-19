package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// buildReleaseArchive produces the tar.gz the release workflow packages:
// a labvory-<tag>-<os>-<arch>/ directory holding the labvory binary.
func buildReleaseArchive(t *testing.T, tag, payload string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	dir := "labvory-" + tag + "-" + runtime.GOOS + "-" + runtime.GOARCH
	if err := tw.WriteHeader(&tar.Header{Name: dir + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: dir + "/labvory", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(payload)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// installFixture wires a checker and installer against a fake GitHub that
// publishes v0.2.0 with a one-file archive whose payload is newPayload.
func installFixture(t *testing.T, newPayload, sumsOverride string) (*Installer, string) {
	t.Helper()

	tag := "v0.2.0"
	checker := NewChecker("v0.1.0")
	archive := buildReleaseArchive(t, tag, newPayload)
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	asset := checker.AssetName(tag)
	sums := digest + "  " + asset + "\n"
	if sumsOverride != "" {
		sums = sumsOverride
	}

	fg := newFakeGitHub(t, tag, map[string][]byte{asset: archive, "sha256sums.txt": []byte(sums)})
	checker.APIURL = fg.URL + "/repos/aancw/labvory"
	checker.DownloadURL = fg.URL + "/releases/download"

	dir := t.TempDir()
	exe := filepath.Join(dir, "labvory")
	if err := os.WriteFile(exe, []byte("old-payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewInstaller(checker), exe
}

func TestInstallSwapsBinary(t *testing.T) {
	inst, exe := installFixture(t, "new-payload", "")

	res, err := inst.installTo(context.Background(), exe)
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if res.From != "v0.1.0" || res.To != "v0.2.0" {
		t.Fatalf("unexpected result: %+v", res)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "new-payload" {
		t.Fatalf("binary not replaced: %q, %v", got, err)
	}
	old, err := os.ReadFile(exe + ".old")
	if err != nil || string(old) != "old-payload" {
		t.Fatalf("backup missing or wrong: %q, %v", old, err)
	}
	info, err := os.Stat(exe)
	if err != nil || info.Mode()&0o100 == 0 {
		t.Fatalf("replacement not executable: %v, %v", info.Mode(), err)
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	// Right asset name, wrong digest: the mismatch — not a missing entry —
	// must be what trips verification.
	wrong := "abababababababababababababababababababababababababababababababab"
	checker := NewChecker("v0.1.0")
	inst, exe := installFixture(t, "new-payload", wrong+"  "+checker.AssetName("v0.2.0")+"\n")

	if _, err := inst.installTo(context.Background(), exe); !errors.Is(err, ErrChecksum) {
		t.Fatalf("expected checksum error, got %v", err)
	}
	// The running binary must be untouched when verification fails.
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "old-payload" {
		t.Fatalf("binary was disturbed on failed install: %q, %v", got, err)
	}
	if _, err := os.Stat(exe + ".old"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup should not exist yet: %v", err)
	}
}

func TestSwapBinaryRollsBack(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "labvory")
	if err := os.WriteFile(exe, []byte("old-payload"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A missing replacement forces the second rename to fail; the previous
	// binary must come back from the .old backup.
	if err := swapBinary(exe, filepath.Join(dir, "does-not-exist")); err == nil {
		t.Fatal("expected swap to fail")
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "old-payload" {
		t.Fatalf("rollback failed: %q, %v", got, err)
	}
}

func TestInstallNoReleaseAndUpToDate(t *testing.T) {
	fg := newFakeGitHub(t, "", nil)
	checker := fg.checker("v0.1.0")
	inst := NewInstaller(checker)
	exe := filepath.Join(t.TempDir(), "labvory")

	if _, err := inst.installTo(context.Background(), exe); err == nil || err.Error() != "no release published yet" {
		t.Fatalf("expected no-release error, got %v", err)
	}

	fg2 := newFakeGitHub(t, "v0.1.0", nil)
	inst2 := NewInstaller(fg2.checker("v0.1.0"))
	if _, err := inst2.installTo(context.Background(), exe); err == nil || err.Error() != "already up to date" {
		t.Fatalf("expected up-to-date error, got %v", err)
	}
}

func TestExtractBinaryRejectsArchiveWithoutBinary(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "labvory-v0.2.0-linux-amd64/LICENSE", Typeflag: tar.TypeReg, Size: 5, Mode: 0o644})
	_, _ = tw.Write([]byte("license"))
	_ = tw.Close()
	_ = gz.Close()

	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := extractBinary(archive, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("expected error for archive without labvory member")
	}
}
