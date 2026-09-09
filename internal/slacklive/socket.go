package slacklive

import (
	"context"
	"fmt"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/maxghenis/openmessage/internal/river"
)

const (
	socketReconnectMinBackoff = time.Second
	socketReconnectMaxBackoff = 30 * time.Second
)

// socketLoop keeps a Socket Mode session alive for the life of ctx,
// reconnecting with exponential backoff (capped at socketReconnectMaxBackoff)
// whenever a session ends. Without this, a single dropped connection would
// silently fall back to 45s polling forever until the process restarts.
func (c *Client) socketLoop(ctx context.Context) {
	backoff := socketReconnectMinBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		connected := c.runSocketSession(ctx)
		if ctx.Err() != nil {
			return
		}
		if connected {
			backoff = socketReconnectMinBackoff
		}
		if err := sleepForRetry(ctx, backoff); err != nil {
			return
		}
		backoff *= 2
		if backoff > socketReconnectMaxBackoff {
			backoff = socketReconnectMaxBackoff
		}
	}
}

// runSocketSession runs a single Socket Mode session until it ends (fatal
// error, closed event channel, or ctx cancellation). It reports whether the
// session ever reached EventTypeConnected, so the caller can reset backoff
// after a session that connected successfully before later dropping.
func (c *Client) runSocketSession(ctx context.Context) (connected bool) {
	api := slack.New(c.token, slack.OptionAppLevelToken(c.appToken))
	socket := socketmode.New(api)
	go func() {
		if err := socket.RunContext(ctx); err != nil && ctx.Err() == nil {
			c.setError(fmt.Errorf("Slack Socket Mode: %w", err))
		}
	}()

	defer func() {
		c.mu.Lock()
		c.socketConnected = false
		c.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return connected
		case event, ok := <-socket.Events:
			if !ok {
				return connected
			}
			switch event.Type {
			case socketmode.EventTypeConnected:
				connected = true
				c.mu.Lock()
				c.socketConnected = true
				c.mu.Unlock()
			case socketmode.EventTypeConnectionError, socketmode.EventTypeInvalidAuth:
				c.mu.Lock()
				c.socketConnected = false
				c.mu.Unlock()
			case socketmode.EventTypeEventsAPI:
				if event.Request != nil {
					if err := socket.Ack(*event.Request); err != nil {
						c.setError(fmt.Errorf("Slack Socket Mode ack: %w", err))
					}
				}
				apiEvent, ok := event.Data.(slackevents.EventsAPIEvent)
				if !ok || apiEvent.Type != slackevents.CallbackEvent {
					continue
				}
				switch inner := apiEvent.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					c.ingestSocketMessage(ctx, inner)
				}
			}
		}
	}
}

func (c *Client) ingestSocketMessage(ctx context.Context, event *slackevents.MessageEvent) {
	if event == nil || event.Channel == "" {
		return
	}
	conversationID := river.SlackConversationID(c.teamID, event.Channel)
	if event.SubType == "message_deleted" {
		ts := event.DeletedTimeStamp
		if ts == "" && event.PreviousMessage != nil {
			ts = event.PreviousMessage.Timestamp
		}
		if ts != "" {
			_ = c.store.DeleteMessageByID(slackMessageID(event.Channel, ts))
			c.emitChange(conversationID)
		}
		return
	}

	var msg slack.Message
	if event.Message != nil {
		msg.Msg = *event.Message
	} else {
		msg.Msg = slack.Msg{
			User:            event.User,
			Text:            event.Text,
			Timestamp:       event.TimeStamp,
			ThreadTimestamp: event.ThreadTimeStamp,
			BotID:           event.BotID,
			Username:        event.Username,
		}
	}
	if msg.Timestamp == "" {
		return
	}
	_, newest, changed, err := c.ingestMessages(ctx, conversationID, event.Channel, []slack.Message{msg}, true)
	if err != nil {
		c.setError(err)
		return
	}
	if newest != "" {
		cursor, _ := c.store.GetSlackSyncCursor(c.riverID, event.Channel)
		cursor.NewestTS = newest
		_ = c.store.UpsertSlackSyncCursor(cursor)
	}
	if changed || event.SubType == "message_changed" {
		c.emitChange(conversationID)
	}
}
