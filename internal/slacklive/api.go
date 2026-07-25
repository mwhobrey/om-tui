package slacklive

import (
	"context"

	"github.com/slack-go/slack"
)

type slackAPI interface {
	AuthTest() (*slack.AuthTestResponse, error)
	GetConversationsContext(context.Context, *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	GetConversationHistoryContext(context.Context, *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationRepliesContext(context.Context, *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersContext(context.Context, ...slack.GetUsersOption) ([]slack.User, error)
	GetUserInfoContext(context.Context, string) (*slack.User, error)
	PostMessageContext(context.Context, string, ...slack.MsgOption) (string, string, error)
}
