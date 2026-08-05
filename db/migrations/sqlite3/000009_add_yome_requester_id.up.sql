ALTER TABLE yome_events ADD COLUMN requester_id TEXT;

CREATE INDEX IF NOT EXISTS idx_yome_events_requester_id ON yome_events(requester_id);
