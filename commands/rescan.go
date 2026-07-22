package commands

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/detect"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/pkg/permissions"
	"github.com/u16-io/FindSenryu4Discord/service"
)

const (
	rescanEmbedColor   = 0x5865F2
	rescanDebugMaxRunes = 900
)

// RescanCommand returns the /rescan slash command definition.
func RescanCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "rescan",
		Description: "指定メッセージを川柳検出パイプラインで再判定します",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "message_id",
				Description: "再判定するメッセージのID",
				Required:    true,
			},
		},
	}
}

// HandleRescanCommand handles /rescan.
func HandleRescanCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("rescan")

	if i.GuildID == "" {
		respondError(s, i, "このコマンドはサーバー内でのみ使用できます")
		return
	}

	var messageID string
	for _, opt := range i.ApplicationCommandData().Options {
		if opt.Name == "message_id" {
			messageID = strings.TrimSpace(opt.StringValue())
		}
	}
	if !isSnowflake(messageID) {
		respondError(s, i, "有効な message_id を指定してください（数字のみの Discord ID）")
		return
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		logger.Error("Failed to defer rescan response", "error", err)
		return
	}

	embed := buildRescanEmbed(s, i, messageID)
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	}); err != nil {
		logger.Error("Failed to edit rescan response", "error", err)
	}
}

func buildRescanEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, messageID string) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title:     "再スキャン結果",
		Color:     rescanEmbedColor,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	msg, err := s.ChannelMessage(i.ChannelID, messageID)
	if err != nil {
		embed.Description = fmt.Sprintf("メッセージの取得に失敗しました。\nmessage_id: `%s`\nerror: %v", messageID, err)
		return embed
	}

	authorLabel := formatAuthorPlain(msg.Author)
	msgLink := fmt.Sprintf("https://discord.com/channels/%s/%s/%s", i.GuildID, i.ChannelID, messageID)

	var b strings.Builder
	fmt.Fprintf(&b, "対象: [メッセージ](%s)\n", msgLink)
	fmt.Fprintf(&b, "投稿者: %s\n", authorLabel)

	content, spoiler := detect.PrepareContent(msg.Content)
	findResult := detect.FindHaikuWithDebug(content, true)
	valid := detect.FilterValidMatches(content, findResult.Matches)
	doubleShots := detect.FindDoubleShots(content)

	verdict, saved := rescanApply(s, i, msg, content, spoiler, valid, doubleShots)
	fmt.Fprintf(&b, "判定: %s\n", verdict)
	if saved > 0 {
		fmt.Fprintf(&b, "DB保存: %d件\n", saved)
	}
	embed.Description = b.String()

	debugLog := strings.TrimSpace(findResult.DebugLog)
	if debugLog == "" {
		debugLog = "(デバッグ出力なし)"
	}
	embed.Fields = []*discordgo.MessageEmbedField{
		{
			Name:  "haiku.Find 詳細",
			Value: "```\n" + truncateRunes(debugLog, rescanDebugMaxRunes) + "\n```",
		},
	}
	return embed
}

func rescanApply(s *discordgo.Session, i *discordgo.InteractionCreate, msg *discordgo.Message, content string, spoiler bool, matches []string, doubleShots []detect.DoubleShot) (verdict string, saved int) {
	if msg.Author == nil {
		return "スキップ（投稿者不明）", 0
	}
	if msg.Author.Bot {
		return "スキップ（Botのメッセージ）", 0
	}

	ch, err := s.State.Channel(i.ChannelID)
	if err != nil {
		ch, err = s.Channel(i.ChannelID)
		if err != nil {
			return fmt.Sprintf("スキップ（チャンネル取得失敗: %v）", err), 0
		}
	}

	switch ch.Type {
	case discordgo.ChannelTypeDM, discordgo.ChannelTypeGroupDM:
		return "スキップ（DM）", 0
	}

	if !service.IsChannelTypeEnabled(i.GuildID, ch.Type) {
		return "スキップ（このチャンネルタイプでは検出無効）", 0
	}
	if i.GuildID == permissions.GetAdminGuildID() {
		return "スキップ（管理ギルド）", 0
	}
	if service.IsMute(i.ChannelID) || (ch.ParentID != "" && service.IsMute(ch.ParentID)) {
		return "スキップ（ミュート中）", 0
	}
	if service.IsDetectionOptedOut(i.GuildID, msg.Author.ID) {
		return "スキップ（検出オプトアウト）", 0
	}
	if detect.ContainsDiscordTokens(msg.Content) {
		return "スキップ（メンション/URL等を含む）", 0
	}
	if !detect.IsJapaneseRich(content) {
		return "スキップ（日本語比率不足）", 0
	}
	if len(matches) == 0 && len(doubleShots) == 0 {
		return "検出なし", 0
	}

	var lines []string
	covered := make(map[string]bool)
	for _, ds := range doubleShots {
		lines = append(lines, detect.FormatDetectionReply(ds.DisplayBody(), false, true))
		for _, match := range []string{ds.First, ds.Second} {
			covered[match] = true
			parts := strings.Split(match, " ")
			if len(parts) != 3 || service.IsExcludedSenryu(match) {
				continue
			}
			sp := spoiler
			if _, err := service.CreateSenryu(model.Senryu{
				ServerID:  i.GuildID,
				AuthorID:  msg.Author.ID,
				Kamigo:    parts[0],
				Nakasichi: parts[1],
				Simogo:    parts[2],
				Spoiler:   &sp,
			}); err != nil {
				logger.Error("Rescan: failed to create senryu", "error", err, "match", match)
				continue
			}
			saved++
		}
	}
	for _, match := range matches {
		if covered[match] {
			continue
		}
		parts := strings.Split(match, " ")
		if len(parts) != 3 {
			continue
		}
		lines = append(lines, "「"+match+"」")
		if service.IsExcludedSenryu(match) {
			continue
		}
		sp := spoiler
		if _, err := service.CreateSenryu(model.Senryu{
			ServerID:  i.GuildID,
			AuthorID:  msg.Author.ID,
			Kamigo:    parts[0],
			Nakasichi: parts[1],
			Simogo:    parts[2],
			Spoiler:   &sp,
		}); err != nil {
			logger.Error("Rescan: failed to create senryu", "error", err, "match", match)
			continue
		}
		saved++
	}
	return "検出あり\n" + strings.Join(lines, "\n"), saved
}

func formatAuthorPlain(u *discordgo.User) string {
	if u == nil {
		return "(unknown)"
	}
	name := u.Username
	if u.GlobalName != "" {
		name = u.GlobalName
	}
	return fmt.Sprintf("%s（%s）", name, u.ID)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
