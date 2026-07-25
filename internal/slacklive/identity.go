package slacklive

import (
	"context"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/maxghenis/openmessage/internal/db"
)

var (
	slackUserMentionRE = regexp.MustCompile(`<@([A-Z0-9]+)>`)
	slackChannelRE     = regexp.MustCompile(`<#([A-Z0-9]+)(?:\|([^>]+))?>`)
	slackLinkRE        = regexp.MustCompile(`<((?:https?|mailto):[^>|]+)(?:\|([^>]+))?>`)
)

func (c *Client) loadCachedUsers() {
	users, err := c.store.ListSlackUsers(c.riverID)
	if err != nil {
		return
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	for _, user := range users {
		c.users[user.UserID] = user
	}
}

func (c *Client) SyncUsers(ctx context.Context) error {
	users, err := c.api.GetUsersContext(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, user := range users {
		c.cacheUser(user, now)
	}
	return nil
}

func (c *Client) cacheUser(user slack.User, now int64) {
	entry := db.SlackUser{
		RiverID:     c.riverID,
		UserID:      user.ID,
		DisplayName: strings.TrimSpace(user.Profile.DisplayName),
		RealName:    strings.TrimSpace(user.Profile.RealName),
		IsDeleted:   user.Deleted,
		IsBot:       user.IsBot,
		UpdatedAtMS: now,
	}
	if entry.RealName == "" {
		entry.RealName = strings.TrimSpace(user.RealName)
	}
	if entry.UserID == "" {
		return
	}
	c.cacheMu.Lock()
	c.users[entry.UserID] = entry
	c.cacheMu.Unlock()
	_ = c.store.UpsertSlackUser(entry)
}

func (c *Client) userName(ctx context.Context, userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	c.cacheMu.RLock()
	user, ok := c.users[userID]
	c.cacheMu.RUnlock()
	if ok && user.Name() != "" {
		return user.Name()
	}
	if remote, err := c.api.GetUserInfoContext(ctx, userID); err == nil && remote != nil {
		c.cacheUser(*remote, time.Now().UnixMilli())
		c.cacheMu.RLock()
		user = c.users[userID]
		c.cacheMu.RUnlock()
		if user.Name() != "" {
			return user.Name()
		}
	}
	return userID
}

func (c *Client) readableSlackText(ctx context.Context, raw string) string {
	text := slackUserMentionRE.ReplaceAllStringFunc(raw, func(token string) string {
		match := slackUserMentionRE.FindStringSubmatch(token)
		return "@" + c.userName(ctx, match[1])
	})
	text = slackChannelRE.ReplaceAllStringFunc(text, func(token string) string {
		match := slackChannelRE.FindStringSubmatch(token)
		if len(match) > 2 && strings.TrimSpace(match[2]) != "" {
			return "#" + match[2]
		}
		c.cacheMu.RLock()
		name := c.channels[match[1]]
		c.cacheMu.RUnlock()
		if name != "" {
			return name
		}
		return "#" + match[1]
	})
	text = slackLinkRE.ReplaceAllStringFunc(text, func(token string) string {
		match := slackLinkRE.FindStringSubmatch(token)
		if len(match) > 2 && strings.TrimSpace(match[2]) != "" {
			return match[2] + " (" + match[1] + ")"
		}
		return match[1]
	})
	return strings.TrimSpace(html.UnescapeString(text))
}

func (c *Client) mentionsMe(raw string) bool {
	return c.userID != "" && strings.Contains(raw, "<@"+c.userID+">")
}
