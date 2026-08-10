package slacklive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/river"
)

// Credentials is the vault blob for a Slack river.
type Credentials struct {
	Token    string `json:"token"`
	AppToken string `json:"app_token,omitempty"`
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
}

type Client struct {
	api      slackAPI
	store    *db.Store
	riverID  string
	teamID   string
	teamName string
	token    string
	appToken string
	userID   string

	cacheMu  sync.RWMutex
	users    map[string]db.SlackUser
	channels map[string]string

	mu              sync.Mutex
	cancel          context.CancelFunc
	connected       bool
	socketConnected bool
	lastError       string
	onChange        func(conversationID string)
}

func New(store *db.Store, riverID string, creds Credentials) (*Client, error) {
	token := strings.TrimSpace(creds.Token)
	if token == "" {
		return nil, fmt.Errorf("slacklive: token required")
	}
	if store == nil {
		return nil, fmt.Errorf("slacklive: store required")
	}
	api := slack.New(token)
	c := &Client{
		api:      api,
		store:    store,
		riverID:  riverID,
		teamID:   strings.TrimSpace(creds.TeamID),
		teamName: strings.TrimSpace(creds.TeamName),
		token:    token,
		appToken: strings.TrimSpace(creds.AppToken),
		users:    make(map[string]db.SlackUser),
		channels: make(map[string]string),
	}
	if resp, err := api.AuthTest(); err == nil {
		c.userID = resp.UserID
		if c.teamID == "" {
			c.teamID = resp.TeamID
		}
		if c.teamName == "" {
			c.teamName = resp.Team
		}
	}
	return c, nil
}

// AuthTest validates the token and fills team metadata.
func AuthTest(token string) (Credentials, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Credentials{}, fmt.Errorf("slack token required")
	}
	api := slack.New(token)
	resp, err := api.AuthTest()
	if err != nil {
		return Credentials{}, fmt.Errorf("slack auth.test: %w", err)
	}
	return Credentials{
		Token:    token,
		TeamID:   resp.TeamID,
		TeamName: resp.Team,
	}, nil
}

func (c *Client) Status() (connected bool, lastError string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected, c.lastError
}

func (c *Client) SocketStatus() (configured, connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.appToken != "", c.socketConnected
}

func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	c.loadCachedUsers()
	_ = c.SyncUsers(runCtx)
	if err := c.SyncStreams(runCtx); err != nil {
		c.setError(err)
		return err
	}
	c.setConnected(true)
	go c.pollLoop(runCtx)
	if c.appToken != "" {
		go c.socketLoop(runCtx)
	}
	return nil
}

func (c *Client) SetOnChange(fn func(conversationID string)) {
	c.mu.Lock()
	c.onChange = fn
	c.mu.Unlock()
}

func (c *Client) emitChange(conversationID string) {
	c.mu.Lock()
	fn := c.onChange
	c.mu.Unlock()
	if fn != nil {
		fn(conversationID)
	}
}

func (c *Client) Stop() {
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.connected = false
	c.socketConnected = false
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Client) setConnected(ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = ok
	if ok {
		c.lastError = ""
	}
}

func (c *Client) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	if err != nil {
		c.lastError = err.Error()
	}
}

// SyncStreams lists channels/DMs and upserts conversations for this river.
func (c *Client) SyncStreams(ctx context.Context) error {
	types := []string{"public_channel", "private_channel", "im", "mpim"}
	var cursor string
	for {
		params := &slack.GetConversationsParameters{
			Cursor:          cursor,
			ExcludeArchived: true,
			Limit:           200,
			Types:           types,
		}
		channels, next, err := c.api.GetConversationsContext(ctx, params)
		if err != nil {
			if wait, limited := rateLimitRetryAfter(err); limited {
				if waitErr := sleepForRetry(ctx, wait); waitErr != nil {
					return waitErr
				}
				continue
			}
			return fmt.Errorf("conversations.list: %w", err)
		}
		for _, ch := range channels {
			if err := c.upsertChannel(ctx, ch); err != nil {
				return err
			}
		}
		if next == "" {
			break
		}
		cursor = next
	}
	return nil
}

func (c *Client) upsertChannel(ctx context.Context, ch slack.Channel) error {
	name := strings.TrimSpace(ch.Name)
	kind := "public_channel"
	if ch.IsIM {
		kind = "im"
		name = c.userName(ctx, ch.User)
		if name == "" {
			name = "DM"
		} else {
			name = "@" + name
		}
	} else if ch.IsMpIM {
		kind = "mpim"
		if name == "" {
			name = "Group DM"
		}
	} else if ch.IsPrivate || ch.IsGroup {
		kind = "private_channel"
	}
	if !ch.IsIM && !ch.IsMpIM && name != "" && !strings.HasPrefix(name, "#") {
		name = "#" + name
	}
	if name == "" {
		name = ch.ID
	}
	c.cacheMu.Lock()
	c.channels[ch.ID] = name
	c.cacheMu.Unlock()
	convID := river.SlackConversationID(c.teamID, ch.ID)
	participants, _ := json.Marshal([]map[string]string{{
		"id": ch.ID, "name": name, "kind": kind, "user_id": ch.User,
	}})
	return c.store.UpsertSlackConversation(&db.Conversation{
		ConversationID: convID,
		Name:           name,
		IsGroup:        ch.IsChannel || ch.IsGroup || ch.IsMpIM,
		Participants:   string(participants),
		LastMessageTS:  slackTSToMS(ch.LastRead),
		SourcePlatform: "slack",
		RiverID:        c.riverID,
	})
}

func (c *Client) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Channel list stays cheap (paginated conversations.list) and
			// keeps new channels/renames showing up even while Socket Mode
			// is connected — but the per-channel history resync duplicates
			// what Socket Mode already delivers live, so skip it when the
			// socket is healthy to avoid redundant API traffic.
			if err := c.SyncStreams(ctx); err != nil {
				c.setError(err)
				continue
			}
			if !c.socketHealthy() {
				_ = c.SyncRecentMessages(ctx, 25)
			}
			c.setConnected(true)
		}
	}
}

// socketHealthy reports whether Socket Mode is currently configured and
// connected, meaning live message events are already flowing.
func (c *Client) socketHealthy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.appToken != "" && c.socketConnected
}

// SyncRecentMessages pulls recent history for each Slack stream in the river.
func (c *Client) SyncRecentMessages(ctx context.Context, perChannel int) error {
	if perChannel <= 0 {
		perChannel = 25
	}
	convs, err := c.store.ListConversationsByRiver(c.riverID, 200)
	if err != nil {
		return err
	}
	for _, conv := range convs {
		if conv == nil {
			continue
		}
		_, channelID, ok := river.ParseSlackConversationID(conv.ConversationID)
		if !ok {
			continue
		}
		if err := c.syncChannelHistory(ctx, conv.ConversationID, channelID, perChannel); err != nil {
			// Keep going — one private channel permission failure shouldn't stop the river.
			continue
		}
	}
	return nil
}

func (c *Client) syncChannelHistory(ctx context.Context, conversationID, channelID string, limit int) error {
	cursor, err := c.store.GetSlackSyncCursor(c.riverID, channelID)
	if err != nil {
		return err
	}
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	}
	countUnread := cursor.NewestTS != ""
	if countUnread {
		params.Oldest = cursor.NewestTS
		params.Inclusive = false
	}
	hist, err := c.api.GetConversationHistoryContext(ctx, params)
	if err != nil {
		if wait, limited := rateLimitRetryAfter(err); limited {
			if waitErr := sleepForRetry(ctx, wait); waitErr != nil {
				return waitErr
			}
			hist, err = c.api.GetConversationHistoryContext(ctx, params)
		}
	}
	if err != nil {
		return err
	}
	oldest, newest, changed, err := c.ingestMessages(ctx, conversationID, channelID, hist.Messages, countUnread)
	if err != nil {
		return err
	}
	if oldest != "" || newest != "" {
		if cursor.OldestTS == "" {
			cursor.OldestTS = oldest
		}
		if newest != "" {
			cursor.NewestTS = newest
		}
		if err := c.store.UpsertSlackSyncCursor(cursor); err != nil {
			return err
		}
	}
	if changed {
		c.emitChange(conversationID)
	}
	return nil
}

// SendText posts a message to a Slack stream.
func (c *Client) SendText(ctx context.Context, conversationID, body, replyToID string) (*db.Message, error) {
	_, channelID, ok := river.ParseSlackConversationID(conversationID)
	if !ok {
		return nil, fmt.Errorf("not a slack conversation id")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty message")
	}
	options := []slack.MsgOption{slack.MsgOptionText(body, false)}
	threadTS := slackMessageTS(replyToID)
	if threadTS != "" {
		options = append(options, slack.MsgOptionTS(threadTS))
	}
	_, ts, err := c.api.PostMessageContext(ctx, channelID, options...)
	if err != nil {
		return nil, fmt.Errorf("chat.postMessage: %w", err)
	}
	msg := &db.Message{
		MessageID:      fmt.Sprintf("slack:%s:%s", channelID, ts),
		ConversationID: conversationID,
		Body:           body,
		TimestampMS:    slackTSToMS(ts),
		Status:         "OUTGOING_COMPLETE",
		IsFromMe:       true,
		ReplyToID:      replyToID,
		SourcePlatform: "slack",
		SourceID:       channelID + ":" + ts,
	}
	return msg, nil
}

func (c *Client) ingestMessages(ctx context.Context, conversationID, channelID string, messages []slack.Message, countUnread bool) (oldest, newest string, changed bool, err error) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.TrimSpace(msg.Text) == "" && len(msg.Files) == 0 {
			continue
		}
		rawBody := strings.TrimSpace(msg.Text)
		body := c.readableSlackText(ctx, rawBody)
		if body == "" && len(msg.Files) > 0 {
			body = "[file]"
		}
		senderID := strings.TrimSpace(msg.User)
		if senderID == "" {
			senderID = strings.TrimSpace(msg.BotID)
		}
		senderName := c.userName(ctx, senderID)
		if senderName == senderID && strings.TrimSpace(msg.Username) != "" {
			senderName = strings.TrimSpace(msg.Username)
		}
		messageID := slackMessageID(channelID, msg.Timestamp)
		replyToID := ""
		if msg.ThreadTimestamp != "" && msg.ThreadTimestamp != msg.Timestamp {
			replyToID = slackMessageID(channelID, msg.ThreadTimestamp)
		}
		dbMsg := &db.Message{
			MessageID:      messageID,
			ConversationID: conversationID,
			SenderName:     senderName,
			SenderNumber:   senderID,
			Body:           body,
			TimestampMS:    slackTSToMS(msg.Timestamp),
			Status:         "INCOMING_COMPLETE",
			IsFromMe:       c.userID != "" && senderID == c.userID,
			MentionsMe:     c.mentionsMe(rawBody),
			ReplyToID:      replyToID,
			ReplyCount:     msg.ReplyCount,
			SourcePlatform: "slack",
			SourceID:       channelID + ":" + msg.Timestamp,
		}
		if dbMsg.IsFromMe {
			dbMsg.Status = "OUTGOING_COMPLETE"
		}
		isNew, upsertErr := c.store.UpsertMessageAndReportNew(dbMsg)
		if upsertErr != nil {
			return oldest, newest, changed, upsertErr
		}
		if isNew {
			changed = true
			if countUnread && !dbMsg.IsFromMe {
				if err := c.store.IncrementConversationUnread(conversationID); err != nil {
					return oldest, newest, changed, err
				}
			}
		}
		if oldest == "" || msg.Timestamp < oldest {
			oldest = msg.Timestamp
		}
		if newest == "" || msg.Timestamp > newest {
			newest = msg.Timestamp
		}
		if dbMsg.TimestampMS > 0 {
			if err := c.store.BumpConversationTimestamp(conversationID, dbMsg.TimestampMS); err != nil {
				return oldest, newest, changed, err
			}
		}
	}
	return oldest, newest, changed, nil
}

// SyncOlderHistory fetches one older page for the active Slack stream.
func (c *Client) SyncOlderHistory(ctx context.Context, conversationID string, limit int) ([]*db.Message, error) {
	_, channelID, ok := river.ParseSlackConversationID(conversationID)
	if !ok {
		return nil, fmt.Errorf("not a slack conversation id")
	}
	if limit <= 0 {
		limit = 100
	}
	cursor, err := c.store.GetSlackSyncCursor(c.riverID, channelID)
	if err != nil {
		return nil, err
	}
	hist, err := c.api.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    cursor.OldestTS,
		Inclusive: false,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	oldest, _, changed, err := c.ingestMessages(ctx, conversationID, channelID, hist.Messages, false)
	if err != nil {
		return nil, err
	}
	if oldest != "" {
		cursor.OldestTS = oldest
		if err := c.store.UpsertSlackSyncCursor(cursor); err != nil {
			return nil, err
		}
	}
	if changed {
		c.emitChange(conversationID)
	}
	return c.store.GetMessages(conversationID, 0, 0, 1000)
}

// FetchThread refreshes and returns a Slack root message plus its replies.
func (c *Client) FetchThread(ctx context.Context, conversationID, rootMessageID string) ([]*db.Message, error) {
	_, channelID, ok := river.ParseSlackConversationID(conversationID)
	if !ok {
		return nil, fmt.Errorf("not a slack conversation id")
	}
	rootTS := slackMessageTS(rootMessageID)
	if rootTS == "" {
		return nil, fmt.Errorf("invalid Slack root message id")
	}
	var all []slack.Message
	cursor := ""
	for {
		repliesParams := &slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: rootTS,
			Cursor:    cursor,
			Limit:     100,
		}
		msgs, hasMore, next, err := c.api.GetConversationRepliesContext(ctx, repliesParams)
		if err != nil {
			if wait, limited := rateLimitRetryAfter(err); limited {
				if waitErr := sleepForRetry(ctx, wait); waitErr != nil {
					return nil, waitErr
				}
				msgs, hasMore, next, err = c.api.GetConversationRepliesContext(ctx, repliesParams)
			}
		}
		if err != nil {
			return nil, err
		}
		all = append(all, msgs...)
		if !hasMore || next == "" {
			break
		}
		cursor = next
	}
	_, _, changed, err := c.ingestMessages(ctx, conversationID, channelID, all, false)
	if err != nil {
		return nil, err
	}
	if changed {
		c.emitChange(conversationID)
	}
	return c.store.ListThreadMessages(conversationID, slackMessageID(channelID, rootTS))
}

func slackMessageID(channelID, ts string) string {
	if channelID == "" || ts == "" {
		return ""
	}
	return fmt.Sprintf("slack:%s:%s", channelID, ts)
}

func slackMessageTS(messageID string) string {
	parts := strings.SplitN(messageID, ":", 3)
	if len(parts) != 3 || parts[0] != "slack" {
		return ""
	}
	return parts[2]
}

func slackTSToMS(ts string) int64 {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return 0
	}
	var sec float64
	if _, err := fmt.Sscanf(ts, "%f", &sec); err != nil {
		return time.Now().UnixMilli()
	}
	return int64(sec * 1000)
}

// MarshalCredentials encodes Slack vault credentials.
func MarshalCredentials(c Credentials) ([]byte, error) {
	return json.Marshal(c)
}

// UnmarshalCredentials decodes Slack vault credentials.
func UnmarshalCredentials(raw []byte) (Credentials, error) {
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return Credentials{}, err
	}
	return c, nil
}
