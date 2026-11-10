package notify

import (
	"os/exec"
	"strings"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
)

type LinuxNotifier struct {
	logger       zerolog.Logger
	enabled      bool
	store        *db.Store
	mentionNames []string
	run          commandRunner
	history      idHistory
}

func NewLinuxNotifier(logger zerolog.Logger, enabled bool, store *db.Store, identityName string) *LinuxNotifier {
	return &LinuxNotifier{
		logger:       logger,
		enabled:      enabled,
		store:        store,
		mentionNames: mentionNamesForIdentity(identityName),
		run: func(name string, args ...string) error {
			return exec.Command(name, args...).Run()
		},
		history: newIDHistory(),
	}
}

func (n *LinuxNotifier) Enabled() bool {
	return n != nil && n.enabled
}

func (n *LinuxNotifier) NotifyIncomingMessage(message *db.Message) {
	if n == nil || !n.enabled || message == nil || message.IsFromMe {
		return
	}
	if !messageFreshForNotification(message) {
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
		if err := n.notify(title, body); err != nil {
			n.logger.Debug().Err(err).Str("msg_id", messageID).Msg("Linux notification failed")
		}
	}()
}

func (n *LinuxNotifier) notify(title, body string) error {
	args := []string{
		"-a", "OM-TUI",
		"-c", "im.received",
		"-i", "mail-unread",
		title,
		body,
	}
	return n.run("notify-send", args...)
}
