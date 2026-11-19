// Package update keeps a standalone LabVory binary current with the public
// GitHub Releases: a Checker polls the releases API (graceful when no release
// exists yet) and an Installer downloads, checksum-verifies, and swaps in the
// new binary. Docker deployments skip the install path entirely — images are
// upgraded by pulling.
package update

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const (
	// DefaultAPIURL is the GitHub API root for the LabVory repository.
	DefaultAPIURL = "https://api.github.com/repos/aancw/labvory"
	// DefaultDownloadURL serves release assets (tag + asset name appended).
	DefaultDownloadURL = "https://github.com/aancw/labvory/releases/download"

	// checkEvery throttles release lookups; two per day is plenty for a
	// homelab tool and stays far inside the unauthenticated API budget.
	checkEvery = 12 * time.Hour

	// downloadLimit caps a release asset read so a bad mirror cannot fill
	// the disk.
	downloadLimit = 1 << 30
)

// State is the last observed update-check result, served verbatim by the API.
type State struct {
	Available   bool       `json:"available"`
	Current     string     `json:"current"`
	Latest      string     `json:"latest,omitempty"`
	URL         string     `json:"url,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CheckedAt   time.Time  `json:"checked_at"`
	LastError   string     `json:"last_error,omitempty"`
}

// githubRelease is the subset of the GitHub releases payload we consume.
// /releases/latest already excludes drafts and prereleases.
type githubRelease struct {
	TagName     string     `json:"tag_name"`
	HTMLURL     string     `json:"html_url"`
	PublishedAt *time.Time `json:"published_at"`
}

// Checker polls GitHub for newer releases and caches the result.
type Checker struct {
	// Current is the running version, e.g. "v0.2.1" (release builds stamp the
	// git tag) or "dev" (source builds never fetch).
	Current string
	// APIURL / DownloadURL default to the GitHub locations; overridable in tests.
	APIURL      string
	DownloadURL string
	Client      *http.Client
	Logger      *slog.Logger

	mu        sync.Mutex
	state     State
	lastCheck time.Time
	now       func() time.Time
}

// NewChecker builds a checker for the given running version.
func NewChecker(current string) *Checker {
	return &Checker{
		Current:     current,
		APIURL:      DefaultAPIURL,
		DownloadURL: DefaultDownloadURL,
		Client:      &http.Client{Timeout: 30 * time.Second},
		Logger:      slog.Default(),
		now:         time.Now,
	}
}

// State returns the cached result without fetching.
func (c *Checker) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// AssetName is the release archive name for the running platform.
func (c *Checker) AssetName(tag string) string {
	return fmt.Sprintf("labvory-%s-%s-%s.tar.gz", tag, runtime.GOOS, runtime.GOARCH)
}

// AssetURL is the browser-download URL for the platform archive.
func (c *Checker) AssetURL(tag string) string {
	return c.DownloadURL + "/" + tag + "/" + c.AssetName(tag)
}

// Check refreshes the cached state at most once per checkEvery and returns it.
// Failures are recorded in State.LastError and never panic or block callers.
func (c *Checker) Check(ctx context.Context) State {
	return c.check(ctx, false)
}

// CheckFresh bypasses the throttle — used before an install so the operator
// acts on current release data, not a half-day-old cache.
func (c *Checker) CheckFresh(ctx context.Context) State {
	return c.check(ctx, true)
}

func (c *Checker) check(ctx context.Context, force bool) State {
	c.mu.Lock()
	defer c.mu.Unlock()

	if isDevVersion(c.Current) {
		// Source builds have no comparable version: never fetch, never nag.
		c.state = State{Current: c.Current}
		return c.state
	}
	if !force && !c.lastCheck.IsZero() && c.now().Sub(c.lastCheck) < checkEvery {
		return c.state
	}

	c.state = c.fetch(ctx)
	c.state.Current = c.current()
	c.lastCheck = c.now()
	return c.state
}

// current normalizes the running version for output.
func (c *Checker) current() string {
	if c.Current == "" {
		return "unknown"
	}
	return c.Current
}

// fetch performs one releases/latest lookup. A 404 means the repository has
// no releases yet — an expected state, not an error.
func (c *Checker) fetch(ctx context.Context) State {
	url := c.APIURL + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return State{LastError: err.Error()}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "labvory-update-check")

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		c.Logger.Debug("update check failed", "error", err)
		return State{LastError: err.Error()}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		// No releases published yet — quiet, expected until the first tag.
		return State{}
	case resp.StatusCode == http.StatusForbidden:
		// Unauthenticated API rate limit exhausted; retry after the throttle.
		c.Logger.Warn("update check rate limited by GitHub")
		return State{LastError: "github api rate limited"}
	case resp.StatusCode != http.StatusOK:
		c.Logger.Warn("update check failed", "status", resp.StatusCode)
		return State{LastError: fmt.Sprintf("github api status %d", resp.StatusCode)}
	}

	var rel githubRelease
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err == nil {
		err = json.Unmarshal(body, &rel)
	}
	if err != nil {
		c.Logger.Warn("update check: parse releases payload failed", "error", err)
		return State{LastError: "could not parse release info"}
	}

	state := State{Latest: rel.TagName, URL: rel.HTMLURL, PublishedAt: rel.PublishedAt}
	if semver.Compare(normalize(c.Current), normalize(rel.TagName)) < 0 {
		state.Available = true
	}
	return state
}

// isDevVersion reports whether the running binary was built from source
// (no version stamped).
func isDevVersion(v string) bool {
	return v == "" || v == "dev"
}

// normalize adds the "v" prefix semver.Compare requires.
func normalize(v string) string {
	if v == "" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// ---------- checksum file parsing ----------

// ErrChecksum reports a checksum mismatch between sha256sums.txt and the
// downloaded archive.
var ErrChecksum = errors.New("checksum mismatch")

// expectedChecksum parses sha256sums content ("<hex>  <name>" lines) and
// returns the digest recorded for the given asset name.
func expectedChecksum(sums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != asset {
			continue
		}
		if _, err := hex.DecodeString(fields[0]); err != nil || len(fields[0]) != 64 {
			return "", fmt.Errorf("sha256sums.txt: malformed digest for %s", asset)
		}
		return strings.ToLower(fields[0]), nil
	}
	return "", fmt.Errorf("sha256sums.txt has no entry for %s", asset)
}

// parseContentLength extracts a Content-Length header, 0 when absent/invalid.
func parseContentLength(h string) int64 {
	n, err := strconv.ParseInt(h, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// writeDownload streams resp into path (bounded by downloadLimit) and returns
// the sha256 hex digest of what landed on disk.
func (c *Checker) writeDownload(resp *http.Response, path string) (string, error) {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	digest, err := copyDigest(io.LimitReader(resp.Body, downloadLimit), out)
	closeErr := out.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return digest, nil
}

// download fetches url into dir and returns the path plus its sha256 hex.
func (c *Checker) download(ctx context.Context, client *http.Client, url, dir, name string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "labvory-update-install")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download %s: status %d", name, resp.StatusCode)
	}
	path := dir + "/" + name
	digest, err := c.writeDownload(resp, path)
	if err != nil {
		return "", "", err
	}
	return path, digest, nil
}
