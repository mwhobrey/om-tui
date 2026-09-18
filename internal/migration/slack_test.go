package migration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxghenis/openmessage/internal/storage/sqlite"
	"github.com/maxghenis/openmessage/internal/v2keys"
)

func TestBuildTransformStateSlackPerTeamAccounts(t *testing.T) {
	t.Parallel()

	const (
		chanA   = "C111"
		chanB   = "D222"
		legacyA = "slack:TAAA:C111"
		legacyB = "slack:TBBB:D222"
		tsA     = "1700000000.000100"
		tsB     = "1700000000.000200"
	)
	dataset := legacyDataset{
		conversations: []legacyConversation{
			{
				ID: legacyA, Name: "#general", IsGroup: true, Platform: "slack",
				ParticipantsJSON: `[{"name":"Ada","id":"UAAA"},{"name":"you","id":"UYOU","is_me":true}]`,
				LastMessageMS:    fixtureBaseMS + 100,
			},
			{
				ID: legacyB, Name: "bob", Platform: "slack",
				ParticipantsJSON: `[{"name":"Bob","id":"UBBB"}]`,
				LastMessageMS:    fixtureBaseMS + 200,
			},
		},
		messages: []legacyMessage{
			{
				ID: "slack:C111:" + tsA, ConversationID: legacyA,
				SenderName: "Ada", SenderNumber: "UAAA", Body: "from team A",
				TimestampMS: fixtureBaseMS + 100, Platform: "slack", SourceID: chanA + ":" + tsA,
			},
			{
				ID: "slack:D222:" + tsB, ConversationID: legacyB,
				SenderName: "you", SenderNumber: "UYOU", Body: "from team B",
				TimestampMS: fixtureBaseMS + 200, IsFromMe: true, Platform: "slack",
				SourceID: chanB + ":" + tsB,
			},
		},
	}
	report := &Report{}
	state, err := buildTransformState(dataset, report)
	mustNoError(t, err)
	if len(state.accounts) != 2 {
		t.Fatalf("accounts = %d, want 2: %+v", len(state.accounts), state.accounts)
	}
	if state.accounts["slack-TAAA"].BridgeKey != "slack_web" || state.accounts["slack-TBBB"].BridgeKey != "slack_web" {
		t.Fatalf("accounts = %+v", state.accounts)
	}

	planA := state.conversations[legacyA]
	planB := state.conversations[legacyB]
	wantConvA := v2keys.DeriveID("conversation", "slack-TAAA", chanA)
	wantConvB := v2keys.DeriveID("conversation", "slack-TBBB", chanB)
	if planA == nil || planA.V2ID != wantConvA || conversationNaturalKey(planA.Account, legacyA) != chanA {
		t.Fatalf("conversation A = %+v, want id %s remote %s", planA, wantConvA, chanA)
	}
	if planB == nil || planB.V2ID != wantConvB || conversationNaturalKey(planB.Account, legacyB) != chanB {
		t.Fatalf("conversation B = %+v, want id %s remote %s", planB, wantConvB, chanB)
	}

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "store.sqlite3"))
	mustNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	mustNoError(t, writeAccounts(store, state))
	mustNoError(t, writeIdentities(store, state))
	mustNoError(t, writeConversations(store, state))

	clock := state.baseTimestampMS
	messages, err := sqlite.NewMessageRepository(store, func() time.Time { return time.UnixMilli(clock) })
	mustNoError(t, err)
	mustNoError(t, writeHistory(context.Background(), store, messages, dataset, state, &clock, report))

	assertSlackConversationViaStore(t, store, wantConvA, "slack-TAAA", chanA, "#general")
	assertSlackConversationViaStore(t, store, wantConvB, "slack-TBBB", chanB, "bob")
	wantMsgA := v2keys.DeriveID("message", "slack-TAAA", chanA+"\x1f"+tsA)
	wantMsgB := v2keys.DeriveID("message", "slack-TBBB", chanB+"\x1f"+tsB)
	assertSlackMessageViaRepo(t, messages, wantMsgA, wantConvA, "slack-TAAA", tsA, "from team A", sqlite.MessageDirectionIncoming)
	assertSlackMessageViaRepo(t, messages, wantMsgB, wantConvB, "slack-TBBB", tsB, "from team B", sqlite.MessageDirectionOutgoing)
}

func TestAccountForLegacyConversationSlack(t *testing.T) {
	t.Parallel()

	got, err := accountForLegacyConversation("slack", "slack:T99:C1")
	mustNoError(t, err)
	if got.AccountID != "slack-T99" || got.BridgeKey != "slack_web" || got.Platform != "slack" {
		t.Fatalf("account = %+v", got)
	}
	if _, err := accountForLegacyConversation("slack", "C1"); err == nil {
		t.Fatal("expected error for channel-only id")
	}
	if _, err := accountForPlatform("slack"); err == nil {
		t.Fatal("expected error for slack without conversation")
	}
}

func TestAccountForLegacyConversationExtraLiveRivers(t *testing.T) {
	t.Parallel()

	wa, err := accountForLegacyConversation("whatsapp", "whatsapp/whatsapp-2/1555@s.whatsapp.net")
	mustNoError(t, err)
	if wa.AccountID != "whatsapp-2" || wa.BridgeKey != "whatsmeow" || wa.Platform != "whatsapp" {
		t.Fatalf("whatsapp extra = %+v", wa)
	}
	if key := conversationNaturalKey(wa, "whatsapp/whatsapp-2/1555@s.whatsapp.net"); key != "whatsapp:1555@s.whatsapp.net" {
		t.Fatalf("whatsapp extra natural key = %q", key)
	}

	sig, err := accountForLegacyConversation("signal", "signal/signal-2/+15551230000")
	mustNoError(t, err)
	if sig.AccountID != "signal-2" || sig.BridgeKey != "signal_cli" {
		t.Fatalf("signal extra = %+v", sig)
	}
	if key := conversationNaturalKey(sig, "signal/signal-2/+15551230000"); key != "signal:+15551230000" {
		t.Fatalf("signal extra natural key = %q", key)
	}

	group, err := accountForLegacyConversation("signal", "signal-group/signal-3/group-id")
	mustNoError(t, err)
	if group.AccountID != "signal-3" {
		t.Fatalf("signal group extra = %+v", group)
	}
	if key := conversationNaturalKey(group, "signal-group/signal-3/group-id"); key != "signal-group:group-id" {
		t.Fatalf("signal group extra natural key = %q", key)
	}

	primary, err := accountForLegacyConversation("whatsapp", "whatsapp:1555@s.whatsapp.net")
	mustNoError(t, err)
	if primary.AccountID != "whatsapp-primary" {
		t.Fatalf("whatsapp default = %+v", primary)
	}
}

func TestDeriveRemoteMessageIDSlackUsesTimestamp(t *testing.T) {
	t.Parallel()

	got := deriveRemoteMessageID(legacyMessage{
		ID: "slack:C1:1700000000.000100", Platform: "slack", SourceID: "C1:1700000000.000100",
	})
	if got != "1700000000.000100" {
		t.Fatalf("remote = %q, want ts", got)
	}
	got = deriveRemoteMessageID(legacyMessage{
		ID: "slack:C1:1700000000.000200", Platform: "slack",
	})
	if got != "1700000000.000200" {
		t.Fatalf("remote from message id = %q", got)
	}
}

func assertSlackConversationViaStore(t *testing.T, store *sqlite.Store, conversationID, accountID, remote, title string) {
	t.Helper()
	got, err := store.GetConversation(conversationID)
	mustNoError(t, err)
	if got.AccountID != accountID || got.RemoteConversationID != remote || got.Title != title {
		t.Fatalf("conversation %s = %+v", conversationID, got)
	}
}

func assertSlackMessageViaRepo(
	t *testing.T,
	repo *sqlite.MessageRepository,
	messageID, conversationID, accountID, remote, body string,
	direction sqlite.MessageDirection,
) {
	t.Helper()
	got, err := repo.GetMessage(context.Background(), messageID)
	mustNoError(t, err)
	if got.ConversationID != conversationID ||
		got.AccountID != accountID ||
		got.RemoteMessageID != remote ||
		got.Body != body ||
		got.Direction != direction {
		t.Fatalf("message %s = %+v", messageID, got)
	}
}
