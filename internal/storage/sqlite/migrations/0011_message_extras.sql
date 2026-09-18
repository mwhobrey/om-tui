-- Optional per-message payload that must not live on frozen messages.db:
-- Slack Block Kit JSON for V2-only TUI layout, and voice transcripts.
CREATE TABLE message_extras (
    message_id TEXT PRIMARY KEY CHECK (trim(message_id) <> ''),
    payload_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload_json)),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms > 0),
    FOREIGN KEY (message_id) REFERENCES messages(message_id) ON DELETE CASCADE
) STRICT;