package slacklive

import (
	"context"
	"io"

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
	AddReactionContext(context.Context, string, slack.ItemRef) error
	RemoveReactionContext(context.Context, string, slack.ItemRef) error
	GetFileInfoContext(context.Context, string, int, int) (*slack.File, []slack.Comment, *slack.Paging, error)
	GetFileContext(context.Context, string, io.Writer) error
	GetUploadURLExternalContext(context.Context, slack.GetUploadURLExternalParameters) (*slack.GetUploadURLExternalResponse, error)
	UploadToURL(context.Context, slack.UploadToURLParameters) error
	CompleteUploadExternalContext(context.Context, slack.CompleteUploadExternalParameters) (*slack.CompleteUploadExternalResponse, error)
}
