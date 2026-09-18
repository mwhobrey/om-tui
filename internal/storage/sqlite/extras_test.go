package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestMessageExtrasMergePreservesTranscriptAndBlocks(t *testing.T) {
	clock := newMessageTestClock(messageTestTimeMS)
	store, repository := openMessageTestRepository(t, clock.Now)
	seedMessageProjectionGraph(t, store)
	message := messageTestMessage("message-extra", "conversation-a", "account-a", "remote-extra", pointer("identity-a"))
	if err := repository.ImportMessage(context.Background(), MessageProjection{Message: message}); err != nil {
		t.Fatalf("ImportMessage(): %v", err)
	}

	blocks := json.RawMessage(`{"blocks":[{"type":"section","text":{"type":"plain_text","text":"hello"}}]}`)
	if err := store.MergeMessagePayload(context.Background(), message.MessageID, MessagePayload{Blocks: blocks}); err != nil {
		t.Fatalf("MergeMessagePayload(blocks): %v", err)
	}
	model := "whisper-test"
	if err := store.SetMessageTranscript(context.Background(), message.MessageID, "said hello", &model); err != nil {
		t.Fatalf("SetMessageTranscript(): %v", err)
	}
	got, err := store.MessageExtrasFor(context.Background(), []string{message.MessageID})
	if err != nil {
		t.Fatalf("MessageExtrasFor(): %v", err)
	}
	extra := got[message.MessageID]
	if string(extra.Payload.Blocks) != string(blocks) {
		t.Fatalf("blocks = %s, want %s", extra.Payload.Blocks, blocks)
	}
	if extra.Payload.Transcript != "said hello" || extra.Payload.TranscriptModel != model || extra.Payload.TranscribedAtMS == 0 {
		t.Fatalf("transcript = %+v", extra.Payload)
	}

	if err := store.SetMessageTranscript(context.Background(), message.MessageID, "", nil); err != nil {
		t.Fatalf("SetMessageTranscript(clear): %v", err)
	}
	got, err = store.MessageExtrasFor(context.Background(), []string{message.MessageID})
	if err != nil {
		t.Fatalf("MessageExtrasFor(after clear): %v", err)
	}
	extra = got[message.MessageID]
	if extra.Payload.Transcript != "" || extra.Payload.TranscriptModel != "" || extra.Payload.TranscribedAtMS != 0 {
		t.Fatalf("cleared transcript = %+v", extra.Payload)
	}
	if string(extra.Payload.Blocks) != string(blocks) {
		t.Fatalf("blocks cleared with transcript: %s", extra.Payload.Blocks)
	}

	if err := store.SetMessageTranscript(context.Background(), "missing", "nope", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing transcript error = %v, want ErrNotFound", err)
	}
}

func TestMessageExtrasMigrationIsChecksummedAndStrict(t *testing.T) {
	store, _ := openMessageTestRepository(t, func() time.Time { return time.UnixMilli(messageTestTimeMS) })
	if len(embeddedMigrations) != 12 {
		t.Fatalf("embedded migrations = %d, want 12", len(embeddedMigrations))
	}
	assertPragmaInt(t, store.db, "user_version", len(embeddedMigrations))
	ledger := readLedgerRow(t, store.db, 11)
	if ledger.name != "message_extras" {
		t.Fatalf("migration 0011 name = %q, want message_extras", ledger.name)
	}
	const wantChecksum = "4618fb5df370d62491bf709f4b79442e07cab7e345d4a08e7b606fbdc875e721"
	if ledger.checksum != wantChecksum || embeddedMigrations[10].checksumSHA256 != wantChecksum {
		t.Fatalf("migration 0011 checksum = %q embedded %q, want %q", ledger.checksum, embeddedMigrations[10].checksumSHA256, wantChecksum)
	}
	var strict int
	if err := store.db.QueryRow(`
		SELECT strict
		FROM pragma_table_list
		WHERE schema = 'main' AND name = 'message_extras'
	`).Scan(&strict); err != nil {
		t.Fatalf("read message_extras STRICT flag: %v", err)
	}
	if strict != 1 {
		t.Fatalf("message_extras strict = %d, want 1", strict)
	}
}
