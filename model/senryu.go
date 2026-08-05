package model

import "time"

// Senryu is struct of senryu.
type Senryu struct {
	ID              int       `gorm:"primaryKey;autoIncrement"`
	ServerID        string    `gorm:"column:server_id;index"`
	AuthorID        string    `gorm:"column:author_id;index"`
	Kamigo          string    `gorm:"column:kamigo"`
	Nakasichi       string    `gorm:"column:nakasichi"`
	Simogo          string    `gorm:"column:simogo"`
	Spoiler         *bool     `gorm:"column:spoiler;not null"`
	SourceMessageID string    `gorm:"column:source_message_id"` // Bot detection reply ID (import dedup)
	CreatedAt       time.Time `gorm:"column:created_at"`
}

// MutedChannel is struct of muted channel.
type MutedChannel struct {
	ChannelID string `gorm:"primaryKey"`
	GuildID   string `gorm:"column:guild_id;index"`
}

// GuildChannelTypeSetting stores per-guild channel type overrides.
// Only rows that differ from the default are stored.
type GuildChannelTypeSetting struct {
	GuildID     string `gorm:"primaryKey;column:guild_id"`
	ChannelType int    `gorm:"primaryKey;column:channel_type"`
	Enabled     bool   `gorm:"column:enabled"`
}

// DetectionOptOut is struct of per-user detection opt-out.
type DetectionOptOut struct {
	ServerID string `gorm:"primaryKey"`
	UserID   string `gorm:"primaryKey"`
	SetBy    string `gorm:"column:set_by;not null;default:'self'"`
}

// Metadata is a key-value store for bot-wide settings.
type Metadata struct {
	Key   string `gorm:"primaryKey;column:key"`
	Value string `gorm:"column:value;not null"`
}

// Yome kind values.
const (
	YomeKindSenryu = "senryu"
	YomeKindTanka  = "tanka"
)

// YomeEvent records one successful bot yome (senryu or tanka) send.
type YomeEvent struct {
	ID            int       `gorm:"primaryKey;autoIncrement"`
	ServerID      string    `gorm:"column:server_id;index"`
	ChannelID     string    `gorm:"column:channel_id"`
	MessageID     string    `gorm:"column:message_id"`
	RequesterID   string    `gorm:"column:requester_id;index"`
	Kind          string    `gorm:"column:kind"`
	Kamigo        string    `gorm:"column:kamigo"`
	Nakasichi     string    `gorm:"column:nakasichi"`
	Simogo        string    `gorm:"column:simogo"`
	Nanaichi      string    `gorm:"column:nanaichi"`
	Nananichi     string    `gorm:"column:nananichi"`
	ReactionCount int       `gorm:"column:reaction_count;not null;default:0"`
	CreatedAt     time.Time `gorm:"column:created_at;index"`
}

// TableName returns the table name for YomeEvent.
func (YomeEvent) TableName() string {
	return "yome_events"
}

// ScheduledYome status values.
const (
	ScheduledYomePending   = "pending"
	ScheduledYomeDone      = "done"
	ScheduledYomeCancelled = "cancelled"
)

// ScheduledYome is a one-shot timed yome reservation.
type ScheduledYome struct {
	ID          int       `gorm:"primaryKey;autoIncrement"`
	GuildID     string    `gorm:"column:guild_id"`
	ChannelID   string    `gorm:"column:channel_id;index"`
	RunAt       time.Time `gorm:"column:run_at;index"`
	Count       int       `gorm:"column:count"`
	RequesterID string    `gorm:"column:requester_id"`
	Status      string    `gorm:"column:status;index"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

// TableName returns the table name for ScheduledYome.
func (ScheduledYome) TableName() string {
	return "scheduled_yomes"
}
