ALTER TABLE yome_events ADD COLUMN channel_id TEXT;
ALTER TABLE yome_events ADD COLUMN message_id TEXT;
ALTER TABLE yome_events ADD COLUMN kind TEXT;
ALTER TABLE yome_events ADD COLUMN kamigo TEXT;
ALTER TABLE yome_events ADD COLUMN nakasichi TEXT;
ALTER TABLE yome_events ADD COLUMN simogo TEXT;
ALTER TABLE yome_events ADD COLUMN nanaichi TEXT;
ALTER TABLE yome_events ADD COLUMN nananichi TEXT;
ALTER TABLE yome_events ADD COLUMN reaction_count INTEGER NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX IF NOT EXISTS idx_yome_events_message_id ON yome_events(message_id);
