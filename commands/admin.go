package commands

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/u16-io/FindSenryu4Discord/config"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/pkg/backup"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/pkg/permissions"
	"github.com/u16-io/FindSenryu4Discord/service"
)

var (
	backupManager *backup.Manager
	startTime     time.Time
	allSessions   []*discordgo.Session
	importMu      sync.Mutex
)

// SetBackupManager sets the backup manager for admin commands
func SetBackupManager(m *backup.Manager) {
	backupManager = m
}

// SetStartTime sets the start time for uptime calculation
func SetStartTime(t time.Time) {
	startTime = t
}

// SetAllSessions sets all shard sessions for cross-shard guild counting
func SetAllSessions(sessions []*discordgo.Session) {
	allSessions = sessions
}

// allGuilds returns guilds from all shard sessions
func allGuilds() []*discordgo.Guild {
	var guilds []*discordgo.Guild
	for _, s := range allSessions {
		if s != nil {
			guilds = append(guilds, s.State.Guilds...)
		}
	}
	return guilds
}

// AdminCommands returns the admin slash commands
func AdminCommands() []*discordgo.ApplicationCommand {
	contactMessageMaxLength := 1000
	return []*discordgo.ApplicationCommand{
		{
			Name:        "admin",
			Description: "Bot管理者向けコマンド",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "stats",
					Description: "Bot統計情報を表示します",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name:        "backup",
					Description: "手動バックアップを作成します",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name:        "import",
					Description: "チャンネルまたはサーバー全体の履歴から過去の川柳検出／詠んだ句をDBへ取り込みます",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "channel_id",
							Description: "取り込むチャンネルのID（guild_id とどちらか一方）",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    false,
						},
						{
							Name:        "guild_id",
							Description: "サーバー一括取り込みのGuild ID（channel_id とどちらか一方）",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    false,
						},
						{
							Name:        "kind",
							Description: "取り込む種類（detection=検出川柳 / yome=詠んだ句）",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    false,
							Choices: []*discordgo.ApplicationCommandOptionChoice{
								{Name: "detection", Value: "detection"},
								{Name: "yome", Value: "yome"},
							},
						},
						{
							Name:        "overwrite",
							Description: "既存レコードがあれば上書きする（reaction / requester 等を更新）",
							Type:        discordgo.ApplicationCommandOptionBoolean,
							Required:    false,
						},
						{
							Name:        "dry_run",
							Description: "実際には保存せず件数だけ確認する",
							Type:        discordgo.ApplicationCommandOptionBoolean,
							Required:    false,
						},
						{
							Name:        "limit",
							Description: "チャンネルごとの走査メッセージ上限（デフォルト5000、最大50000）",
							Type:        discordgo.ApplicationCommandOptionInteger,
							Required:    false,
							MinValue:    floatPtr(1),
							MaxValue:    50000,
						},
					},
				},
				{
					Name:        "contact-message",
					Description: "/contactコマンドに表示する追加メッセージを管理します",
					Type:        discordgo.ApplicationCommandOptionSubCommandGroup,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "set",
							Description: "追加メッセージを設定します",
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Options: []*discordgo.ApplicationCommandOption{
								{
									Name:        "message",
									Description: "表示するメッセージ",
									Type:        discordgo.ApplicationCommandOptionString,
									Required:    true,
									MaxLength:   contactMessageMaxLength,
								},
							},
						},
						{
							Name:        "clear",
							Description: "追加メッセージを削除します",
							Type:        discordgo.ApplicationCommandOptionSubCommand,
						},
						{
							Name:        "show",
							Description: "現在の追加メッセージを表示します",
							Type:        discordgo.ApplicationCommandOptionSubCommand,
						},
					},
				},
			},
		},
	}
}

// HandleAdminCommand handles admin slash commands
func HandleAdminCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Check if user is an owner
	userID := getUserID(i)

	if !permissions.CheckOwnerPermission(userID, "admin_command") {
		respondError(s, i, "このコマンドはBot管理者のみ使用できます")
		return
	}

	metrics.RecordCommandExecuted("admin")

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		respondError(s, i, "サブコマンドを指定してください")
		return
	}

	switch options[0].Name {
	case "stats":
		handleStatsCommand(s, i)
	case "backup":
		handleBackupCommand(s, i)
	case "import":
		handleImportCommand(s, i, options[0].Options)
	case "contact-message":
		handleContactMessageCommand(s, i, options[0].Options)
	}
}

func floatPtr(v float64) *float64 { return &v }

func handleImportCommand(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	var channelID string
	var guildID string
	var dryRun bool
	var overwrite bool
	var limit int
	kind := service.ImportKindDetection

	for _, opt := range options {
		switch opt.Name {
		case "channel_id":
			channelID = strings.TrimSpace(opt.StringValue())
		case "guild_id":
			guildID = strings.TrimSpace(opt.StringValue())
		case "kind":
			kind = strings.TrimSpace(opt.StringValue())
		case "overwrite":
			overwrite = opt.BoolValue()
		case "dry_run":
			dryRun = opt.BoolValue()
		case "limit":
			limit = int(opt.IntValue())
		}
	}

	hasChannel := channelID != ""
	hasGuild := guildID != ""
	switch {
	case hasChannel && hasGuild:
		respondError(s, i, "channel_id と guild_id は同時に指定できません。どちらか一方を指定してください")
		return
	case !hasChannel && !hasGuild:
		respondError(s, i, "channel_id または guild_id のいずれかを指定してください")
		return
	case hasChannel && !isSnowflake(channelID):
		respondError(s, i, "有効な channel_id を指定してください（数字のみの Discord ID）")
		return
	case hasGuild && !isSnowflake(guildID):
		respondError(s, i, "有効な guild_id を指定してください（数字のみの Discord ID）")
		return
	}
	if kind != service.ImportKindDetection && kind != service.ImportKindYome {
		respondError(s, i, "kind は detection または yome を指定してください")
		return
	}

	conf := config.GetConf()
	sourceBotIDs := append([]string{}, conf.Import.SourceBotIDs...)
	if len(sourceBotIDs) == 0 && s.State != nil && s.State.User != nil {
		sourceBotIDs = []string{s.State.User.ID}
	}
	if len(sourceBotIDs) == 0 {
		respondError(s, i, "取り込む Bot のユーザー ID を特定できません。config の import.source_bot_ids を設定してください")
		return
	}

	// Defer: history scan can exceed the 3s interaction timeout
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		logger.Error("Failed to defer import response", "error", err)
		return
	}

	if !importMu.TryLock() {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("別の import が実行中です。完了してから再実行してください。"),
		})
		return
	}
	defer importMu.Unlock()

	opts := service.ImportOptions{
		ChannelID:    channelID,
		GuildID:      guildID,
		SourceBotIDs: sourceBotIDs,
		DryRun:       dryRun,
		Overwrite:    overwrite,
		Limit:        limit,
		Kind:         kind,
	}

	var result service.ImportResult
	var err error
	if hasGuild {
		result, err = service.ImportGuildHistory(s, opts)
	} else {
		result, err = service.ImportChannelHistory(s, opts)
	}
	if err != nil {
		logger.Error("Import failed", "error", err, "channel_id", channelID, "guild_id", guildID, "kind", kind)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("インポートに失敗しました: " + err.Error()),
		})
		return
	}

	title := "Import Complete"
	if dryRun {
		title = "Import Dry Run"
	}

	scope := fmt.Sprintf("channel_id: `%s`", channelID)
	if hasGuild {
		scope = fmt.Sprintf("guild_id: `%s`", guildID)
	}

	fields := []*discordgo.MessageEmbedField{
		{Name: "Kind", Value: kind, Inline: true},
		{Name: "Overwrite", Value: fmt.Sprintf("%v", overwrite), Inline: true},
		{Name: "Scanned", Value: fmt.Sprintf("%d", result.Scanned), Inline: true},
		{Name: "Matched", Value: fmt.Sprintf("%d", result.Matched), Inline: true},
		{Name: "Imported", Value: fmt.Sprintf("%d", result.Imported), Inline: true},
		{Name: "Updated", Value: fmt.Sprintf("%d", result.Updated), Inline: true},
		{Name: "Skipped (duplicate)", Value: fmt.Sprintf("%d", result.SkippedDuplicate), Inline: true},
		{Name: "Errors", Value: fmt.Sprintf("%d", result.Errors), Inline: true},
	}
	if hasGuild {
		fields = append(fields,
			&discordgo.MessageEmbedField{Name: "Channels OK", Value: fmt.Sprintf("%d", result.ChannelsOK), Inline: true},
			&discordgo.MessageEmbedField{Name: "Channels Failed", Value: fmt.Sprintf("%d", result.ChannelsFailed), Inline: true},
		)
	}
	if kind == service.ImportKindDetection {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Skipped (no parent)", Value: fmt.Sprintf("%d", result.SkippedNoParent), Inline: true,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: fmt.Sprintf("%s\nsource_bot_ids: `%s`", scope, strings.Join(sourceBotIDs, "`, `")),
		Color:       0x5865F2,
		Timestamp:   time.Now().Format(time.RFC3339),
		Fields:      fields,
	}

	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

func isSnowflake(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) >= 17 && len(s) <= 20
}

func handleStatsCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	dbStats := db.GetStats()
	conf := config.GetConf()

	uptime := time.Since(startTime).Round(time.Second)

	embed := &discordgo.MessageEmbed{
		Title:     "Bot Statistics",
		Color:     0x00ff00,
		Timestamp: time.Now().Format(time.RFC3339),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Uptime",
				Value:  uptime.String(),
				Inline: true,
			},
			{
				Name:   "Connected Guilds",
				Value:  fmt.Sprintf("%d", len(allGuilds())),
				Inline: true,
			},
			{
				Name:   "Database Driver",
				Value:  conf.Database.Driver,
				Inline: true,
			},
			{
				Name:   "Total Senryus",
				Value:  fmt.Sprintf("%d", dbStats.SenryuCount),
				Inline: true,
			},
			{
				Name:   "Muted Channels",
				Value:  fmt.Sprintf("%d", dbStats.MutedChannelCount),
				Inline: true,
			},
			{
				Name:   "Database Connected",
				Value:  fmt.Sprintf("%v", dbStats.IsConnected),
				Inline: true,
			},
		},
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

func handleBackupCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	conf := config.GetConf()

	if conf.Database.Driver != "sqlite3" {
		respondError(s, i, "バックアップはSQLiteのみ対応しています")
		return
	}

	if backupManager == nil {
		respondError(s, i, "バックアップマネージャーが初期化されていません")
		return
	}

	// Defer response for long-running operation
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	if err := backupManager.CreateBackup(); err != nil {
		logger.Error("Manual backup failed", "error", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("バックアップの作成に失敗しました: " + err.Error()),
		})
		return
	}

	// Get backup list
	backups, err := backupManager.ListBackups()
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("バックアップは作成されましたが、一覧の取得に失敗しました"),
		})
		return
	}

	description := "最新のバックアップ:\n"
	for idx, b := range backups {
		if idx >= 5 {
			break
		}
		description += fmt.Sprintf("- `%s` (%s)\n", b.Name, b.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Backup Created",
		Description: description,
		Color:       0x00ff00,
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

func handleContactMessageCommand(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		respondError(s, i, "サブコマンドを指定してください")
		return
	}

	switch options[0].Name {
	case "set":
		handleContactMessageSet(s, i, options[0].Options)
	case "clear":
		handleContactMessageClear(s, i)
	case "show":
		handleContactMessageShow(s, i)
	}
}

func handleContactMessageSet(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		respondError(s, i, "メッセージを指定してください")
		return
	}

	message := options[0].StringValue()
	if err := service.SetContactAdditionalMessage(message); err != nil {
		logger.Error("Failed to set contact additional message", "error", err)
		respondError(s, i, "追加メッセージの設定に失敗しました")
		return
	}

	respondEphemeral(s, i, "追加メッセージを設定しました ✅")
}

func handleContactMessageClear(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if err := service.ClearContactAdditionalMessage(); err != nil {
		logger.Error("Failed to clear contact additional message", "error", err)
		respondError(s, i, "追加メッセージの削除に失敗しました")
		return
	}

	respondEphemeral(s, i, "追加メッセージを削除しました ✅")
}

func handleContactMessageShow(s *discordgo.Session, i *discordgo.InteractionCreate) {
	message, err := service.GetContactAdditionalMessage()
	if err != nil {
		logger.Error("Failed to get contact additional message", "error", err)
		respondError(s, i, "追加メッセージの取得に失敗しました")
		return
	}

	if message == "" {
		respondEphemeral(s, i, "追加メッセージは設定されていません")
		return
	}

	embed := &discordgo.MessageEmbed{
		Title:       "現在の追加メッセージ",
		Description: message,
		Color:       0x5865F2,
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

func getUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func isServerAdmin(i *discordgo.InteractionCreate) bool {
	if i.Member == nil {
		return false
	}
	return i.Member.Permissions&discordgo.PermissionAdministrator != 0
}

func canManageChannel(i *discordgo.InteractionCreate) bool {
	if i.Member == nil {
		return false
	}
	return i.Member.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageChannels) != 0
}

func respondError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func strPtr(s string) *string {
	return &s
}
