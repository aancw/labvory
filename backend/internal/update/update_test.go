package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGitHub serves a releases/latest payload plus release assets, counting
// API hits so tests can assert on throttle behaviour.
type fakeGitHub struct {
	*httptest.Server
	apiHits atomic.Int64
	release string // tag_name served, or "" to answer 404
	assets  map[string][]byte
}

func newFakeGitHub(t *testing.T, release string, assets map[string][]byte) *fakeGitHub {
	t.Helper()
	fg := &fakeGitHub{release: release, assets: assets}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/aancw/labvory/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fg.apiHits.Add(1)
		if fg.release == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		pub := "2026-11-01T12:00:00Z"
		ts, err := time.Parse(time.RFC3339, pub)
		if err != nil {
			t.Fatalf("bad fixture time: %v", err)
		}
		_ = json.NewEncoder(w).Encode(githubRelease{
			TagName:     fg.release,
			HTMLURL:     "https://github.com/aancw/labvory/releases/tag/" + fg.release,
			PublishedAt: &ts,
		})
	})
	mux.HandleFunc("GET /releases/download/", func(w http.ResponseWriter, r *http.Request) {
		asset := r.URL.Path[len("/releases/download/v0.2.0/"):]
		data, ok := fg.assets[asset]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})
	fg.Server = httptest.NewServer(mux)
	t.Cleanup(fg.Server.Close)
	return fg
}

func (fg *fakeGitHub) checker(current string) *Checker {
	c := NewChecker(current)
	c.APIURL = fg.URL + "/repos/aancw/labvory"
	c.DownloadURL = fg.URL + "/releases/download"
	return c
}

func TestCheckDetectsNewerRelease(t *testing.T) {
	fg := newFakeGitHub(t, "v0.2.0", nil)
	c := fg.checker("v0.1.0")

	state := c.Check(context.Background())
	if !state.Available {
		t.Fatalf("expected available, got %+v", state)
	}
	if state.Latest != "v0.2.0" || state.Current != "v0.1.0" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.URL == "" || state.PublishedAt == nil {
		t.Fatalf("release metadata missing: %+v", state)
	}
	if state.LastError != "" {
		t.Fatalf("unexpected error: %s", state.LastError)
	}
}

func TestCheckQuietWhenNoReleases(t *testing.T) {
	// The repository has no releases yet (404): available=false and no error,
	// so the UI stays clean until the first tag is pushed.
	fg := newFakeGitHub(t, "", nil)
	c := fg.checker("v0.1.0")

	state := c.Check(context.Background())
	if state.Available || state.Latest != "" || state.LastError != "" {
		t.Fatalf("expected a quiet no-release state, got %+v", state)
	}
}

func TestCheckUpToDate(t *testing.T) {
	fg := newFakeGitHub(t, "v0.2.0", nil)
	c := fg.checker("v0.2.0")

	if state := c.Check(context.Background()); state.Available {
		t.Fatalf("same version must not be available: %+v", state)
	}
}

func TestCheckThrottle(t *testing.T) {
	fg := newFakeGitHub(t, "v9.0.0", nil)
	c := fg.checker("v0.1.0")

	c.Check(context.Background())
	c.Check(context.Background())
	if n := fg.apiHits.Load(); n != 1 {
		t.Fatalf("throttle broken: %d API hits after two checks", n)
	}
	c.CheckFresh(context.Background())
	if n := fg.apiHits.Load(); n != 2 {
		t.Fatalf("CheckFresh must bypass the throttle: %d hits", n)
	}
}

func TestCheckSkipsSourceBuilds(t *testing.T) {
	fg := newFakeGitHub(t, "v9.0.0", nil)
	c := fg.checker("dev")

	state := c.Check(context.Background())
	if state.Available || state.Latest != "" {
		t.Fatalf("dev builds must not fetch: %+v", state)
	}
	if n := fg.apiHits.Load(); n != 0 {
		t.Fatalf("dev build hit the API %d times", n)
	}
}

func TestExpectedChecksum(t *testing.T) {
	good := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	other := "ff40541d7eeb7a0e5a48e0e57b5f1a2a1e4a9f9a9a9a9a9a9a9a9a9a9a9a9a9a"
	sums := []byte(good + "  labvory-v0.2.0-linux-amd64.tar.gz\n" + other + "  other.tar.gz\n")
	got, err := expectedChecksum(sums, "labvory-v0.2.0-linux-amd64.tar.gz")
	if err != nil || got != good {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := expectedChecksum(sums, "missing.tar.gz"); err == nil {
		t.Fatal("expected error for missing entry")
	}
	if _, err := expectedChecksum([]byte("zz  bad-hex-name\n"), "bad-hex-name"); err == nil {
		t.Fatal("expected error for malformed digest")
	}
}
