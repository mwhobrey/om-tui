-- PRIMARY leftovers that must not land in frozen messages.db:
-- MCP/API drafts, custom conversation tabs, and contact CRM metadata.
CREATE TABLE drafts (
    draft_id TEXT PRIMARY KEY CHECK (trim(draft_id) <> ''),
    conversation_id TEXT NOT NULL CHECK (trim(conversation_id) <> ''),
    body TEXT NOT NULL DEFAULT '',
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= created_at_ms),
    FOREIGN KEY (conversation_id) REFERENCES conversations(conversation_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX drafts_conversation_idx
    ON drafts(conversation_id, created_at_ms DESC);

CREATE TABLE tabs (
    tab_id TEXT PRIMARY KEY CHECK (trim(tab_id) <> ''),
    name TEXT NOT NULL CHECK (trim(name) <> ''),
    position INTEGER NOT NULL DEFAULT 0,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0)
) STRICT;

CREATE TABLE contact_meta (
    person_key TEXT PRIMARY KEY CHECK (trim(person_key) <> ''),
    display_name TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags_json)),
    reach_out_days INTEGER NOT NULL DEFAULT 0 CHECK (reach_out_days >= 0),
    summary TEXT NOT NULL DEFAULT '',
    summary_at_ms INTEGER NOT NULL DEFAULT 0 CHECK (summary_at_ms >= 0),
    updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms > 0)
) STRICT;
