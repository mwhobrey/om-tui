package slacklive

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/maxghenis/openmessage/internal/db"
)

type fakeSlackAPI struct {
	users          []slack.User
	history        *slack.GetConversationHistoryResponse
	replies        []slack.Message
	addName        string
	addItem        slack.ItemRef
	addErr         error
	removeName     string
	removeItem     slack.ItemRef
	removeErr      error
	fileInfo       *slack.File
	fileInfoErr    error
	fileBytes      []byte
	fileGetErr     error
	gotFileID      string
	gotDownloadURL string
	uploadURL      string
	uploadFileID   string
	getURLErr      error
	uploadErr      error
	completeErr    error
	uploaded       []byte
	uploadedName   string
	completeParams slack.CompleteUploadExternalParameters
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
func (f *fakeSlackAPI) AddReactionContext(_ context.Context, name string, item slack.ItemRef) error {
	f.addName = name
	f.addItem = item
	return f.addErr
}
func (f *fakeSlackAPI) RemoveReactionContext(_ context.Context, name string, item slack.ItemRef) error {
	f.removeName = name
	f.removeItem = item
	return f.removeErr
}
func (f *fakeSlackAPI) GetFileInfoContext(_ context.Context, fileID string, _, _ int) (*slack.File, []slack.Comment, *slack.Paging, error) {
	f.gotFileID = fileID
	if f.fileInfoErr != nil {
		return nil, nil, nil, f.fileInfoErr
	}
	if f.fileInfo != nil {
		return f.fileInfo, nil, nil, nil
	}
	return &slack.File{
		ID:                 fileID,
		Name:               "photo.png",
		Mimetype:           "image/png",
		URLPrivateDownload: "https://files.slack.com/files-pri/T1-F1/download/photo.png",
	}, nil, nil, nil
}
func (f *fakeSlackAPI) GetFileContext(_ context.Context, downloadURL string, writer io.Writer) error {
	f.gotDownloadURL = downloadURL
	if f.fileGetErr != nil {
		return f.fileGetErr
	}
	if len(f.fileBytes) == 0 {
		f.fileBytes = []byte("png-bytes")
	}
	_, err := writer.Write(f.fileBytes)
	return err
}
func (f *fakeSlackAPI) GetUploadURLExternalContext(_ context.Context, _ slack.GetUploadURLExternalParameters) (*slack.GetUploadURLExternalResponse, error) {
	if f.getURLErr != nil {
		return nil, f.getURLErr
	}
	id := f.uploadFileID
	if id == "" {
		id = "FUP1"
	}
	url := f.uploadURL
	if url == "" {
		url = "https://files.slack.com/upload/v1/xxx"
	}
	return &slack.GetUploadURLExternalResponse{UploadURL: url, FileID: id}, nil
}
func (f *fakeSlackAPI) UploadToURL(_ context.Context, params slack.UploadToURLParameters) error {
	if f.uploadErr != nil {
		return f.uploadErr
	}
	f.uploadedName = params.Filename
	if params.Reader != nil {
		b, err := io.ReadAll(params.Reader)
		if err != nil {
			return err
		}
		f.uploaded = b
	}
	return nil
}
func (f *fakeSlackAPI) CompleteUploadExternalContext(_ context.Context, params slack.CompleteUploadExternalParameters) (*slack.CompleteUploadExternalResponse, error) {
	f.completeParams = params
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	files := params.Files
	if len(files) == 0 {
		id := f.uploadFileID
		if id == "" {
			id = "FUP1"
		}
		files = []slack.FileSummary{{ID: id}}
	}
	return &slack.CompleteUploadExternalResponse{Files: files}, nil
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

func TestIngestMessagesEmitsV2IngressFrames(t *testing.T) {
	api := &fakeSlackAPI{users: []slack.User{{
		ID:      "U1",
		Profile: slack.UserProfile{DisplayName: "Alice"},
	}}}
	client, store := newTestClient(t, api)
	if err := client.SyncUsers(context.Background()); err != nil {
		t.Fatal(err)
	}
	var frames []IngressFrame
	client.SetIngress(func(frame IngressFrame) { frames = append(frames, frame) })
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
		User:      "U1",
		Text:      "hello",
		Timestamp: "1700000001.000001",
	}}}
	if _, _, _, err := client.ingestMessages(context.Background(), convID, "C1", input, false); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("ingress frames = %d, want 1", len(frames))
	}
	if frames[0].ChannelID != "C1" || frames[0].TS != "1700000001.000001" || frames[0].Body != "hello" ||
		frames[0].UserID != "U1" || frames[0].ChannelKind != "group" {
		t.Fatalf("frame = %+v", frames[0])
	}
	if _, _, _, err := client.ingestMessages(context.Background(), convID, "C1", input, false); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("duplicate ingest should still emit for v2 dedupe, got %d", len(frames))
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

func TestAddAndRemoveReactionUseChannelAndMappedName(t *testing.T) {
	api := &fakeSlackAPI{}
	client, _ := newTestClient(t, api)
	ctx := context.Background()
	if err := client.AddReaction(ctx, "slack:T1:C9", "slack:C9:1700000001.000001", "👍"); err != nil {
		t.Fatal(err)
	}
	if api.addName != "+1" || api.addItem.Channel != "C9" || api.addItem.Timestamp != "1700000001.000001" {
		t.Fatalf("add = name=%q item=%+v", api.addName, api.addItem)
	}
	if err := client.RemoveReaction(ctx, "slack:T1:C9", "1700000001.000001", "❤️"); err != nil {
		t.Fatal(err)
	}
	if api.removeName != "heart" || api.removeItem.Channel != "C9" || api.removeItem.Timestamp != "1700000001.000001" {
		t.Fatalf("remove = name=%q item=%+v", api.removeName, api.removeItem)
	}
}

func TestIngestMessagesCopiesReactionsOntoIngressFrame(t *testing.T) {
	api := &fakeSlackAPI{users: []slack.User{{
		ID:      "U1",
		Profile: slack.UserProfile{DisplayName: "Alice"},
	}}}
	client, store := newTestClient(t, api)
	client.userID = "U0"
	if err := client.SyncUsers(context.Background()); err != nil {
		t.Fatal(err)
	}
	var frames []IngressFrame
	client.SetIngress(func(frame IngressFrame) { frames = append(frames, frame) })
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
		User:      "U1",
		Text:      "hello",
		Timestamp: "1700000001.000001",
		Reactions: []slack.ItemReaction{{
			Name:  "+1",
			Users: []string{"U1", "U0"},
		}},
	}}}
	if _, _, _, err := client.ingestMessages(context.Background(), convID, "C1", input, false); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames = %d", len(frames))
	}
	if len(frames[0].Reactions) != 2 {
		t.Fatalf("reactions = %+v", frames[0].Reactions)
	}
	if frames[0].Reactions[0].Name != "+1" || frames[0].Reactions[0].UserID != "U1" || frames[0].Reactions[0].IsSelf {
		t.Fatalf("first reaction = %+v", frames[0].Reactions[0])
	}
	if !frames[0].Reactions[1].IsSelf || frames[0].Reactions[1].UserID != "U0" {
		t.Fatalf("self reaction = %+v", frames[0].Reactions[1])
	}
}

func TestIngestMessagesCopiesFilesOntoIngressFrame(t *testing.T) {
	api := &fakeSlackAPI{}
	client, store := newTestClient(t, api)
	var frames []IngressFrame
	client.SetIngress(func(frame IngressFrame) { frames = append(frames, frame) })
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
		User:      "U1",
		Timestamp: "1700000001.000001",
		Files: []slack.File{{
			ID:       "F1",
			Name:     "photo.png",
			Mimetype: "image/png",
			Size:     9,
		}},
	}}}
	if _, _, _, err := client.ingestMessages(context.Background(), convID, "C1", input, false); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames = %d", len(frames))
	}
	if frames[0].Body != "[file]" || len(frames[0].Files) != 1 || frames[0].Files[0].ID != "F1" {
		t.Fatalf("frame = %+v", frames[0])
	}
}

func TestDownloadFileUsesPrivateURL(t *testing.T) {
	api := &fakeSlackAPI{fileBytes: []byte("png-bytes")}
	client, _ := newTestClient(t, api)
	got, err := client.DownloadFile(context.Background(), "F9")
	if err != nil {
		t.Fatal(err)
	}
	if api.gotFileID != "F9" {
		t.Fatalf("files.info id = %q", api.gotFileID)
	}
	if api.gotDownloadURL != "https://files.slack.com/files-pri/T1-F1/download/photo.png" {
		t.Fatalf("download url = %q", api.gotDownloadURL)
	}
	if string(got.Data) != "png-bytes" || got.Filename != "photo.png" || got.MIME != "image/png" {
		t.Fatalf("downloaded = %+v", got)
	}
}

func TestIngestMessagesFlattensBlockKitBotMessage(t *testing.T) {
	api := &fakeSlackAPI{}
	client, store := newTestClient(t, api)
	var frames []IngressFrame
	client.SetIngress(func(frame IngressFrame) { frames = append(frames, frame) })
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
		BotID:     "B1",
		Username:  "GitHub",
		Timestamp: "1700000001.000001",
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			&slack.HeaderBlock{Text: slack.NewTextBlockObject("plain_text", "PR opened", true, false)},
			&slack.SectionBlock{Text: slack.NewTextBlockObject("mrkdwn", "*feat:* mention <@U0>", false, false)},
		}},
	}}}
	if _, _, changed, err := client.ingestMessages(context.Background(), convID, "C1", input, true); err != nil || !changed {
		t.Fatalf("ingest changed=%v err=%v", changed, err)
	}
	msg, err := store.GetMessageByID("slack:C1:1700000001.000001")
	if err != nil {
		t.Fatal(err)
	}
	if msg.SenderName != "GitHub" || msg.SenderNumber != "B1" {
		t.Fatalf("sender = %+v", msg)
	}
	if !strings.Contains(msg.Body, "PR opened") || !strings.Contains(msg.Body, "*feat:* mention @U0") {
		t.Fatalf("body = %q", msg.Body)
	}
	if !msg.MentionsMe {
		t.Fatal("expected mention from flattened blocks")
	}
	if len(frames) != 1 || frames[0].UserID != "B1" || !strings.Contains(frames[0].Body, "PR opened") {
		t.Fatalf("frame = %+v", frames)
	}
	if len(frames[0].BlocksJSON) == 0 {
		t.Fatal("expected BlocksJSON on ingress frame")
	}
}
