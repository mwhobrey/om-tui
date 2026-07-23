package notify

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
)

type commandRunner func(name string, args ...string) error

type MacOSNotifier struct {
	logger               zerolog.Logger
	enabled              bool
	store                *db.Store
	mentionNames         []string
	run                  commandRunner
	baseURL              string
	terminalNotifierPath string
	history              idHistory
}

func NewMacOSNotifier(logger zerolog.Logger, enabled bool, baseURL string, store *db.Store, identityName string) *MacOSNotifier {
	notifier := &MacOSNotifier{
		logger:       logger,
		enabled:      enabled,
		store:        store,
		mentionNames: mentionNamesForIdentity(identityName),
		run: func(name string, args ...string) error {
			return exec.Command(name, args...).Run()
		},
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		history: newIDHistory(),
	}
	if enabled {
		if path, err := exec.LookPath("terminal-notifier"); err == nil {
			notifier.terminalNotifierPath = path
		}
	}
	return notifier
}

func (n *MacOSNotifier) Enabled() bool {
	return n != nil && n.enabled
}

func (n *MacOSNotifier) NotifyIncomingMessage(message *db.Message) {
	if n == nil || !n.enabled || message == nil || message.IsFromMe {
		return
	}
	if !notificationAllowed(n.store, n.mentionNames, message) {
		return
	}
	messageID := strings.TrimSpace(message.MessageID)
	if messageID == "" || !n.history.remember(messageID) {
		return
	}

	title := notificationTitle(message)
	body := notificationBody(message)

	go func() {
		if err := n.notify(title, body, messageID, message.ConversationID); err != nil {
			n.logger.Debug().Err(err).Str("msg_id", messageID).Msg("macOS notification failed")
		}
	}()
}

func (n *MacOSNotifier) notify(title, body, messageID, conversationID string) error {
	if n.terminalNotifierPath != "" {
		args := []string{
			"-title", title,
			"-subtitle", "OpenMessage",
			"-message", body,
			"-group", "openmessage:" + messageID,
		}
		if openURL := n.openURL(conversationID); openURL != "" {
			args = append(args, "-open", openURL)
		}
		return n.run(n.terminalNotifierPath, args...)
	}
	return n.run("osascript", "-e", appleScriptNotification(title, body))
}

func (n *MacOSNotifier) openURL(conversationID string) string {
	if n == nil || n.baseURL == "" {
		return ""
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return n.baseURL + "/"
	}
	return n.baseURL + "/?conversation=" + url.QueryEscape(conversationID)
}

func appleScriptNotification(title, body string) string {
	return fmt.Sprintf(
		"display notification %s with title %s",
		appleScriptString(body),
		appleScriptString(title),
	)
}

func appleScriptString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(value) + `"`
}
