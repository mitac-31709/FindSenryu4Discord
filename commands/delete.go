package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/service"
)

const (
	DeleteSelectCustomID       = "delete_select"
	DeleteConfirmPrefix        = "delete_confirm:"
	DeleteCancelCustomID       = "delete_cancel"
	DeletePagePrefix           = "delete_page:"
	DeleteBulkConfirmPrefix    = "delete_bulk_confirm:"
	DeleteBulkCancelCustomID   = "delete_bulk_cancel"

	deletePageSize   = 25
	deleteDateLayout = "2006-01-02"
)

// HandleDeleteCommand handles the /delete slash command (select / bulk subcommands).
func HandleDeleteCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("delete")

	if i.GuildID == "" {
		respondError(s, i, "このコマンドはサーバー内でのみ使用できます")
		return
	}

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		respondError(s, i, "サブコマンドを指定してください")
		return
	}

	switch options[0].Name {
	case "select":
		handleDeleteSelectCommand(s, i, options[0].Options)
	case "bulk":
		handleDeleteBulkCommand(s, i, options[0].Options)
	default:
		respondError(s, i, "不明なサブコマンドです")
	}
}

func handleDeleteSelectCommand(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	userID := getUserID(i)
	targetUserID := ""
	for _, opt := range options {
		if opt.Name == "user" {
			if u := opt.UserValue(s); u != nil {
				targetUserID = u.ID
			}
		}
	}
	if targetUserID == "" {
		respondError(s, i, "削除対象のユーザーを指定してください")
		return
	}

	// 他人の川柳を削除する場合は管理者権限が必要
	if targetUserID != userID && !isServerAdmin(i) {
		respondError(s, i, "他のユーザーの川柳を削除する権限がありません")
		return
	}

	// Deferred response (ephemeral)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	total, err := service.CountSenryusByAuthor(i.GuildID, targetUserID)
	if err != nil {
		logger.Error("Failed to count senryus for delete", "error", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("川柳の取得に失敗しました"),
		})
		return
	}

	if total == 0 {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("削除できる川柳がありません"),
		})
		return
	}

	content, components := buildDeletePage(i.GuildID, targetUserID, 0, total)
	if components == nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("川柳の取得に失敗しました"),
		})
		return
	}

	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    &content,
		Components: components,
	})
}

func handleDeleteBulkCommand(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	userID := getUserID(i)

	var targetUserID, fromStr, toStr string
	for _, opt := range options {
		switch opt.Name {
		case "user":
			if u := opt.UserValue(s); u != nil {
				targetUserID = u.ID
			}
		case "from":
			fromStr = strings.TrimSpace(opt.StringValue())
		case "to":
			toStr = strings.TrimSpace(opt.StringValue())
		}
	}

	if targetUserID == "" && fromStr == "" && toStr == "" {
		respondError(s, i, "ユーザーまたは期間（from / to）のいずれかを指定してください")
		return
	}

	// 他人・全ユーザー対象は管理者のみ
	if targetUserID == "" || targetUserID != userID {
		if !isServerAdmin(i) {
			respondError(s, i, "他のユーザーの川柳を削除する権限がありません")
			return
		}
	}

	from, to, errMsg := parseDeleteDateRange(fromStr, toStr)
	if errMsg != "" {
		respondError(s, i, errMsg)
		return
	}

	filter := service.SenryuFilter{
		ServerID: i.GuildID,
		AuthorID: targetUserID,
		From:     from,
		To:       to,
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	total, err := service.CountSenryusFiltered(filter)
	if err != nil {
		logger.Error("Failed to count senryus for bulk delete", "error", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("川柳の取得に失敗しました"),
		})
		return
	}

	if total == 0 {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("削除できる川柳がありません"),
		})
		return
	}

	content := buildBulkDeleteConfirmText(targetUserID, fromStr, toStr, total)
	confirmID := fmt.Sprintf("%s%s:%s:%s", DeleteBulkConfirmPrefix, targetUserID, fromStr, toStr)

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "一括削除する",
					Style:    discordgo.DangerButton,
					CustomID: confirmID,
				},
				discordgo.Button{
					Label:    "キャンセル",
					Style:    discordgo.SecondaryButton,
					CustomID: DeleteBulkCancelCustomID,
				},
			},
		},
	}

	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    &content,
		Components: &components,
	})
}

func buildBulkDeleteConfirmText(targetUserID, fromStr, toStr string, total int) string {
	var parts []string
	if targetUserID != "" {
		parts = append(parts, fmt.Sprintf("ユーザー: <@%s>", targetUserID))
	} else {
		parts = append(parts, "ユーザー: 全員")
	}
	if fromStr != "" || toStr != "" {
		fromLabel := fromStr
		if fromLabel == "" {
			fromLabel = "指定なし"
		}
		toLabel := toStr
		if toLabel == "" {
			toLabel = "指定なし"
		}
		parts = append(parts, fmt.Sprintf("期間: %s 〜 %s", fromLabel, toLabel))
	} else {
		parts = append(parts, "期間: すべて")
	}
	parts = append(parts, fmt.Sprintf("対象: **%d件**", total))
	return strings.Join(parts, "\n") + "\n\nこの条件に一致する川柳を一括削除しますか？"
}

// parseDeleteDateRange parses YYYY-MM-DD dates in JST into [from, to).
// Empty strings mean no bound. Returns a user-facing error message on failure.
func parseDeleteDateRange(fromStr, toStr string) (time.Time, time.Time, string) {
	jst := deleteJST()

	var from, to time.Time
	if fromStr != "" {
		t, err := time.ParseInLocation(deleteDateLayout, fromStr, jst)
		if err != nil {
			return time.Time{}, time.Time{}, "from は YYYY-MM-DD 形式で指定してください"
		}
		from = t
	}
	if toStr != "" {
		t, err := time.ParseInLocation(deleteDateLayout, toStr, jst)
		if err != nil {
			return time.Time{}, time.Time{}, "to は YYYY-MM-DD 形式で指定してください"
		}
		// Inclusive end date → exclusive next-day bound
		to = t.AddDate(0, 0, 1)
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return time.Time{}, time.Time{}, "from は to 以前の日付を指定してください"
	}
	return from, to, ""
}

func deleteJST() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return time.FixedZone("JST", 9*60*60)
	}
	return loc
}

// HandleDeletePage handles pagination button clicks for delete
func HandleDeletePage(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	parts := strings.SplitN(strings.TrimPrefix(data.CustomID, DeletePagePrefix), ":", 3)
	if len(parts) != 3 {
		respondComponentUpdate(s, i, "無効な操作です")
		return
	}

	guildID := parts[0]
	targetUserID := parts[1]

	// 権限チェック: ボタン押下者が元のコマンド実行者または管理者であることを確認
	if getUserID(i) != targetUserID && !isServerAdmin(i) {
		respondEphemeral(s, i, "他のユーザーの削除操作を行う権限がありません")
		return
	}

	page, err := strconv.Atoi(parts[2])
	if err != nil || page < 0 {
		respondComponentUpdate(s, i, "無効な操作です")
		return
	}

	total, err := service.CountSenryusByAuthor(guildID, targetUserID)
	if err != nil {
		logger.Error("Failed to count senryus for delete page", "error", err)
		respondComponentUpdate(s, i, "川柳の取得に失敗しました")
		return
	}

	if total == 0 {
		respondComponentUpdate(s, i, "削除できる川柳がありません")
		return
	}

	content, components := buildDeletePage(guildID, targetUserID, page, total)
	if components == nil {
		respondComponentUpdate(s, i, "川柳の取得に失敗しました")
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: componentsToSlice(components),
		},
	})
}

// buildDeletePage builds the select menu and pagination buttons for a given page.
// Returns the message content and components. components is nil on error.
func buildDeletePage(guildID, targetUserID string, page, total int) (string, *[]discordgo.MessageComponent) {
	if total <= 0 {
		return "削除できる川柳がありません", nil
	}

	totalPages := (total + deletePageSize - 1) / deletePageSize
	if page >= totalPages {
		page = totalPages - 1
	}

	offset := page * deletePageSize
	senryus, err := service.GetSenryusByAuthorPaged(guildID, targetUserID, deletePageSize, offset)
	if err != nil {
		logger.Error("Failed to get senryus for delete page", "error", err)
		return "", nil
	}

	if len(senryus) == 0 {
		return "削除できる川柳がありません", nil
	}

	menuOptions := make([]discordgo.SelectMenuOption, 0, len(senryus))
	for _, sr := range senryus {
		text := fmt.Sprintf("%s %s %s", sr.Kamigo, sr.Nakasichi, sr.Simogo)
		if sr.Spoiler != nil && *sr.Spoiler {
			text = "🔒 " + text
		}
		menuOptions = append(menuOptions, discordgo.SelectMenuOption{
			Label: truncateLabel(text),
			Value: strconv.Itoa(sr.ID),
		})
	}

	var content string
	if totalPages > 1 {
		content = fmt.Sprintf("削除する川柳を選んでください（%d/%dページ, 全%d件）:", page+1, totalPages, total)
	} else {
		content = "削除する川柳を選んでください:"
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    DeleteSelectCustomID,
					Placeholder: "川柳を選択",
					Options:     menuOptions,
				},
			},
		},
	}

	// Add pagination buttons if there are multiple pages
	if totalPages > 1 {
		prevID := fmt.Sprintf("%s%s:%s:%d", DeletePagePrefix, guildID, targetUserID, page-1)
		nextID := fmt.Sprintf("%s%s:%s:%d", DeletePagePrefix, guildID, targetUserID, page+1)

		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "◀ 前へ",
					Style:    discordgo.SecondaryButton,
					CustomID: prevID,
					Disabled: page == 0,
				},
				discordgo.Button{
					Label:    "次へ ▶",
					Style:    discordgo.SecondaryButton,
					CustomID: nextID,
					Disabled: page >= totalPages-1,
				},
			},
		})
	}

	return content, &components
}

// componentsToSlice converts *[]discordgo.MessageComponent to []discordgo.MessageComponent.
func componentsToSlice(c *[]discordgo.MessageComponent) []discordgo.MessageComponent {
	if c == nil {
		return nil
	}
	return *c
}

// truncateLabel truncates a label to fit Discord's 100-character limit for SelectMenuOption.
func truncateLabel(s string) string {
	r := []rune(s)
	if len(r) <= 100 {
		return s
	}
	return string(r[:97]) + "..."
}

// HandleDeleteSelectMenu handles the select menu interaction for delete
func HandleDeleteSelectMenu(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		return
	}

	senryuID, err := strconv.Atoi(data.Values[0])
	if err != nil {
		respondComponentUpdate(s, i, "無効な選択です")
		return
	}

	senryu, err := service.GetSenryuByID(senryuID, i.GuildID)
	if err != nil {
		if errors.Is(err, service.ErrSenryuNotFound) {
			respondComponentUpdate(s, i, "川柳が見つかりませんでした")
		} else {
			respondComponentUpdate(s, i, "川柳の取得に失敗しました")
		}
		return
	}

	var text string
	if senryu.Spoiler != nil && *senryu.Spoiler {
		text = fmt.Sprintf("||「%s %s %s」||を削除しますか？", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	} else {
		text = fmt.Sprintf("「%s %s %s」を削除しますか？", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content: text,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label:    "削除する",
							Style:    discordgo.DangerButton,
							CustomID: DeleteConfirmPrefix + data.Values[0],
						},
						discordgo.Button{
							Label:    "キャンセル",
							Style:    discordgo.SecondaryButton,
							CustomID: DeleteCancelCustomID,
						},
					},
				},
			},
		},
	})
}

// HandleDeleteConfirm handles the confirm button for delete
func HandleDeleteConfirm(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	idStr := strings.TrimPrefix(data.CustomID, DeleteConfirmPrefix)

	senryuID, err := strconv.Atoi(idStr)
	if err != nil {
		respondComponentUpdate(s, i, "無効な操作です")
		return
	}

	// 再度権限チェック
	senryu, err := service.GetSenryuByID(senryuID, i.GuildID)
	if err != nil {
		if errors.Is(err, service.ErrSenryuNotFound) {
			respondComponentUpdate(s, i, "川柳が見つかりませんでした（既に削除された可能性があります）")
		} else {
			respondComponentUpdate(s, i, "川柳の取得に失敗しました")
		}
		return
	}

	userID := getUserID(i)
	if senryu.AuthorID != userID && !isServerAdmin(i) {
		respondComponentUpdate(s, i, "この川柳を削除する権限がありません")
		return
	}

	if err := service.DeleteSenryu(senryuID, i.GuildID); err != nil {
		if errors.Is(err, service.ErrSenryuNotFound) {
			respondComponentUpdate(s, i, "川柳が見つかりませんでした（既に削除された可能性があります）")
		} else {
			logger.Error("Failed to delete senryu", "error", err, "id", senryuID)
			respondComponentUpdate(s, i, "川柳の削除に失敗しました")
		}
		return
	}

	var deleteText string
	if senryu.Spoiler != nil && *senryu.Spoiler {
		deleteText = fmt.Sprintf("||「%s %s %s」||を削除しました", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	} else {
		deleteText = fmt.Sprintf("「%s %s %s」を削除しました", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	}

	respondComponentUpdate(s, i, deleteText)
	sendDeleteNoticeToChat(s, i.ChannelID, deleteText)
}

// HandleDeleteCancel handles the cancel button for delete
func HandleDeleteCancel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	respondComponentUpdate(s, i, "削除をキャンセルしました")
}

// HandleDeleteBulkConfirm handles the confirm button for bulk delete.
func HandleDeleteBulkConfirm(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	payload := strings.TrimPrefix(data.CustomID, DeleteBulkConfirmPrefix)
	parts := strings.SplitN(payload, ":", 3)
	if len(parts) != 3 {
		respondComponentUpdate(s, i, "無効な操作です")
		return
	}

	targetUserID := parts[0]
	fromStr := parts[1]
	toStr := parts[2]

	userID := getUserID(i)
	if targetUserID == "" || targetUserID != userID {
		if !isServerAdmin(i) {
			respondEphemeral(s, i, "他のユーザーの川柳を削除する権限がありません")
			return
		}
	}

	from, to, errMsg := parseDeleteDateRange(fromStr, toStr)
	if errMsg != "" {
		respondComponentUpdate(s, i, errMsg)
		return
	}

	filter := service.SenryuFilter{
		ServerID: i.GuildID,
		AuthorID: targetUserID,
		From:     from,
		To:       to,
	}

	deleted, err := service.DeleteSenryusFiltered(filter)
	if err != nil {
		logger.Error("Failed to bulk delete senryus", "error", err)
		respondComponentUpdate(s, i, "川柳の一括削除に失敗しました")
		return
	}

	chatText := buildBulkDeleteResultText(targetUserID, fromStr, toStr, deleted)
	respondComponentUpdate(s, i, chatText)
	sendDeleteNoticeToChat(s, i.ChannelID, chatText)
}

// HandleDeleteBulkCancel handles the cancel button for bulk delete.
func HandleDeleteBulkCancel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	respondComponentUpdate(s, i, "一括削除をキャンセルしました")
}

func buildBulkDeleteResultText(targetUserID, fromStr, toStr string, deleted int64) string {
	var target string
	if targetUserID != "" {
		target = fmt.Sprintf("<@%s>", targetUserID)
	} else {
		target = "全員"
	}

	period := "すべて"
	if fromStr != "" || toStr != "" {
		fromLabel := fromStr
		if fromLabel == "" {
			fromLabel = "指定なし"
		}
		toLabel := toStr
		if toLabel == "" {
			toLabel = "指定なし"
		}
		period = fmt.Sprintf("%s 〜 %s", fromLabel, toLabel)
	}

	return fmt.Sprintf("%s の川柳を %d 件削除しました（期間: %s）", target, deleted, period)
}

func sendDeleteNoticeToChat(s *discordgo.Session, channelID, content string) {
	if channelID == "" || content == "" {
		return
	}
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: content,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
		Flags: discordgo.MessageFlagsSuppressEmbeds,
	}); err != nil {
		logger.Error("Failed to send delete notice to chat",
			"error", err,
			"channel_id", channelID,
		)
	}
}

func respondComponentUpdate(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    message,
			Components: []discordgo.MessageComponent{},
		},
	})
}
