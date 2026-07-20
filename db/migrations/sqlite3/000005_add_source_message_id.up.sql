ALTER TABLE senryus ADD COLUMN source_message_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_senryus_source_message_id
  ON senryus(source_message_id)
  WHERE source_message_id IS NOT NULL AND source_message_id != '';
