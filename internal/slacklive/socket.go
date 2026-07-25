package slacklive

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/maxghenis/openmessage/internal/river"
)

func (c *Client) socketLoop(ctx context.Context) {
	api := slack.New(c.token, slack.OptionAppLevelToken(c.appToken))
	socket := socketmode.New(api)
	go func() {
		if err := socket.RunContext(ctx); err != nil && ctx.Err() == nil {
			c.setError(fmt.Errorf("Slack Socket Mode: %w", err))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-socket.Events:
			if !ok {
				return
			}
			switch event.Type {
			case socketmode.EventTypeConnected:
				c.mu.Lock()
				c.socketConnected = true
				c.mu.Unlock()
			case socketmode.EventTypeConnectionError, socketmode.EventTypeInvalidAuth:
				c.mu.Lock()
				c.socketConnected = false
				c.mu.Unlock()
			case socketmode.EventTypeEventsAPI:
				if event.Request != nil {
					socket.Ack(*event.Request)
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
