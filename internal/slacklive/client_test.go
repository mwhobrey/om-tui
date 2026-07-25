package slacklive

import (
	"context"
	"testing"

	"github.com/slack-go/slack"

	"github.com/maxghenis/openmessage/internal/db"
)

type fakeSlackAPI struct {
	users   []slack.User
	history *slack.GetConversationHistoryResponse
	replies []slack.Message
}

func (f *fakeSlackAPI) AuthTest() (*slack.AuthTestResponse, error) {
	return &slack.AuthTestResponse{UserID: "U0", TeamID: "T1", Team: "Test"}, nil
}
func (f *fakeSlackAPI) GetConversationsContext(context.Context, *slack.GetConversationsParameters) ([]slack.Channel, string, error) {
	return nil, "", nil
}
func (f *fakeSlackAPI) GetConversationHistoryContext(context.Context, *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	if f.history == nil {
		return &slack.GetConversationHistoryResponse{}, nil
	}
	return f.history, nil
}
func (f *fakeSlackAPI) GetConversationRepliesContext(context.Context, *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	return f.replies, false, "", nil
}
func (f *fakeSlackAPI) GetUsersContext(context.Context, ...slack.GetUsersOption) ([]slack.User, error) {
	return f.users, nil
}
func (f *fakeSlackAPI) GetUserInfoContext(_ context.Context, user string) (*slack.User, error) {
	for i := range f.users {
		if f.users[i].ID == user {
			return &f.users[i], nil
		}
	}
	return &slack.User{ID: user}, nil
}
func (f *fakeSlackAPI) PostMessageContext(context.Context, string, ...slack.MsgOption) (string, string, error) {
	return "", "1700000002.000001", nil
}

func newTestClient(t *testing.T, api slackAPI) (*Client, *db.Store) {
	t.Helper()
	store, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	client := &Client{
		api:      api,
		store:    store,
		riverID:  "slack-T1",
		teamID:   "T1",
		userID:   "U0",
		users:    make(map[string]db.SlackUser),
		channels: make(map[string]string),
	}
	return client, store
}

func TestIngestMessagesResolvesIdentityThreadsAndUnreadOnce(t *testing.T) {
	api := &fakeSlackAPI{users: []slack.User{{
		ID:      "U1",
		Profile: slack.UserProfile{DisplayName: "Alice"},
	}}}
	client, store := newTestClient(t, api)
	if err := client.SyncUsers(context.Background()); err != nil {
		t.Fatal(err)
	}
	convID := "slack:T1:C1"
	if err := store.UpsertSlackConversation(&db.Conversation{
		ConversationID: convID,
		Name:           "#general",
		Participants:   `[{"id":"C1","kind":"public_channel"}]`,
		RiverID:        "slack-T1",
	}); err != nil {
		t.Fatal(err)
	}
	input := []slack.Message{{Msg: slack.Msg{
		User:            "U1",
		Text:            "hello <@U0>",
		Timestamp:       "1700000001.000001",
		ThreadTimestamp: "1700000000.000001",
	}}}
	_, _, changed, err := client.ingestMessages(context.Background(), convID, "C1", input, true)
	if err != nil || !changed {
		t.Fatalf("ingest changed=%v err=%v", changed, err)
	}
	msg, err := store.GetMessageByID("slack:C1:1700000001.000001")
	if err != nil {
		t.Fatal(err)
	}
	if msg.SenderName != "Alice" || msg.Body != "hello @U0" || !msg.MentionsMe {
		t.Fatalf("resolved message = %+v", msg)
	}
	if msg.ReplyToID != "slack:C1:1700000000.000001" || msg.SourceID != "C1:1700000001.000001" {
		t.Fatalf("thread/source ids = %+v", msg)
	}
	conv, _ := store.GetConversation(convID)
	if conv.UnreadCount != 1 {
		t.Fatalf("unread = %d, want 1", conv.UnreadCount)
	}
	if _, _, _, err := client.ingestMessages(context.Background(), convID, "C1", input, true); err != nil {
		t.Fatal(err)
	}
	conv, _ = store.GetConversation(convID)
	if conv.UnreadCount != 1 {
		t.Fatalf("duplicate poll unread = %d, want 1", conv.UnreadCount)
	}
}

func TestUpsertChannelUsesReadableDMNameAndKind(t *testing.T) {
	api := &fakeSlackAPI{users: []slack.User{{
		ID: "U1", Profile: slack.UserProfile{RealName: "Alice Example"},
	}}}
	client, store := newTestClient(t, api)
	if err := client.SyncUsers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.upsertChannel(context.Background(), slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	conv, err := store.GetConversation("slack:T1:D1")
	if err != nil {
		t.Fatal(err)
	}
	if conv.Name != "@Alice Example" || conv.StreamKind != "im" {
		t.Fatalf("DM = %+v", conv)
	}
}
