package migration

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCheckpointAndSyncSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite3")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO t (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := checkpointAndSyncSQLite(context.Background(), path); err != nil {
		t.Fatalf("checkpointAndSyncSQLite(): %v", err)
	}
}
