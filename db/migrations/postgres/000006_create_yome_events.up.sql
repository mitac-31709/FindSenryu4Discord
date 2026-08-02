CREATE TABLE IF NOT EXISTS yome_events (
    id SERIAL PRIMARY KEY,
    server_id TEXT,
    created_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_yome_events_server_id ON yome_events(server_id);
CREATE INDEX IF NOT EXISTS idx_yome_events_created_at ON yome_events(created_at);
