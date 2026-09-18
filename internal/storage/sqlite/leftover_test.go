package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestLeftoverWritesDraftsTabsAndContactMeta(t *testing.T) {
	store, _ := openMessageTestRepository(t, func() time.Time { return time.UnixMilli(messageTestTimeMS) })
	seedMessageProjectionGraph(t, store)
	ctx := context.Background()

	draft, err := store.UpsertDraft(ctx, Draft{ConversationID: "conversation-a", Body: "hello later"})
	if err != nil {
		t.Fatalf("UpsertDraft: %v", err)
	}
	if draft.DraftID == "" || draft.Body != "hello later" {
		t.Fatalf("draft = %+v", draft)
	}
	listed, err := store.ListDrafts(ctx, "conversation-a")
	if err != nil || len(listed) != 1 || listed[0].DraftID != draft.DraftID {
		t.Fatalf("ListDrafts = %+v err=%v", listed, err)
	}

	tab, err := store.CreateTab(ctx, "Work")
	if err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	if err := store.SetConversationTab(ctx, "conversation-a", tab.TabID); err != nil {
		t.Fatalf("SetConversationTab: %v", err)
	}
	loaded, err := store.GetConversation("conversation-a")
	if err != nil {
		t.Fatal(err)
	}
	if ConversationTab(loaded) != tab.TabID {
		t.Fatalf("tab = %q, want %q", ConversationTab(loaded), tab.TabID)
	}
	if err := store.DeleteTab(ctx, tab.TabID); err != nil {
		t.Fatalf("DeleteTab: %v", err)
	}
	loaded, err = store.GetConversation("conversation-a")
	if err != nil {
		t.Fatal(err)
	}
	if ConversationTab(loaded) != "" {
		t.Fatalf("tab after delete = %q, want inbox", ConversationTab(loaded))
	}

	if err := store.SetContactTags(ctx, "alice", "Alice", []string{"friend", "friend", " work "}); err != nil {
		t.Fatalf("SetContactTags: %v", err)
	}
	meta, err := store.GetContactMeta(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if meta.DisplayName != "Alice" || len(meta.Tags) != 2 || meta.Tags[0] != "friend" || meta.Tags[1] != "work" {
		t.Fatalf("contact meta = %+v", meta)
	}
}

func TestLeftoverWritesMigrationIsChecksummedAndStrict(t *testing.T) {
	store, _ := openMessageTestRepository(t, func() time.Time { return time.UnixMilli(messageTestTimeMS) })
	if len(embeddedMigrations) != 12 {
		t.Fatalf("embedded migrations = %d, want 12", len(embeddedMigrations))
	}
	assertPragmaInt(t, store.db, "user_version", len(embeddedMigrations))
	ledger := readLedgerRow(t, store.db, 12)
	if ledger.name != "leftover_writes" {
		t.Fatalf("migration 0012 name = %q, want leftover_writes", ledger.name)
	}
	const wantChecksum = "8d0e550c9797b8c201b2fdcc1f9fe5a789899c9c8ecc270c91376c12ee526205"
	if ledger.checksum != wantChecksum || embeddedMigrations[11].checksumSHA256 != wantChecksum {
		t.Fatalf("migration 0012 checksum = %q embedded %q, want %q", ledger.checksum, embeddedMigrations[11].checksumSHA256, wantChecksum)
	}
	for _, table := range []string{"drafts", "tabs", "contact_meta"} {
		var strict int
		if err := store.db.QueryRow(`
			SELECT strict FROM pragma_table_list
			WHERE schema = 'main' AND name = ?
		`, table).Scan(&strict); err != nil {
			t.Fatalf("read %s STRICT flag: %v", table, err)
		}
		if strict != 1 {
			t.Fatalf("%s STRICT = %d, want 1", table, strict)
		}
	}
}
