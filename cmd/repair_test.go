package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/storage/sqlite"
)

// seedRepairStore provisions a current v2 store with the Google account the
// repair plans for, and returns its path.
func seedRepairStore(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("OPENMESSAGES_DATA_DIR", dataDir)
	t.Setenv("OPENMESSAGES_DEMO", "0")
	t.Setenv("OPENMESSAGES_APP_SANDBOX", "1")
	v2Dir := filepath.Join(dataDir, "v2")
	if err := os.MkdirAll(v2Dir, 0o700); err != nil {
		t.Fatalf("create v2 dir: %v", err)
	}
	storePath := filepath.Join(v2Dir, "store.sqlite3")
	store, err := sqlite.Open(storePath)
	if err != nil {
		t.Fatalf("sqlite.Open(): %v", err)
	}
	nowMS := time.Now().UnixMilli()
	if err := store.UpsertAccount(sqlite.Account{
		AccountID:   "google-primary",
		BridgeKey:   "google_messages",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	}); err != nil {
		store.Close()
		t.Fatalf("UpsertAccount(): %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return storePath
}

// A dry run needs no exclusive ownership (no daemon probe, no instance lock),
// so it must never migrate: run beside the app, a migration's write lock
// would stall the daemon's writes.
func TestRepairDryRunNeverMigratesTheStore(t *testing.T) {
	storePath := seedRepairStore(t)
	latest := rollBackLedgerOneVersion(t, storePath)

	var stdout, stderr bytes.Buffer
	err := runRepairGoogleIDSpace(context.Background(), zerolog.Nop(), []string{"--since", "1"}, &stdout, &stderr)
	if !errors.Is(err, sqlite.ErrMigrationPending) || !strings.Contains(err.Error(), "reopen the OpenMessage app") {
		t.Fatalf("dry run error = %v, want ErrMigrationPending telling the user to reopen the app", err)
	}
	if got := ledgerVersionCount(t, storePath); got != latest-1 {
		t.Fatalf("ledger rows = %d after the dry run, want %d: the dry run applied a migration", got, latest-1)
	}
}

// The reference snapshot is the user's own backup. The repair reads a migrated
// copy of it and never writes the file itself, so a read-only snapshot works
// and stays byte-identical.
func TestRepairDryRunReadsACopyOfTheReferenceSnapshot(t *testing.T) {
	storePath := seedRepairStore(t)
	reference := filepath.Join(t.TempDir(), "reference.sqlite3")
	payload, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reference, payload, 0o400); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := runRepairGoogleIDSpace(
		context.Background(), zerolog.Nop(),
		[]string{"--since", "1", "--reference", reference, "--json"}, &stdout, &stderr,
	); err != nil {
		t.Fatalf("dry run with a read-only reference: %v\n%s", err, stderr.String())
	}
	after, err := os.ReadFile(reference)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, after) {
		t.Fatal("the repair wrote the reference snapshot")
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(reference + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the repair created %s next to the reference snapshot: %v", reference+suffix, err)
		}
	}
}

// rollBackLedgerOneVersion removes the newest ledger row and lowers
// user_version to match: the shape of a store the running app has not
// upgraded to this build's schema yet. It returns the version it removed.
func rollBackLedgerOneVersion(t *testing.T, storePath string) int {
	t.Helper()
	latest := ledgerVersionCount(t, storePath)
	database, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`DELETE FROM schema_migrations WHERE version = ?`, latest); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, latest-1)); err != nil {
		t.Fatal(err)
	}
	return latest
}

func ledgerVersionCount(t *testing.T, storePath string) int {
	t.Helper()
	database, err := sql.Open("sqlite", storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var rows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}
