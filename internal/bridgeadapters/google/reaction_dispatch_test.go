package google

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gmproto "go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"

	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/messaging"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
)

// reactionDispatchRegistry leases one adapter's text and reaction senders to
// the dispatcher, the way the account registry does for a connected account.
type reactionDispatchRegistry struct {
	accountID string
	adapter   *Adapter
}

func (r reactionDispatchRegistry) Snapshot(string) (bridge.Snapshot, bool) {
	return bridge.Snapshot{}, false
}

func (r reactionDispatchRegistry) Acquire(
	ctx context.Context,
	accountID string,
	capability bridge.Capability,
) (*bridge.DispatchLease, error) {
	if accountID != r.accountID {
		return nil, bridge.ErrCapabilityUnavailable
	}
	lease := &bridge.DispatchLease{AccountID: accountID, Platform: bridge.PlatformGoogle, Generation: 1, Ctx: ctx}
	switch capability {
	case bridge.CapabilityReactions:
		lease.Reaction = r.adapter
	case bridge.CapabilityTextSend:
		lease.Text = r.adapter
	default:
		return nil, bridge.ErrCapabilityUnavailable
	}
	return lease, nil
}

func (r reactionDispatchRegistry) Capabilities(accountID string) bridge.CapabilitySet {
	return bridge.CapabilitySet{Reactions: accountID == r.accountID, TextSend: accountID == r.accountID}
}

// reactionDispatchClock is a wall clock the test moves by hand.
type reactionDispatchClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *reactionDispatchClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *reactionDispatchClock) Advance(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(by)
}

func (c *reactionDispatchClock) NewTimer(d time.Duration) messaging.Timer {
	return messaging.SystemClock{}.NewTimer(d)
}

const (
	reactionDispatchAccountID      = "google-primary"
	reactionDispatchConversationID = "v2-google-thread"
)

// reactionDispatchFixture is a v2 store with one Google thread holding one
// incoming message, served by the real Google adapter (only libgm's transport
// is faked) through the real MessageService dispatcher.
type reactionDispatchFixture struct {
	t        *testing.T
	ctx      context.Context
	clock    *reactionDispatchClock
	store    *sqlite.Store
	service  *messaging.MessageService
	outbox   *sqlite.OutboxRepository
	adapter  *Adapter
	reaction *fakeReactionSendClient
	text     *fakeTextSendClient
	keys     int
}

func newReactionDispatchFixture(t *testing.T) *reactionDispatchFixture {
	t.Helper()
	ctx := context.Background()
	clock := &reactionDispatchClock{now: time.UnixMilli(1_900_000_000_000)}
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "v2.sqlite3"))
	if err != nil {
		t.Fatalf("sqlite.Open(): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	nowMS := clock.Now().UnixMilli()
	if err := store.UpsertAccount(sqlite.Account{
		AccountID: reactionDispatchAccountID, BridgeKey: "google", DisplayName: "Google Messages",
		Mode: sqlite.AccountModeLive, Enabled: true, ConfigJSON: "{}",
		CreatedAtMS: nowMS, UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatalf("UpsertAccount(): %v", err)
	}
	if err := store.UpsertConversation(sqlite.Conversation{
		ConversationID: reactionDispatchConversationID, AccountID: reactionDispatchAccountID,
		RemoteConversationID: "conversation-id", Kind: sqlite.ConversationKindDirect, Title: "thread",
		NotificationMode: sqlite.NotificationModeAll, MetadataJSON: "{}",
		CreatedAtMS: nowMS, UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatalf("UpsertConversation(): %v", err)
	}
	messages, err := sqlite.NewMessageRepository(store, clock.Now)
	if err != nil {
		t.Fatalf("NewMessageRepository(): %v", err)
	}
	if _, err := messages.AppendInbox(ctx, sqlite.InboxRecord{
		InboxID: "inbox-target", AccountID: reactionDispatchAccountID, Generation: 1,
		DedupeKey: "inbox-target", Codec: "test", CodecVersion: 1, Payload: []byte("test"),
	}); err != nil {
		t.Fatalf("AppendInbox(): %v", err)
	}
	if err := messages.ProjectMessage(ctx, sqlite.MessageProjection{
		InboxID: "inbox-target",
		Message: sqlite.Message{
			MessageID: "v2-target", ConversationID: reactionDispatchConversationID,
			AccountID: reactionDispatchAccountID, RemoteMessageID: "target-id",
			Direction: sqlite.MessageDirectionIncoming, Body: "react to this",
			State: sqlite.MessageStateActive, OccurredAtMS: nowMS - 60_000,
		},
	}); err != nil {
		t.Fatalf("ProjectMessage(): %v", err)
	}

	reaction := &fakeReactionSendClient{
		conversationResult: &gmproto.Conversation{},
		sendResult:         &gmproto.SendReactionResponse{Success: false},
	}
	text := &fakeTextSendClient{
		conversationResult: &gmproto.Conversation{},
		sendResult:         &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_SUCCESS},
	}
	adapter := newReactionSendTestAdapter(t, reaction)
	installTextSendClient(t, adapter.host.GetClient(), text)
	service, err := messaging.NewMessageService(
		store,
		reactionDispatchRegistry{accountID: reactionDispatchAccountID, adapter: adapter},
		nil,
		clock,
		messaging.CryptoIDSource{},
	)
	if err != nil {
		t.Fatalf("NewMessageService(): %v", err)
	}
	outbox, err := sqlite.NewOutboxRepository(store, clock.Now)
	if err != nil {
		t.Fatalf("NewOutboxRepository(): %v", err)
	}
	return &reactionDispatchFixture{
		t: t, ctx: ctx, clock: clock, store: store, service: service, outbox: outbox,
		adapter: adapter, reaction: reaction, text: text,
	}
}

func (f *reactionDispatchFixture) common() messaging.CommonCommand {
	f.keys++
	return messaging.CommonCommand{
		AccountID:      reactionDispatchAccountID,
		ConversationID: reactionDispatchConversationID,
		IdempotencyKey: fmt.Sprintf("reaction-dispatch-%d", f.keys),
	}
}

func (f *reactionDispatchFixture) react(targetMessageID string) messaging.Submission {
	f.t.Helper()
	submission, err := f.service.SendReaction(f.ctx, messaging.SendReactionCommand{
		CommonCommand:   f.common(),
		TargetMessageID: targetMessageID,
		Emoji:           "👍",
		Action:          bridge.ReactionAdd,
	})
	if err != nil {
		f.t.Fatalf("SendReaction(%q): %v", targetMessageID, err)
	}
	return submission
}

func (f *reactionDispatchFixture) dispatch(want int) {
	f.t.Helper()
	if processed, err := f.service.DispatchDue(f.ctx, 4); err != nil || processed != want {
		f.t.Fatalf("DispatchDue() = %d, %v; want %d, nil", processed, err, want)
	}
}

func (f *reactionDispatchFixture) delivery(outboxID string) messaging.Delivery {
	f.t.Helper()
	delivery, err := f.service.Get(f.ctx, outboxID)
	if err != nil {
		f.t.Fatalf("Get(%q): %v", outboxID, err)
	}
	return delivery
}
