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
	rescanEmbedColor    = 0x5865F2
	rescanDebugMaxRunes = 900
	// RescanSavePrefix is the custom ID prefix for the rescan DB save button.
	RescanSavePrefix = "rescan_save:"
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

	embed, components := buildRescanResult(s, i.GuildID, i.ChannelID, messageID, getUserID(i), false, 0)
	edit := &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	}
	if components != nil {
		edit.Components = components
	}
	if _, err := s.InteractionResponseEdit(i.Interaction, edit); err != nil {
		logger.Error("Failed to edit rescan response", "error", err)
	}
}

// HandleRescanSave handles the "DBに保存" button on a /rescan result.
func HandleRescanSave(s *discordgo.Session, i *discordgo.InteractionCreate) {
	payload := strings.TrimPrefix(i.MessageComponentData().CustomID, RescanSavePrefix)
	parts := strings.Split(payload, ":")
	if len(parts) != 5 {
		respondEphemeral(s, i, "無効な操作です")
		return
	}
	guildID, channelID, messageID, invokerID, spoilerFlag := parts[0], parts[1], parts[2], parts[3], parts[4]

	if getUserID(i) != invokerID {
		respondEphemeral(s, i, "このボタンは /rescan を実行したユーザーのみ押せます")
		return
	}
	if i.GuildID != "" && i.GuildID != guildID {
		respondEphemeral(s, i, "無効な操作です")
		return
	}

	spoiler := spoilerFlag == "1"

	msg, err := s.ChannelMessage(channelID, messageID)
	if err != nil {
		respondEphemeral(s, i, fmt.Sprintf("メッセージの取得に失敗しました: %v", err))
		return
	}

	content, _ := detect.PrepareContent(msg.Content)
	findResult := detect.FindHaikuWithDebug(content, false)
	valid := detect.FilterValidMatches(content, findResult.Matches)
	doubleShots := detect.FindDoubleShots(content)

	if msg.Author == nil {
		respondEphemeral(s, i, "投稿者が不明なため保存できません")
		return
	}

	candidates := collectSavableMatches(valid, doubleShots)
	saved := 0
	for _, match := range candidates {
		parts := strings.Split(match, " ")
		if len(parts) != 3 {
			continue
		}
		sp := spoiler
		if _, err := service.CreateSenryu(model.Senryu{
			ServerID:  guildID,
			AuthorID:  msg.Author.ID,
			Kamigo:    parts[0],
			Nakasichi: parts[1],
			Simogo:    parts[2],
			Spoiler:   &sp,
		}); err != nil {
			logger.Error("Rescan save: failed to create senryu", "error", err, "match", match)
			continue
		}
		saved++
	}

	embed, _ := buildRescanResult(s, guildID, channelID, messageID, invokerID, true, saved)
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: []discordgo.MessageComponent{},
		},
	}); err != nil {
		logger.Error("Failed to update rescan after save", "error", err)
	}
}

func buildRescanResult(s *discordgo.Session, guildID, channelID, messageID, invokerID string, afterSave bool, savedCount int) (*discordgo.MessageEmbed, *[]discordgo.MessageComponent) {
	embed := &discordgo.MessageEmbed{
		Title:     "再スキャン結果",
		Color:     rescanEmbedColor,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	msg, err := s.ChannelMessage(channelID, messageID)
	if err != nil {
		embed.Description = fmt.Sprintf("メッセージの取得に失敗しました。\nmessage_id: `%s`\nerror: %v", messageID, err)
		return embed, nil
	}

	authorLabel := formatAuthorPlain(msg.Author)
	msgLink := fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)

	var b strings.Builder
	fmt.Fprintf(&b, "対象: [メッセージ](%s)\n", msgLink)
	fmt.Fprintf(&b, "投稿者: %s\n", authorLabel)

	content, spoiler := detect.PrepareContent(msg.Content)
	findResult := detect.FindHaikuWithDebug(content, true)
	valid := detect.FilterValidMatches(content, findResult.Matches)
	doubleShots := detect.FindDoubleShots(content)

	verdict, saveCandidates := rescanApply(s, guildID, channelID, msg, content, valid, doubleShots)
	fmt.Fprintf(&b, "判定: %s\n", verdict)
	if afterSave {
		fmt.Fprintf(&b, "DB保存: %d件\n", savedCount)
	} else if saveCandidates > 0 {
		fmt.Fprintf(&b, "保存候補: %d件\n", saveCandidates)
	}
	embed.Description = b.String()

	debugLog := strings.TrimSpace(findResult.DebugLog)
	if debugLog == "" {
		debugLog = "(デバッグ出力なし)"
	} else {
		formatted := detect.FormatHaikuDebugLog(debugLog)
		if formatted != "" {
			debugLog = formatted
		}
	}
	embed.Fields = []*discordgo.MessageEmbedField{
		{
			Name:  "haiku.Find 詳細",
			Value: "```\n" + truncateRunes(debugLog, rescanDebugMaxRunes) + "\n```",
		},
	}

	var components *[]discordgo.MessageComponent
	if !afterSave && saveCandidates > 0 {
		spoilerFlag := "0"
		if spoiler {
			spoilerFlag = "1"
		}
		customID := fmt.Sprintf("%s%s:%s:%s:%s:%s", RescanSavePrefix, guildID, channelID, messageID, invokerID, spoilerFlag)
		components = &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "DBに保存",
						Style:    discordgo.PrimaryButton,
						CustomID: customID,
					},
				},
			},
		}
	}
	return embed, components
}

// rescanApply returns the verdict text and the number of savable matches (not yet saved).
func rescanApply(s *discordgo.Session, guildID, channelID string, msg *discordgo.Message, content string, matches []string, doubleShots []detect.DoubleShot) (verdict string, saveCandidates int) {
	if msg.Author == nil {
		return "スキップ（投稿者不明）", 0
	}
	if msg.Author.Bot {
		return "スキップ（Botのメッセージ）", 0
	}

	ch, err := s.State.Channel(channelID)
	if err != nil {
		ch, err = s.Channel(channelID)
		if err != nil {
			return fmt.Sprintf("スキップ（チャンネル取得失敗: %v）", err), 0
		}
	}

	switch ch.Type {
	case discordgo.ChannelTypeDM, discordgo.ChannelTypeGroupDM:
		return "スキップ（DM）", 0
	}

	if !service.IsChannelTypeEnabled(guildID, ch.Type) {
		return "スキップ（このチャンネルタイプでは検出無効）", 0
	}
	if guildID == permissions.GetAdminGuildID() {
		return "スキップ（管理ギルド）", 0
	}
	if service.IsMute(channelID) || (ch.ParentID != "" && service.IsMute(ch.ParentID)) {
		return "スキップ（ミュート中）", 0
	}
	if service.IsDetectionOptedOut(guildID, msg.Author.ID) {
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
	}

	saveCandidates = len(collectSavableMatches(matches, doubleShots))
	return "検出あり\n" + strings.Join(lines, "\n"), saveCandidates
}

func collectSavableMatches(matches []string, doubleShots []detect.DoubleShot) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(match string) {
		parts := strings.Split(match, " ")
		if len(parts) != 3 || service.IsExcludedSenryu(match) || seen[match] {
			return
		}
		seen[match] = true
		out = append(out, match)
	}
	for _, ds := range doubleShots {
		add(ds.First)
		add(ds.Second)
	}
	for _, match := range matches {
		add(match)
	}
	return out
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
