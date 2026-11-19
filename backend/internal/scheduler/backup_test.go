package scheduler

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/labvory/internal/db"
)

// newTestPool migrates the shared test database (migrations are idempotent)
// and opens a pool; the backup job only reads, so no truncation is needed.
func newTestPool(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	if err := db.Migrate(databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestPruneBackupSnapshots covers the scheduled-backup retention pruning:
// snapshots are timestamped files, the newest `retention` survive, and
// unrelated files are never touched.
func TestPruneBackupSnapshots(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Five daily snapshots (names sort chronologically) plus two foreign files.
	for _, day := range []string{"01", "02", "03", "04", "05"} {
		write(backupFilePrefix + "202611" + day + "-120000.json")
	}
	write("other.json")
	write(backupFilePrefix + "20261106-120000.json.tmp")

	if err := pruneBackupSnapshots(dir, 3); err != nil {
		t.Fatalf("prune: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var kept, foreign []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, backupFilePrefix) && strings.HasSuffix(name, ".json") {
			kept = append(kept, name)
		} else {
			foreign = append(foreign, name)
		}
	}
	if len(kept) != 3 {
		t.Fatalf("expected 3 snapshots kept, got %d: %v", len(kept), kept)
	}
	// The two oldest must be gone; the newest three remain.
	if kept[0] != backupFilePrefix+"20261103-120000.json" {
		t.Fatalf("oldest kept snapshot = %s, want the 03 file", kept[0])
	}
	if len(foreign) != 2 {
		t.Fatalf("foreign files must survive pruning, got %v", foreign)
	}

	// Retention above the count is a no-op.
	if err := pruneBackupSnapshots(dir, 10); err != nil {
		t.Fatalf("prune with high retention: %v", err)
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 5 {
		t.Fatalf("no-op prune changed the directory: %d entries", len(entries))
	}
}

// TestWriteBackupSnapshot exercises the backup job end-to-end against a real
// database: the snapshot lands in the directory under the expected name and
// pruning applies on the next write.
func TestWriteBackupSnapshot(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s := New(newTestPool(t, databaseURL), nil, testLogger(), nil)
	dir := t.TempDir()

	now := time.Now()
	name, err := s.writeBackupSnapshot(t.Context(), now, backupSettings{
		dir: dir, intervalHours: 24, retention: 1,
	})
	if err != nil {
		t.Fatalf("writeBackupSnapshot: %v", err)
	}
	if !strings.HasPrefix(name, backupFilePrefix) || !strings.HasSuffix(name, ".json") {
		t.Fatalf("unexpected snapshot name %q", name)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !strings.Contains(string(data), "assets") {
		t.Fatal("snapshot should contain the exported inventory")
	}

	// A second write with retention 1 prunes the first snapshot away.
	time.Sleep(10 * time.Millisecond) // distinct timestamp
	name2, err := s.writeBackupSnapshot(t.Context(), now.Add(time.Second), backupSettings{
		dir: dir, intervalHours: 24, retention: 1,
	})
	if err != nil {
		t.Fatalf("second writeBackupSnapshot: %v", err)
	}
	if name2 == name {
		t.Fatal("snapshot names must differ")
	}
	entries, _ := os.ReadDir(dir)
	var kept int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".tmp") {
			kept++
		}
	}
	if kept != 1 {
		t.Fatalf("retention 1 should keep one snapshot, got %d", kept)
	}
}
