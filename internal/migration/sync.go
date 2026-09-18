package migration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/storage/sqlite"
)

// SyncInto copies one legacy platform from sourcePath into an already-opened
// v2 store. People/person_identities are skipped because CreatePerson is
// INSERT-only. Live Slack/Google/WhatsApp/Signal conversations are left alone
// unless they belong to the requested platform.
func SyncInto(ctx context.Context, sourcePath string, target *sqlite.Store, platform string) error {
	if target == nil {
		return fmt.Errorf("sync into v2: store is nil")
	}
	platform = normalizeLegacyPlatform(platform)
	if platform == "" {
		return fmt.Errorf("sync into v2: platform is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dataset, err := loadLegacyDatasetForSync(ctx, sourcePath)
	if err != nil {
		return err
	}
	dataset = filterLegacyDataset(dataset, platform)
	if len(dataset.conversations) == 0 && len(dataset.messages) == 0 {
		return nil
	}
	report := Report{}
	state, err := buildTransformState(dataset, &report)
	if err != nil {
		return fmt.Errorf("sync into v2: plan: %w", err)
	}
	clockMS := state.baseTimestampMS
	now := func() time.Time { return time.UnixMilli(clockMS) }
	messages, err := sqlite.NewMessageRepository(target, now)
	if err != nil {
		return fmt.Errorf("sync into v2: messages: %w", err)
	}
	reactions, err := sqlite.NewReactionRepository(target, now)
	if err != nil {
		return fmt.Errorf("sync into v2: reactions: %w", err)
	}
	if err := writeAccounts(target, state); err != nil {
		return fmt.Errorf("sync into v2: %w", err)
	}
	if err := writeIdentities(target, state); err != nil {
		return fmt.Errorf("sync into v2: %w", err)
	}
	if err := writeConversations(target, state); err != nil {
		return fmt.Errorf("sync into v2: %w", err)
	}
	if err := writeHistory(ctx, target, messages, dataset, state, &clockMS, &report); err != nil {
		return fmt.Errorf("sync into v2: %w", err)
	}
	if err := writeReactions(ctx, reactions, state, &report); err != nil {
		return fmt.Errorf("sync into v2: %w", err)
	}
	return nil
}

func filterLegacyDataset(dataset legacyDataset, platform string) legacyDataset {
	platform = normalizeLegacyPlatform(platform)
	keep := map[string]bool{}
	conversations := make([]legacyConversation, 0, len(dataset.conversations))
	for _, conv := range dataset.conversations {
		if normalizeLegacyPlatform(conv.Platform) == platform {
			conversations = append(conversations, conv)
			keep[conv.ID] = true
		}
	}
	messages := make([]legacyMessage, 0, len(dataset.messages))
	platformMessages := map[string]int64{}
	var mediaRows, reactionsBearing, transcriptRows int64
	for _, msg := range dataset.messages {
		if !keep[msg.ConversationID] {
			continue
		}
		messages = append(messages, msg)
		platformMessages[platform]++
		if strings.TrimSpace(msg.MediaID) != "" {
			mediaRows++
		}
		if strings.TrimSpace(msg.Reactions) != "" &&
			strings.TrimSpace(msg.Reactions) != "null" &&
			strings.TrimSpace(msg.Reactions) != "[]" {
			reactionsBearing++
		}
		if strings.TrimSpace(msg.Transcript) != "" || msg.TranscribedAtMS != 0 ||
			strings.TrimSpace(msg.TranscriptModel) != "" {
			transcriptRows++
		}
	}
	dataset.conversations = conversations
	dataset.messages = messages
	dataset.contacts = nil
	dataset.unified = nil
	dataset.scheduled = nil
	dataset.platformMessages = platformMessages
	dataset.mediaRows = mediaRows
	dataset.reactionsBearingMessages = reactionsBearing
	dataset.transcriptRows = transcriptRows
	dataset.dropped = DroppedDimensions{}
	return dataset
}

func loadLegacyDatasetForSync(ctx context.Context, sourcePath string) (legacyDataset, error) {
	database, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		return legacyDataset{}, fmt.Errorf("sync into v2: open legacy store: %w", err)
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		return legacyDataset{}, fmt.Errorf("sync into v2: busy timeout: %w", err)
	}
	if err := database.PingContext(ctx); err != nil {
		return legacyDataset{}, fmt.Errorf("sync into v2: connect to legacy store: %w", err)
	}
	if err := validateLegacySchema(ctx, database); err != nil {
		return legacyDataset{}, fmt.Errorf("sync into v2: %w", err)
	}
	dataset := legacyDataset{
		tableCounts: map[string]int64{},
		scheduleByStatus: map[string]int64{
			"pending": 0, "sending": 0, "sent": 0, "failed": 0, "canceled": 0,
		},
		platformMessages: map[string]int64{},
	}
	if err := loadLegacyRows(ctx, database, &dataset); err != nil {
		return legacyDataset{}, fmt.Errorf("sync into v2: %w", err)
	}
	if err := validateLegacyRelationships(&dataset); err != nil {
		return legacyDataset{}, fmt.Errorf("sync into v2: %w", err)
	}
	return dataset, nil
}
