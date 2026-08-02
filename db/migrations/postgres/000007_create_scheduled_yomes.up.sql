CREATE TABLE IF NOT EXISTS scheduled_yomes (
    id SERIAL PRIMARY KEY,
    guild_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    run_at TIMESTAMP WITH TIME ZONE NOT NULL,
    count INTEGER NOT NULL DEFAULT 1,
    requester_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_scheduled_yomes_status_run_at ON scheduled_yomes(status, run_at);
CREATE INDEX IF NOT EXISTS idx_scheduled_yomes_channel_status ON scheduled_yomes(channel_id, status);
