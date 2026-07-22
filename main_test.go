package main

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/service"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	var err error
	db.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.DB.AutoMigrate(&model.MutedChannel{}, &model.GuildChannelTypeSetting{}).Error; err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Close()
	})
}

func TestIsChannelTypeEnabled_デフォルト有効タイプ(t *testing.T) {
	setupTestDB(t)

	enabledTypes := []discordgo.ChannelType{
		discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildVoice,
		discordgo.ChannelTypeGuildStageVoice,
		discordgo.ChannelTypeGuildNewsThread,
		discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread,
	}

	for _, ct := range enabledTypes {
		if !service.IsChannelTypeEnabled("test-guild", ct) {
			t.Errorf("channel type %d should be enabled by default", ct)
		}
	}
}

func TestIsChannelTypeEnabled_デフォルト無効タイプ(t *testing.T) {
	setupTestDB(t)

	disabledTypes := []discordgo.ChannelType{
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildForum,
	}

	for _, ct := range disabledTypes {
		if service.IsChannelTypeEnabled("test-guild", ct) {
			t.Errorf("channel type %d should be disabled by default", ct)
		}
	}
}

func TestIsChannelTypeEnabled_未知のタイプは無効(t *testing.T) {
	setupTestDB(t)

	if service.IsChannelTypeEnabled("test-guild", discordgo.ChannelType(999)) {
		t.Error("unknown channel type should be disabled")
	}
}

func TestIsParentChannelMuted_親チャンネルがミュート(t *testing.T) {
	setupTestDB(t)

	if err := service.ToMute("parent-channel", "test-guild"); err != nil {
		t.Fatalf("failed to mute parent channel: %v", err)
	}

	ch := &discordgo.Channel{ParentID: "parent-channel"}
	if !isParentChannelMuted(ch) {
		t.Error("should detect parent channel as muted")
	}
}

func TestIsParentChannelMuted_親チャンネルがミュートされていない(t *testing.T) {
	setupTestDB(t)

	ch := &discordgo.Channel{ParentID: "unmuted-parent"}
	if isParentChannelMuted(ch) {
		t.Error("should not detect unmuted parent channel as muted")
	}
}

func TestIsParentChannelMuted_親チャンネルなし(t *testing.T) {
	setupTestDB(t)

	ch := &discordgo.Channel{ParentID: ""}
	if isParentChannelMuted(ch) {
		t.Error("channel with no parent should not be considered muted")
	}
}

func TestIsParentChannelMuted_自チャンネルのミュートは親に影響しない(t *testing.T) {
	setupTestDB(t)

	if err := service.ToMute("thread-channel", "test-guild"); err != nil {
		t.Fatalf("failed to mute thread channel: %v", err)
	}

	ch := &discordgo.Channel{
		ID:       "thread-channel",
		ParentID: "other-parent",
	}
	if isParentChannelMuted(ch) {
		t.Error("muting the thread itself should not affect parent mute check")
	}
}

func TestParseYomeCount(t *testing.T) {
	const max = 10
	tests := []struct {
		content     string
		wantCount   int
		wantOK      bool
		wantOutRange bool
	}{
		{content: "詠め", wantCount: 1, wantOK: true},
		{content: "5回詠め", wantCount: 5, wantOK: true},
		{content: "10回詠め", wantCount: 10, wantOK: true},
		{content: "11回詠め", wantCount: 11, wantOK: true, wantOutRange: true},
		{content: "0回詠め", wantCount: 0, wantOK: true, wantOutRange: true},
		{content: "回詠め", wantOK: false},
		{content: "詠むな", wantOK: false},
		{content: "あ回詠め", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			count, ok, outOfRange := parseYomeCount(tt.content, max)
			if ok != tt.wantOK || outOfRange != tt.wantOutRange || count != tt.wantCount {
				t.Errorf("parseYomeCount(%q) = (%d, %v, %v), want (%d, %v, %v)",
					tt.content, count, ok, outOfRange, tt.wantCount, tt.wantOK, tt.wantOutRange)
			}
		})
	}
}
