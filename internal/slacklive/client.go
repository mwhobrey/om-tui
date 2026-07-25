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
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
}

type Client struct {
	api      *slack.Client
	store    *db.Store
	riverID  string
	teamID   string
	teamName string
	token    string
	userID   string

	mu        sync.Mutex
	cancel    context.CancelFunc
	connected bool
	lastError string
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

func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	if err := c.SyncStreams(runCtx); err != nil {
		c.setError(err)
		return err
	}
	c.setConnected(true)
	go c.pollLoop(runCtx)
	return nil
}

func (c *Client) Stop() {
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.connected = false
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
			return fmt.Errorf("conversations.list: %w", err)
		}
		for _, ch := range channels {
			if err := c.upsertChannel(ch); err != nil {
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

func (c *Client) upsertChannel(ch slack.Channel) error {
	name := strings.TrimSpace(ch.Name)
	if ch.IsIM {
		name = strings.TrimSpace(ch.User)
		if name == "" {
			name = "DM"
		} else {
			name = "@" + name
		}
	} else if ch.IsMpIM {
		if name == "" {
			name = "Group DM"
		}
	} else if name != "" && !strings.HasPrefix(name, "#") {
		name = "#" + name
	}
	if name == "" {
		name = ch.ID
	}
	convID := river.SlackConversationID(c.teamID, ch.ID)
	participants, _ := json.Marshal([]map[string]string{{"id": ch.ID, "name": name}})
	return c.store.UpsertConversation(&db.Conversation{
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
			if err := c.SyncStreams(ctx); err != nil {
				c.setError(err)
				continue
			}
			_ = c.SyncRecentMessages(ctx, 25)
			c.setConnected(true)
		}
	}
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
	hist, err := c.api.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	})
	if err != nil {
		return err
	}
	var latest int64
	for i := len(hist.Messages) - 1; i >= 0; i-- {
		msg := hist.Messages[i]
		if strings.TrimSpace(msg.Text) == "" && len(msg.Files) == 0 {
			continue
		}
		ts := slackTSToMS(msg.Timestamp)
		if ts > latest {
			latest = ts
		}
		body := strings.TrimSpace(msg.Text)
		if body == "" && len(msg.Files) > 0 {
			body = "[file]"
		}
		dbMsg := &db.Message{
			MessageID:      fmt.Sprintf("slack:%s:%s", channelID, msg.Timestamp),
			ConversationID: conversationID,
			SenderName:     msg.User,
			SenderNumber:   msg.User,
			Body:           body,
			TimestampMS:    ts,
			Status:         "INCOMING_COMPLETE",
			IsFromMe:       c.userID != "" && msg.User == c.userID,
			SourcePlatform: "slack",
			SourceID:       msg.Timestamp,
		}
		if dbMsg.IsFromMe {
			dbMsg.Status = "OUTGOING_COMPLETE"
		}
		_ = c.store.UpsertMessage(dbMsg)
	}
	if latest > 0 {
		_ = c.store.BumpConversationTimestamp(conversationID, latest)
	}
	return nil
}

// SendText posts a message to a Slack stream.
func (c *Client) SendText(ctx context.Context, conversationID, body string) (*db.Message, error) {
	_, channelID, ok := river.ParseSlackConversationID(conversationID)
	if !ok {
		return nil, fmt.Errorf("not a slack conversation id")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty message")
	}
	_, ts, err := c.api.PostMessageContext(ctx, channelID, slack.MsgOptionText(body, false))
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
		SourcePlatform: "slack",
		SourceID:       ts,
	}
	return msg, nil
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
