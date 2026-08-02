package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/commands"
	"github.com/u16-io/FindSenryu4Discord/config"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/adminnotify"
	"github.com/u16-io/FindSenryu4Discord/pkg/backup"
	"github.com/u16-io/FindSenryu4Discord/pkg/crypto"
	"github.com/u16-io/FindSenryu4Discord/pkg/detect"
	"github.com/u16-io/FindSenryu4Discord/pkg/health"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/pkg/permissions"
	"github.com/u16-io/FindSenryu4Discord/pkg/yomesched"
	"github.com/u16-io/FindSenryu4Discord/service"

	"github.com/0x307e/go-haiku"
	"github.com/bwmarrin/discordgo"
	"github.com/ikawaha/kagome-dict/uni"
)

var (
	startTime       time.Time
	adminNotifier   *adminnotify.Manager
	yomeScheduler   *yomesched.Manager
	botReady        atomic.Bool
	guildCacheTimer atomic.Pointer[time.Timer]
	allSessions     []*discordgo.Session
	expectedShards  atomic.Int32
	connectedShards atomic.Int32

	// adminPermission is used for DefaultMemberPermissions on admin-only commands.
	adminPermission int64 = discordgo.PermissionAdministrator

	// manageChannelPermission is used for DefaultMemberPermissions on channel management commands.
	manageChannelPermission int64 = discordgo.PermissionManageChannels

	userCommands = []*discordgo.ApplicationCommand{
		{
			Name:                     "mute",
			Description:              "このチャンネルでの川柳検出をミュートします",
			DefaultMemberPermissions: &manageChannelPermission,
		},
		{
			Name:                     "unmute",
			Description:              "このチャンネルでの川柳検出のミュートを解除します",
			DefaultMemberPermissions: &manageChannelPermission,
		},
		{
			Name:        "rank",
			Description: "ギルド内で詠んだ回数が多い人のランキングを表示します",
		},
		{
			Name:        "delete",
			Description: "川柳を削除します",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "select",
					Description: "川柳を選んで1件ずつ削除します",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionUser,
							Name:        "user",
							Description: "削除対象のユーザー",
							Required:    true,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "bulk",
					Description: "ユーザーや期間を指定して一括削除します",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionUser,
							Name:        "user",
							Description: "削除対象のユーザー（省略時は期間内の全員・管理者のみ）",
							Required:    false,
						},
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "from",
							Description: "開始日（YYYY-MM-DD、この日を含む）",
							Required:    false,
						},
						{
							Type:        discordgo.ApplicationCommandOptionString,
							Name:        "to",
							Description: "終了日（YYYY-MM-DD、この日を含む）",
							Required:    false,
						},
					},
				},
			},
		},
		{
			Name:                     "channel",
			Description:              "チャンネルタイプ別の川柳検出設定を変更します",
			DefaultMemberPermissions: &adminPermission,
		},
		{
			Name:        "doctor",
			Description: "このチャンネルでBotが正常に動作するか診断します",
		},
		{
			Name:        "detect",
			Description: "自分の川柳検出のオン/オフを切り替えます",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "on",
					Description: "川柳検出を有効にします",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "off",
					Description: "川柳検出を無効にします",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "status",
					Description: "現在の川柳検出設定を表示します",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "ban",
					Description: "指定ユーザーの川柳検出を無効化します（管理者専用）",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionUser,
							Name:        "user",
							Description: "対象ユーザー",
							Required:    true,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "unban",
					Description: "指定ユーザーの川柳検出無効化を解除します（管理者専用）",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type:        discordgo.ApplicationCommandOptionUser,
							Name:        "user",
							Description: "対象ユーザー",
							Required:    true,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "list",
					Description: "川柳検出無効化ユーザー一覧を表示します（管理者専用）",
				},
			},
		},
	}

	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"mute":    commands.HandleMuteCommand,
		"unmute":  commands.HandleUnmuteCommand,
		"rank":    handleRankCommand,
		"channel": commands.HandleChannelCommand,
		"delete":  commands.HandleDeleteCommand,
		"doctor":  commands.HandleDoctorCommand,
		"detect":  commands.HandleDetectCommand,
		"rescan":  commands.HandleRescanCommand,
		"admin":   commands.HandleAdminCommand,
		"contact": commands.HandleContactCommand,
	}
)

func main() {
	startTime = time.Now()

	// Initialize haiku dictionary
	haiku.UseDict(uni.Dict())

	// Load configuration
	conf, err := config.Load("config.toml")
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger.Init(logger.Config{
		Level:  conf.Log.Level,
		Format: conf.Log.Format,
	})

	logger.Info("Starting FindSenryu4Discord",
		"log_level", conf.Log.Level,
		"db_driver", conf.Database.Driver,
	)

	// Initialize encryption
	if err := crypto.Init(conf.Encryption.Key); err != nil {
		logger.Error("Failed to initialize encryption", "error", err)
		os.Exit(1)
	}
	conf.Encryption.Key = "" // zero out key from config struct
	if crypto.IsEnabled() {
		logger.Info("Senryu encryption enabled")
	}

	// Initialize database
	if err := db.Init(); err != nil {
		logger.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}

	// Remove test senryu rows that must not remain in the database
	if n, err := service.DeleteSenryusMatching("テストです", "この川柳に", "反応は"); err != nil {
		logger.Error("Failed to clean up excluded test senryus", "error", err)
	} else if n > 0 {
		logger.Info("Cleaned up excluded test senryus", "count", n)
	}

	// Start health check server
	healthServer, err := health.StartServer()
	if err != nil {
		logger.Error("Failed to start health server", "error", err)
	}

	// Initialize backup manager
	var backupManager *backup.Manager
	if conf.Database.Driver == "sqlite3" && conf.Backup.Enabled {
		backupManager = backup.NewManager(conf.Backup, conf.Database.Path)
		backupManager.Start()
		commands.SetBackupManager(backupManager)
	}
	commands.SetStartTime(startTime)

	// Get recommended shard count from Discord
	tmpSession, err := discordgo.New("Bot " + conf.Discord.Token)
	if err != nil {
		logger.Error("Failed to create Discord session", "error", err)
		os.Exit(1)
	}
	gatewayBot, err := tmpSession.GatewayBot()
	if err != nil {
		logger.Error("Failed to get gateway bot info", "error", err)
		os.Exit(1)
	}
	shardCount := gatewayBot.Shards
	if shardCount < 1 {
		shardCount = 1
	}
	logger.Info("Discord gateway info", "recommended_shards", gatewayBot.Shards, "using_shards", shardCount)

	// Gateway Intents
	intents := discordgo.IntentGuilds |
		discordgo.IntentGuildMessages |
		discordgo.IntentGuildMessageReactions |
		discordgo.IntentMessageContent

	// Create and open sessions for each shard
	expectedShards.Store(int32(shardCount))
	allSessions = make([]*discordgo.Session, shardCount)
	for i := 0; i < shardCount; i++ {
		s, err := discordgo.New("Bot " + conf.Discord.Token)
		if err != nil {
			logger.Error("Failed to create Discord session", "error", err, "shard", i)
			os.Exit(1)
		}
		s.ShardID = i
		s.ShardCount = shardCount
		s.Identify.Intents = intents

		s.AddHandler(messageCreate)
		s.AddHandler(messageReactionAdd)
		s.AddHandler(interactionCreate)
		s.AddHandler(guildCreate)
		s.AddHandler(guildDelete)
		s.AddHandler(onConnect)

		if err := s.Open(); err != nil {
			logger.Error("Failed to open Discord connection", "error", err, "shard", i)
			os.Exit(1)
		}
		logger.Info("Shard connected", "shard_id", i, "shard_count", shardCount)
		allSessions[i] = s

		// Rate limit: wait between shard connections (Discord recommends ~5s)
		if i < shardCount-1 {
			time.Sleep(5 * time.Second)
		}
	}

	// Share all sessions with commands package for cross-shard guild counting
	commands.SetAllSessions(allSessions)

	// Use shard 0 as the primary session for command registration
	dg := allSessions[0]
	adminGuildID := permissions.GetAdminGuildID()
	registerSlashCommands(dg, conf.Admin.ContactChannelID != "", adminGuildID)

	// Clear stale guild-scoped copies of user commands (registered historically for
	// instant availability). Those stack with global commands and appear as duplicates.
	go clearStaleGuildUserCommands(dg, adminGuildID)

	// Update game status
	dg.UpdateGameStatus(1, conf.Discord.Playing)

	// Update database stats in metrics
	dbStats := db.GetStats()
	metrics.SetDatabaseStats(dbStats.SenryuCount, dbStats.MutedChannelCount, dbStats.OptOutCount)

	// Initialize admin notification manager
	if conf.Admin.LogChannelID != "" || conf.Admin.ReportChannelID != "" {
		adminNotifier = adminnotify.NewManager(dg, conf.Admin.LogChannelID, conf.Admin.ReportChannelID)
		adminNotifier.SetAllSessions(allSessions)
		adminNotifier.Start()
		adminNotifier.NotifyStarted()
	}

	// Start scheduled yome poller (restores pending jobs from DB on tick)
	yomeScheduler = yomesched.NewManager(dg, sendRandomSenryu)
	yomeScheduler.Start()

	botReady.Store(true)

	// Mark as ready
	if healthServer != nil {
		healthServer.SetReady(true)
	}

	logger.Info("Bot is now running. Press CTRL-C to exit.")

	// Wait for termination signal
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Graceful shutdown
	logger.Info("Shutting down...")

	// Mark as not ready
	if healthServer != nil {
		healthServer.SetReady(false)
	}

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop admin notification manager
	if adminNotifier != nil {
		adminNotifier.NotifyStopping()
		adminNotifier.Stop(ctx)
	}

	// Stop scheduled yome manager
	if yomeScheduler != nil {
		yomeScheduler.Stop(ctx)
	}

	// Stop backup manager
	if backupManager != nil {
		backupManager.Stop(ctx)
	}

	// Stop health server
	if healthServer != nil {
		if err := healthServer.Stop(ctx); err != nil {
			logger.Error("Failed to stop health server", "error", err)
		}
	}

	// Slash commands are intentionally NOT removed on shutdown.
	// ApplicationCommandBulkOverwrite (called on startup) keeps the desired set
	// without the up-to-1-hour global propagation delay of delete-and-recreate.

	// Close all Discord shard connections
	for _, s := range allSessions {
		if err := s.Close(); err != nil {
			logger.Error("Failed to close Discord connection", "error", err, "shard", s.ShardID)
		}
	}

	// Close database
	if err := db.Close(); err != nil {
		logger.Error("Failed to close database", "error", err)
	}

	logger.Info("Shutdown complete")
}

func onConnect(s *discordgo.Session, _ *discordgo.Connect) {
	n := connectedShards.Add(1)
	logger.Info("Gateway connected, caching guilds...", "shard", s.ShardID, "connected_shards", n, "expected_shards", expectedShards.Load())
	botReady.Store(false)
	// Reset debounce timer on new shard connection to prevent premature ready
	if t := guildCacheTimer.Load(); t != nil {
		t.Stop()
	}
}

func countAllGuilds() int {
	total := 0
	for _, s := range allSessions {
		if s != nil {
			total += len(s.State.Guilds)
		}
	}
	return total
}

func guildCreate(s *discordgo.Session, g *discordgo.GuildCreate) {
	metrics.SetConnectedGuilds(countAllGuilds())
	if !botReady.Load() {
		logger.Debug("Guild cache", "name", g.Name, "id", g.ID)
		// Register existing guilds so reconnect doesn't trigger welcome messages
		commands.MarkGuildWelcomeSent(g.ID)
		// Debounce: reset timer on each GUILD_CREATE during cache burst.
		// When no more events arrive within 5s, mark as ready.
		if t := guildCacheTimer.Load(); t != nil {
			t.Stop()
		}
		t := time.AfterFunc(5*time.Second, func() {
			if connectedShards.Load() < expectedShards.Load() {
				// Not all shards connected yet; wait for remaining shards
				logger.Info("Guild cache paused, waiting for remaining shards",
					"guilds", countAllGuilds(),
					"connected_shards", connectedShards.Load(),
					"expected_shards", expectedShards.Load(),
				)
				return
			}
			total := countAllGuilds()
			logger.Info("Guild cache complete, bot is ready", "guilds", total, "shards", expectedShards.Load())
			metrics.SetConnectedGuilds(total)
			botReady.Store(true)
		})
		guildCacheTimer.Store(t)
		return
	}
	logger.Info("Joined guild", "name", g.Name, "id", g.ID)
	if adminNotifier != nil {
		adminNotifier.NotifyGuildJoin(g.Guild)
	}
	go commands.SendWelcomeMessage(s, g)
	// Ensure this guild has no leftover guild-scoped user commands (duplicates of global).
	go syncGuildSlashCommands(s, g.ID, permissions.GetAdminGuildID())
}

// buildUserCommands returns the global user slash command set for this process.
func buildUserCommands(includeContact bool) []*discordgo.ApplicationCommand {
	cmds := make([]*discordgo.ApplicationCommand, len(userCommands), len(userCommands)+2)
	copy(cmds, userCommands)
	if includeContact {
		cmds = append(cmds, &discordgo.ApplicationCommand{
			Name:                     "contact",
			Description:              "Bot管理者にお問い合わせを送信します",
			DefaultMemberPermissions: &adminPermission,
		})
	}
	cmds = append(cmds, commands.RescanCommand())
	return cmds
}

// registerSlashCommands overwrites global user commands and admin-guild commands.
func registerSlashCommands(s *discordgo.Session, includeContact bool, adminGuildID string) {
	appID := s.State.User.ID
	userCmds := buildUserCommands(includeContact)

	logger.Info("Registering user slash commands (global overwrite)...", "count", len(userCmds))
	if _, err := s.ApplicationCommandBulkOverwrite(appID, "", userCmds); err != nil {
		logger.Error("Failed to register global commands", "error", err)
	} else {
		for _, cmd := range userCmds {
			logger.Info("Registered command", "command", cmd.Name, "scope", "global")
		}
	}

	if adminGuildID == "" {
		return
	}
	adminCmds := commands.AdminCommands()
	logger.Info("Registering admin slash commands (guild overwrite)...", "guild_id", adminGuildID, "count", len(adminCmds))
	if _, err := s.ApplicationCommandBulkOverwrite(appID, adminGuildID, adminCmds); err != nil {
		logger.Error("Failed to register admin commands", "guild_id", adminGuildID, "error", err)
	} else {
		for _, cmd := range adminCmds {
			logger.Info("Registered admin command", "command", cmd.Name, "guild_id", adminGuildID)
		}
	}
}

// clearStaleGuildUserCommands waits for guild cache, then removes guild-scoped
// user commands that would duplicate the global set.
func clearStaleGuildUserCommands(s *discordgo.Session, adminGuildID string) {
	for connectedShards.Load() < expectedShards.Load() {
		time.Sleep(time.Second)
	}
	// Match guildCreate debounce (~5s) so State.Guilds is populated.
	time.Sleep(6 * time.Second)

	seen := make(map[string]bool)
	var guildIDs []string
	for _, sess := range allSessions {
		if sess == nil {
			continue
		}
		for _, g := range sess.State.Guilds {
			if g == nil || g.ID == "" || seen[g.ID] {
				continue
			}
			seen[g.ID] = true
			guildIDs = append(guildIDs, g.ID)
		}
	}

	logger.Info("Clearing stale guild-scoped user commands...", "guilds", len(guildIDs))
	for _, guildID := range guildIDs {
		syncGuildSlashCommands(s, guildID, adminGuildID)
		time.Sleep(300 * time.Millisecond)
	}
	logger.Info("Stale guild command cleanup finished", "guilds", len(guildIDs))
}

// syncGuildSlashCommands sets guild commands to admin-only on the admin guild,
// or clears all guild commands elsewhere (user commands stay global-only).
func syncGuildSlashCommands(s *discordgo.Session, guildID, adminGuildID string) {
	if s == nil || guildID == "" {
		return
	}
	appID := s.State.User.ID
	cmds := []*discordgo.ApplicationCommand{}
	if adminGuildID != "" && guildID == adminGuildID {
		cmds = commands.AdminCommands()
	}
	if _, err := s.ApplicationCommandBulkOverwrite(appID, guildID, cmds); err != nil {
		logger.Error("Failed to sync guild slash commands", "guild_id", guildID, "error", err)
		return
	}
	logger.Info("Synced guild slash commands", "guild_id", guildID, "count", len(cmds))
}

func guildDelete(s *discordgo.Session, g *discordgo.GuildDelete) {
	logger.Info("Left guild", "id", g.ID)
	metrics.SetConnectedGuilds(countAllGuilds())

	// Clear welcome-sent flag so re-invitation triggers a new welcome message
	commands.ClearGuildWelcomeSent(g.ID)

	// Clean up guild data
	senryuCount, err := service.DeleteSenryuByServer(g.ID)
	if err != nil {
		logger.Error("Failed to clean up guild data", "error", err, "guild_id", g.ID, "type", "senryus")
	}
	optOutCount, err := service.DeleteOptOutByServer(g.ID)
	if err != nil {
		logger.Error("Failed to clean up guild data", "error", err, "guild_id", g.ID, "type", "opt-outs")
	}
	channelConfigCount, err := service.DeleteChannelConfigByGuild(g.ID)
	if err != nil {
		logger.Error("Failed to clean up guild data", "error", err, "guild_id", g.ID, "type", "channel-config")
	}

	logger.Info("Guild data cleaned up",
		"guild_id", g.ID,
		"senryus", senryuCount,
		"opt_outs", optOutCount,
		"channel_configs", channelConfigCount,
	)

	if botReady.Load() && adminNotifier != nil {
		adminNotifier.NotifyGuildLeave(g, senryuCount, optOutCount)
	}
}

func interactionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	case discordgo.InteractionMessageComponent:
		handleComponentInteraction(s, i)
	case discordgo.InteractionModalSubmit:
		handleModalSubmitInteraction(s, i)
	}
}

func handleModalSubmitInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.ModalSubmitData().CustomID
	switch {
	case customID == commands.ContactModalCustomID:
		commands.HandleContactModalSubmit(s, i)
	case strings.HasPrefix(customID, commands.ReplyModalPrefix):
		commands.HandleContactReplyModalSubmit(s, i)
	}
}

func handleComponentInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID

	switch {
	case customID == commands.DeleteSelectCustomID:
		commands.HandleDeleteSelectMenu(s, i)
	case strings.HasPrefix(customID, commands.DeleteConfirmPrefix):
		commands.HandleDeleteConfirm(s, i)
	case customID == commands.DeleteCancelCustomID:
		commands.HandleDeleteCancel(s, i)
	case strings.HasPrefix(customID, commands.DeletePagePrefix):
		commands.HandleDeletePage(s, i)
	case strings.HasPrefix(customID, commands.DeleteBulkConfirmPrefix):
		commands.HandleDeleteBulkConfirm(s, i)
	case customID == commands.DeleteBulkCancelCustomID:
		commands.HandleDeleteBulkCancel(s, i)
	case customID == commands.ContactCategoryCustomID:
		commands.HandleContactCategorySelect(s, i)
	case strings.HasPrefix(customID, commands.ContactReplyPrefix):
		commands.HandleContactReplyButton(s, i)
	case strings.HasPrefix(customID, commands.ChannelTogglePrefix):
		commands.HandleChannelToggle(s, i)
	case strings.HasPrefix(customID, commands.RescanSavePrefix):
		commands.HandleRescanSave(s, i)
	}
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}

	metrics.RecordMessageProcessed()

	ch, err := s.State.Channel(m.ChannelID)
	if err != nil {
		ch, err = s.Channel(m.ChannelID)
		if err != nil {
			logger.Warn("Failed to get channel", "error", err, "channel_id", m.ChannelID)
			metrics.RecordError("discord_api")
			return
		}
	}

	// DM channels are not supported
	switch ch.Type {
	case discordgo.ChannelTypeDM, discordgo.ChannelTypeGroupDM:
		s.ChannelMessageSend(m.ChannelID, "個チャはダメです")
		return
	}

	// Check if this channel type is enabled for the guild
	if !service.IsChannelTypeEnabled(m.GuildID, ch.Type) {
		logger.Debug("Skip detection: channel type disabled",
			"guild_id", m.GuildID, "channel_id", m.ChannelID, "channel_type", ch.Type)
		return
	}

	// Skip senryu features in admin guild
	if m.GuildID == permissions.GetAdminGuildID() {
		logger.Debug("Skip detection: admin guild", "guild_id", m.GuildID)
		return
	}

	if handleYomeYomuna(m, s) {
		return
	}

	if service.IsMute(m.ChannelID) || isParentChannelMuted(ch) {
		logger.Debug("Skip detection: muted",
			"guild_id", m.GuildID, "channel_id", m.ChannelID)
		return
	}
	if m.Author.ID == s.State.User.ID {
		return
	}
	if service.IsDetectionOptedOut(m.GuildID, m.Author.ID) {
		logger.Debug("Skip detection: user opted out",
			"guild_id", m.GuildID, "user_id", m.Author.ID)
		return
	}
	if detect.ContainsDiscordTokens(m.Content) {
		logger.Debug("Skip detection: discord tokens",
			"guild_id", m.GuildID, "channel_id", m.ChannelID)
		return
	}
	content, spoiler := detect.PrepareContent(m.Content)
	if !detect.IsJapaneseRich(content) {
		logger.Debug("Skip detection: not japanese-rich",
			"guild_id", m.GuildID, "channel_id", m.ChannelID, "content_len", len(content))
		return
	}
	h := detect.FindHaiku(content)
	doubleShots := detect.FindDoubleShots(content)
	if len(h) == 0 && len(doubleShots) == 0 {
		logger.Debug("Skip detection: haiku.Find returned no matches",
			"guild_id", m.GuildID, "channel_id", m.ChannelID, "content_len", len(content))
		return
	}

	sendReply := func(replyText string, createdIDs []int) bool {
		if _, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Content:   replyText,
			Reference: m.Reference(),
			AllowedMentions: &discordgo.MessageAllowedMentions{
				Parse: []discordgo.AllowedMentionType{},
			},
			Flags: discordgo.MessageFlagsSuppressEmbeds,
		}); err != nil {
			logger.Warn("Failed to send senryu reply", "error", err, "channel_id", m.ChannelID)
			for _, createdID := range createdIDs {
				if delErr := service.DeleteSenryu(createdID, m.GuildID); delErr != nil {
					logger.Error("Failed to rollback senryu after reply failure", "error", delErr, "senryu_id", createdID)
				} else {
					logger.Info("Rolled back senryu after reply failure", "senryu_id", createdID, "channel_id", m.ChannelID)
				}
			}
			if isBotPermissionError(err) {
				if muteErr := service.ToMute(m.ChannelID, m.GuildID); muteErr != nil {
					logger.Error("Failed to auto-mute channel after permission error", "error", muteErr, "channel_id", m.ChannelID)
				} else {
					metrics.RecordAutoMute()
					logger.Warn("Auto-muted channel due to missing Bot permissions", "channel_id", m.ChannelID, "server_id", m.GuildID)
				}
				return false
			}
		}
		return true
	}

	saveSenryu := func(match string) (id int, saved bool) {
		parts := strings.Split(match, " ")
		if len(parts) != 3 || service.IsExcludedSenryu(match) {
			return 0, false
		}
		created, err := service.CreateSenryu(model.Senryu{
			ServerID:  m.GuildID,
			AuthorID:  m.Author.ID,
			Kamigo:    parts[0],
			Nakasichi: parts[1],
			Simogo:    parts[2],
			Spoiler:   &spoiler,
		})
		if err != nil {
			logger.Error("Failed to create senryu", "error", err)
			metrics.RecordError("database")
			return 0, false
		}
		return created.ID, true
	}

	covered := make(map[string]bool)
	for _, ds := range doubleShots {
		var createdIDs []int
		if id, ok := saveSenryu(ds.First); ok {
			createdIDs = append(createdIDs, id)
		}
		if id, ok := saveSenryu(ds.Second); ok {
			createdIDs = append(createdIDs, id)
		}
		replyText := detect.FormatDetectionReply(ds.DisplayBody(), spoiler, true)
		if !sendReply(replyText, createdIDs) {
			return
		}
		covered[ds.First] = true
		covered[ds.Second] = true
	}

	for _, match := range detect.FilterValidMatches(content, h) {
		if covered[match] {
			continue
		}
		parts := strings.Split(match, " ")
		if len(parts) != 3 {
			continue
		}
		var createdIDs []int
		if id, ok := saveSenryu(match); ok {
			createdIDs = append(createdIDs, id)
		}
		replyText := detect.FormatDetectionReply(match, spoiler, false)
		if !sendReply(replyText, createdIDs) {
			return
		}
	}
}

var medals = []string{"🥇", "🥈", "🥉", "🎖️", "🎖️"}

func handleRankCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("rank")

	ranks, err := service.GetRanking(i.GuildID)
	if err != nil {
		logger.Error("Failed to get ranking", "error", err, "guild_id", i.GuildID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "ランキングの取得に失敗しました",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	stats, statsErr := service.GetServerStats(i.GuildID)
	if statsErr != nil {
		logger.Warn("Failed to get server stats", "error", statsErr, "guild_id", i.GuildID)
	}

	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		guild, err = s.Guild(i.GuildID)
		if err != nil {
			logger.Warn("Failed to get guild for rank embed", "error", err, "guild_id", i.GuildID)
		}
	}

	embed := discordgo.MessageEmbed{
		Type:      discordgo.EmbedTypeRich,
		Title:     "サーバー内ランキング",
		Timestamp: time.Now().Format(time.RFC3339),
		Fields:    []*discordgo.MessageEmbedField{},
	}
	if statsErr == nil {
		if stats.TotalSenryus == 0 {
			embed.Description = "まだ誰も詠んでいません"
		} else {
			embed.Description = fmt.Sprintf("累計 **%d** 句 / **%d** 人の詠み手", stats.TotalSenryus, stats.UniqueAuthors)
		}
	}
	if guild != nil {
		embed.Footer = &discordgo.MessageEmbedFooter{
			Text:    guild.Name,
			IconURL: guild.IconURL(""),
		}
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: guild.IconURL(""),
		}
	}

	for _, rank := range ranks {
		member, err := s.GuildMember(i.GuildID, rank.AuthorId)
		if err != nil {
			continue
		}
		displayName := resolveDisplayName(member)
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%s 第%d位: %d回", medals[rank.Rank-1], rank.Rank, rank.Count),
			Value:  displayName,
			Inline: true,
		})
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{&embed},
		},
	})
}

func handleYomeYomuna(m *discordgo.MessageCreate, s *discordgo.Session) bool {
	if m.Content == "詠むな" {
		senryu, err := service.GetLastSenryu(m.GuildID)
		if err != nil {
			if errors.Is(err, service.ErrSenryuNotFound) {
				s.ChannelMessageSendReply(m.ChannelID, "まだ誰も詠んでいません。", m.Reference())
			} else {
				logger.Error("Failed to get last senryu", "error", err)
				s.MessageReactionAdd(m.ChannelID, m.ID, "❌")
			}
		} else {
			var authorName string
			if senryu.AuthorID == m.Author.ID {
				authorName = "お前"
			} else {
				member, err := s.GuildMember(m.GuildID, senryu.AuthorID)
				if err != nil {
					authorName = "<@" + senryu.AuthorID + ">"
				} else {
					authorName = resolveDisplayName(member)
				}
			}
			var reply string
			if senryu.Spoiler != nil && *senryu.Spoiler {
				reply = authorName + "が||「" + senryu.Kamigo + " " + senryu.Nakasichi + " " + senryu.Simogo + "」||って詠んだのが最後やぞ"
			} else {
				reply = authorName + "が「" + senryu.Kamigo + " " + senryu.Nakasichi + " " + senryu.Simogo + "」って詠んだのが最後やぞ"
			}
			if _, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
				Content:   reply,
				Reference: m.Reference(),
				AllowedMentions: &discordgo.MessageAllowedMentions{
					Parse: []discordgo.AllowedMentionType{},
				},
				Flags: discordgo.MessageFlagsSuppressEmbeds,
			}); err != nil {
				logger.Warn("Failed to send reply", "error", err, "channel_id", m.ChannelID)
			}
		}
		return true
	}

	yomeMax := config.GetConf().Discord.YomeMax
	durationMaxSec := config.GetConf().Discord.YomeDurationMaxSec
	scheduleMaxSec := config.GetConf().Discord.YomeScheduleMaxSec

	if count, ok, outOfRange := parseTankaYomeCount(m.Content, yomeMax); ok {
		if outOfRange {
			msg := fmt.Sprintf("詠めるのは1〜%d回までです。", yomeMax)
			if _, err := s.ChannelMessageSend(m.ChannelID, msg); err != nil {
				logger.Warn("Failed to send tanka yome limit message", "error", err, "channel_id", m.ChannelID)
			}
			return true
		}
		for i := 0; i < count; i++ {
			if err := sendRandomTanka(s, m.ChannelID, m.GuildID, m.ID); err != nil {
				return true
			}
		}
		return true
	}

	req, ok := parseYomeRequest(m.Content, time.Now(), yomeMax, durationMaxSec, scheduleMaxSec)
	if !ok {
		return false
	}

	switch req.Err {
	case yomeErrCountRange:
		msg := fmt.Sprintf("詠めるのは1〜%d回までです。", yomeMax)
		if _, err := s.ChannelMessageSend(m.ChannelID, msg); err != nil {
			logger.Warn("Failed to send yome limit message", "error", err, "channel_id", m.ChannelID)
		}
		return true
	case yomeErrDurationRange:
		msg := fmt.Sprintf("詠めるのは1〜%d秒間までです。", durationMaxSec)
		if _, err := s.ChannelMessageSend(m.ChannelID, msg); err != nil {
			logger.Warn("Failed to send yome duration limit message", "error", err, "channel_id", m.ChannelID)
		}
		return true
	case yomeErrScheduleRange:
		hours := scheduleMaxSec / 3600
		msg := fmt.Sprintf("予約できるのは1秒後から最大%d時間後までです。", hours)
		if scheduleMaxSec < 3600 {
			msg = fmt.Sprintf("予約できるのは1秒後から最大%d秒後までです。", scheduleMaxSec)
		}
		if _, err := s.ChannelMessageSend(m.ChannelID, msg); err != nil {
			logger.Warn("Failed to send yome schedule limit message", "error", err, "channel_id", m.ChannelID)
		}
		return true
	case yomeErrPastTime:
		if _, err := s.ChannelMessageSend(m.ChannelID, "その時刻はすでに過ぎています。"); err != nil {
			logger.Warn("Failed to send yome past time message", "error", err, "channel_id", m.ChannelID)
		}
		return true
	}

	switch req.Kind {
	case yomeDuration:
		if !tryLockDurationYome(m.ChannelID) {
			if _, err := s.ChannelMessageSend(m.ChannelID, "このチャンネルではすでに秒間詠みが実行中です。"); err != nil {
				logger.Warn("Failed to send duration busy message", "error", err, "channel_id", m.ChannelID)
			}
			return true
		}
		ack := fmt.Sprintf("%d秒間詠みます。", req.DurationSec)
		if _, err := s.ChannelMessageSend(m.ChannelID, ack); err != nil {
			unlockDurationYome(m.ChannelID)
			logger.Warn("Failed to send duration ack", "error", err, "channel_id", m.ChannelID)
			return true
		}
		go runDurationYome(s, m.ChannelID, m.GuildID, m.ID, req.DurationSec)
		return true

	case yomeScheduled:
		_, err := service.CreateScheduledYome(m.GuildID, m.ChannelID, m.Author.ID, req.RunAt, req.Count)
		if errors.Is(err, service.ErrScheduledYomePendingExists) {
			if _, err := s.ChannelMessageSend(m.ChannelID, "このチャンネルにはすでに予約があります。"); err != nil {
				logger.Warn("Failed to send schedule busy message", "error", err, "channel_id", m.ChannelID)
			}
			return true
		}
		if err != nil {
			logger.Error("Failed to create scheduled yome", "error", err)
			s.MessageReactionAdd(m.ChannelID, m.ID, "❌")
			return true
		}
		if _, err := s.ChannelMessageSend(m.ChannelID, formatScheduledYomeAck(req)); err != nil {
			logger.Warn("Failed to send schedule ack", "error", err, "channel_id", m.ChannelID)
		}
		return true

	default: // yomeImmediate
		for i := 0; i < req.Count; i++ {
			if err := sendRandomSenryu(s, m.ChannelID, m.GuildID, m.ID); err != nil {
				return true
			}
		}
		return true
	}
}

// sendRandomTanka composes and sends a tanka from random senryu phrases.
// reactionMessageID is used for ❌ reactions on failure (the user's command message).
func sendRandomTanka(s *discordgo.Session, channelID, guildID, reactionMessageID string) error {
	senryus, err := service.GetThreeRandomSenryus(guildID)
	if err != nil {
		logger.Error("Failed to get random senryus for tanka", "error", err)
		s.MessageReactionAdd(channelID, reactionMessageID, "❌")
		return err
	}
	if len(senryus) == 0 {
		if _, err := s.ChannelMessageSend(channelID, "まだ誰も詠んでいません。あなたが先に詠んでください。"); err != nil {
			logger.Warn("Failed to send message", "error", err, "channel_id", channelID)
		}
		return errors.New("no senryus")
	}
	extra, err := service.GetTwoRandomNakasichi(guildID)
	if err != nil {
		logger.Error("Failed to get random nakasichi for tanka", "error", err)
		s.MessageReactionAdd(channelID, reactionMessageID, "❌")
		return err
	}
	if len(extra) < 2 {
		s.MessageReactionAdd(channelID, reactionMessageID, "❌")
		return errors.New("not enough nakasichi")
	}
	phrases := []string{
		senryus[0].Kamigo,
		senryus[1].Nakasichi,
		senryus[2].Simogo,
		extra[0].Nakasichi,
		extra[1].Nakasichi,
	}
	all := append(senryus, extra...)
	content := buildTankaMessage(phrases, strings.Join(getWriters(all, guildID, s), ", "))
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: content,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
		Flags: discordgo.MessageFlagsSuppressEmbeds,
	}); err != nil {
		logger.Warn("Failed to send tanka message", "error", err, "channel_id", channelID)
		return err
	}
	if err := service.RecordYome(guildID); err != nil {
		logger.Warn("Failed to record yome", "error", err, "guild_id", guildID)
	}
	return nil
}

// parseYomeCount parses "詠め" or "n回詠め".
// ok=false means the content is not a yome command.
// outOfRange=true means it matched the pattern but n is outside [1, max].
func parseYomeCount(content string, max int) (count int, ok bool, outOfRange bool) {
	if content == "詠め" {
		return 1, true, false
	}
	if !strings.HasSuffix(content, "回詠め") {
		return 0, false, false
	}
	// "n回短歌を詠め" is handled by parseTankaYomeCount.
	if strings.Contains(content, "短歌") {
		return 0, false, false
	}
	numStr := strings.TrimSuffix(content, "回詠め")
	if numStr == "" {
		return 0, false, false
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, false, false
	}
	if n < 1 || n > max {
		return n, true, true
	}
	return n, true, false
}

// parseTankaYomeCount parses "短歌を詠め" or "n回短歌を詠め".
func parseTankaYomeCount(content string, max int) (count int, ok bool, outOfRange bool) {
	if content == "短歌を詠め" {
		return 1, true, false
	}
	const suffix = "回短歌を詠め"
	if !strings.HasSuffix(content, suffix) {
		return 0, false, false
	}
	numStr := strings.TrimSuffix(content, suffix)
	if numStr == "" {
		return 0, false, false
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, false, false
	}
	if n < 1 || n > max {
		return n, true, true
	}
	return n, true, false
}

// parseYomeMessage parses a bot 「詠め」 reply (ここで一句 with exactly 3 phrases).
func parseYomeMessage(content string) (phrases []string, writers string, ok bool) {
	const prefix = "ここで一句\n"
	if !strings.HasPrefix(content, prefix) {
		return nil, "", false
	}
	rest := strings.TrimPrefix(content, prefix)
	lines := strings.SplitN(rest, "\n", 2)
	phraseLine := lines[0]
	if !strings.HasPrefix(phraseLine, "「") || !strings.HasSuffix(phraseLine, "」") {
		return nil, "", false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(phraseLine, "「"), "」")
	phrases = strings.Split(inner, " ")
	if len(phrases) != 3 {
		return nil, "", false
	}
	for _, p := range phrases {
		if p == "" {
			return nil, "", false
		}
	}
	if len(lines) >= 2 && strings.HasPrefix(lines[1], "詠み手: ") {
		writers = strings.TrimPrefix(lines[1], "詠み手: ")
	}
	return phrases, writers, true
}

// buildTankaMessage builds the message content for a tanka (5 phrases).
func buildTankaMessage(phrases []string, writers string) string {
	return fmt.Sprintf("ここで一首\n「%s」\n詠み手: %s", strings.Join(phrases, " "), writers)
}

// stripEmojiModifiers removes VS16/VS15 and skin-tone modifiers from an emoji string.
func stripEmojiModifiers(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\uFE0F' || r == '\uFE0E':
			continue
		case r >= 0x1F3FB && r <= 0x1F3FF:
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isTankaReaction reports whether emojiName matches the configured tanka reaction.
// Unicode skin-tone / variation-selector variants are accepted; custom emoji names require an exact match.
func isTankaReaction(emojiName, configured string) bool {
	if configured == "" || emojiName == "" {
		return false
	}
	if emojiName == configured {
		return true
	}
	baseConfigured := stripEmojiModifiers(configured)
	baseEmoji := stripEmojiModifiers(emojiName)
	if baseConfigured == "" {
		return false
	}
	// Custom emoji names are alphanumeric; only exact match (already handled) applies.
	// For unicode, compare stripped bases and allow configured prefix (skin tones).
	if baseEmoji == baseConfigured {
		return true
	}
	return strings.HasPrefix(emojiName, configured) || strings.HasPrefix(emojiName, baseConfigured)
}

func messageReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if r.UserID == s.State.User.ID {
		return
	}
	if r.GuildID == "" || r.GuildID == permissions.GetAdminGuildID() {
		return
	}
	configured := config.GetConf().Discord.TankaReaction
	if !isTankaReaction(r.Emoji.Name, configured) {
		return
	}

	msg, err := s.ChannelMessage(r.ChannelID, r.MessageID)
	if err != nil {
		logger.Warn("Failed to fetch message for tanka reaction",
			"error", err, "channel_id", r.ChannelID, "message_id", r.MessageID)
		return
	}
	if msg.Author == nil || msg.Author.ID != s.State.User.ID {
		return
	}

	phrases, writers, ok := parseYomeMessage(msg.Content)
	if !ok {
		return
	}

	extra, err := service.GetTwoRandomNakasichi(r.GuildID)
	if err != nil {
		logger.Error("Failed to get random nakasichi for tanka", "error", err, "guild_id", r.GuildID)
		s.MessageReactionAdd(r.ChannelID, r.MessageID, "❌")
		return
	}
	if len(extra) < 2 {
		logger.Debug("Not enough senryus for tanka extension", "guild_id", r.GuildID, "count", len(extra))
		s.MessageReactionAdd(r.ChannelID, r.MessageID, "❌")
		return
	}

	phrases = append(phrases, extra[0].Nakasichi, extra[1].Nakasichi)
	var existing []string
	if writers != "" {
		existing = strings.Split(writers, ", ")
	}
	mergedWriters := sliceUnique(append(existing, getWriters(extra, r.GuildID, s)...))

	content := buildTankaMessage(phrases, strings.Join(mergedWriters, ", "))
	if _, err := s.ChannelMessageSendComplex(r.ChannelID, &discordgo.MessageSend{
		Content:   content,
		Reference: &discordgo.MessageReference{MessageID: r.MessageID, ChannelID: r.ChannelID, GuildID: r.GuildID},
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
		Flags: discordgo.MessageFlagsSuppressEmbeds,
	}); err != nil {
		logger.Warn("Failed to send tanka message from reaction",
			"error", err, "channel_id", r.ChannelID, "message_id", r.MessageID)
	} else if err := service.RecordYome(r.GuildID); err != nil {
		logger.Warn("Failed to record yome", "error", err, "guild_id", r.GuildID)
	}
}

// resolveDisplayName returns the best display name for a guild member,
// preferring Nick > GlobalName > Username.
func resolveDisplayName(member *discordgo.Member) string {
	if member.Nick != "" {
		return member.Nick
	}
	if member.User.GlobalName != "" {
		return member.User.GlobalName
	}
	return member.User.Username
}

// isParentChannelMuted checks if the parent channel of a thread is muted.
func isParentChannelMuted(ch *discordgo.Channel) bool {
	if ch.ParentID == "" {
		return false
	}
	return service.IsMute(ch.ParentID)
}

func sliceUnique(target []string) (unique []string) {
	m := map[string]bool{}
	for _, v := range target {
		if !m[v] {
			m[v] = true
			unique = append(unique, v)
		}
	}
	return unique
}

// isBotPermissionError returns true if the error is a Discord API error
// caused by missing Bot permissions on the channel.
func isBotPermissionError(err error) bool {
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Message != nil {
		switch restErr.Message.Code {
		case 50001, // Missing Access
			50013,  // Missing Permissions
			160002: // Cannot reply without permission to read message history
			return true
		}
	}
	return false
}

func getWriters(senryus []model.Senryu, guildID string, session *discordgo.Session) []string {
	var writers []string
	for _, senryu := range senryus {
		member, err := session.GuildMember(guildID, senryu.AuthorID)
		if err != nil {
			continue
		}
		writers = append(writers, resolveDisplayName(member))
	}
	return sliceUnique(writers)
}
