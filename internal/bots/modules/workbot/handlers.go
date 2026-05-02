package workbot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/example/bots-platform/internal/bots"
	"github.com/example/bots-platform/internal/telegram"
)

func handleChatMessage(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message) error {
	chatMsg := ChatMessage{
		FromAdmin: false,
		Text:      msg.Text,
		SentAt:    time.Now(),
	}
	_ = SaveChatMessage(ctx, bot.Deps.Redis.Raw(), msg.From.ID, chatMsg)
	return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtChatReceived)
}

func handleText(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message) error {
	if msg.From == nil {
		return nil
	}
	store := NewRedisStore(bot.Deps.Redis.Raw())
	state, err := store.GetState(ctx, msg.From.ID)
	if err != nil {
		return err
	}

	switch state {
	case StateNew:
		return handleStart(ctx, bot, msg, store)
	case StateAwaitName:
		return handleAwaitName(ctx, bot, msg, store)
	case StateAwaitDOB:
		return handleAwaitDOB(ctx, bot, msg, store)
	case StateAwaitCit:
		return handleAwaitCitizen(ctx, bot, msg, store)
	case StateAwaitPhone:
		return bot.Deps.Telegram.SendMessageWithReplyKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtPhoneRetry, PhoneKeyboard())
	case StateAwaitLoc:
		return bot.Deps.Telegram.SendMessageWithReplyKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtLocRetry, LocationKeyboard())
	case StateAwaitConfirm:
		return handleAwaitConfirmText(ctx, bot, msg, store)
	case StateAwaitEdit:
		return handleAwaitEdit(ctx, bot, msg, store)
	case StateRegistered:
		return bot.Deps.Telegram.SendMessageWithReplyKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtMainMenu, MainMenuKeyboard())
	case StateSupport:
		return handleChatMessage(ctx, bot, msg)
	case StateAwaitCityManual:
		return handleAwaitCityManual(ctx, bot, msg, store)
	default:
		return handleStart(ctx, bot, msg, store)
	}
}

func handleStart(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message, store *RedisStore) error {
	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	if p.State == StateRegistered || p.State == StateSupport {
		if p.State == StateSupport {
			p.State = StateRegistered
			_ = store.SaveProfile(ctx, p)
		}
		if msg.From.Username != "" && p.Username != msg.From.Username {
			p.Username = msg.From.Username
			_ = store.SaveProfile(ctx, p)
		}
		return bot.Deps.Telegram.SendMessageWithReplyKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtAlreadyReg, MainMenuKeyboard())
	}

	p.TelegramID = msg.From.ID
	p.FirstName = msg.From.FirstName
	p.Username = msg.From.Username
	p.State = StateAwaitName
	if err := store.SaveProfile(ctx, p); err != nil {
		return err
	}
	return bot.Deps.Telegram.SendMessageRemoveKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtWelcome)
}

func handleAwaitName(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message, store *RedisStore) error {
	name := strings.TrimSpace(msg.Text)
	if name == "" {
		return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtWelcome)
	}

	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	p.FullName = name
	p.State = StateAwaitDOB
	if err := store.SaveProfile(ctx, p); err != nil {
		return err
	}
	return bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtAskDOB, "HTML", EmptyInlineKeyboard())
}

var dobLayouts = []string{"02.01.2006", "02/01/2006", "2006-01-02"}

func handleAwaitDOB(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message, store *RedisStore) error {
	raw := strings.TrimSpace(msg.Text)
	var parsed time.Time
	for _, layout := range dobLayouts {
		t, err := time.Parse(layout, raw)
		if err == nil {
			parsed = t
			break
		}
	}
	if parsed.IsZero() {
		return bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtBadDOB, "HTML", EmptyInlineKeyboard())
	}

	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	p.DOB = parsed.Format("02.01.2006")
	p.State = StateAwaitCit
	if err := store.SaveProfile(ctx, p); err != nil {
		return err
	}
	return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtAskCitizen)
}

func handleAwaitCitizen(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message, store *RedisStore) error {
	cit := strings.TrimSpace(msg.Text)
	if cit == "" {
		return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtAskCitizen)
	}

	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	p.Citizenship = cit
	p.State = StateAwaitPhone
	if err := store.SaveProfile(ctx, p); err != nil {
		return err
	}
	return bot.Deps.Telegram.SendMessageWithReplyKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtAskPhone, PhoneKeyboard())
}

func handleContact(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message, store *RedisStore) error {
	if msg.From == nil || msg.Contact == nil {
		return nil
	}

	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}

	p.Phone = msg.Contact.PhoneNumber
	if p.EditingField == "phone" {
		p.EditingField = ""
		p.State = StateAwaitConfirm
		if err := store.SaveProfile(ctx, p); err != nil {
			return err
		}
		return sendConfirmSummary(ctx, bot, msg.Chat.ID, p)
	}

	p.State = StateAwaitLoc
	if err := store.SaveProfile(ctx, p); err != nil {
		return err
	}
	return bot.Deps.Telegram.SendMessageWithReplyKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtAskLocation, LocationKeyboard())
}

func handleLocation(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message, store *RedisStore) error {
	if msg.From == nil || msg.Location == nil {
		return nil
	}

	city, err := ReverseGeocode(ctx, bot.Deps.HTTP, msg.Location.Latitude, msg.Location.Longitude)
	if err != nil {
		bot.Logger.Warn("geocode failed", "error", err)
		p, profErr := store.GetProfile(ctx, msg.From.ID)
		if profErr != nil {
			return profErr
		}
		p.State = StateAwaitCityManual
		p.Lat = msg.Location.Latitude
		p.Lon = msg.Location.Longitude
		if saveErr := store.SaveProfile(ctx, p); saveErr != nil {
			return saveErr
		}
		return bot.Deps.Telegram.SendMessageRemoveKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtAskCityManual)
	}

	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	p.City = city
	p.CityNorm = NormCity(city)
	p.Lat = msg.Location.Latitude
	p.Lon = msg.Location.Longitude
	if p.EditingField == "location" {
		p.CityChangedAt = time.Now()
	}
	p.EditingField = ""
	p.State = StateAwaitConfirm
	if err := store.SaveProfile(ctx, p); err != nil {
		return err
	}
	return sendConfirmSummary(ctx, bot, msg.Chat.ID, p)
}

func sendConfirmSummary(ctx context.Context, bot *bots.BotRuntime, chatID int64, p *UserProfile) error {
	text := fmt.Sprintf(TxtConfirmFmt, p.FullName, p.DOB, p.Citizenship, p.Phone, p.City)
	return bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, bot.Config.Secrets.Token, chatID, text, "HTML", ConfirmProfileKeyboard())
}

func handleAwaitConfirmText(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message, store *RedisStore) error {
	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	return sendConfirmSummary(ctx, bot, msg.Chat.ID, p)
}

func handleAwaitEdit(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message, store *RedisStore) error {
	num := strings.TrimSpace(msg.Text)
	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}

	switch num {
	case "1":
		p.State = StateAwaitName
		p.EditingField = "full_name"
		if err := store.SaveProfile(ctx, p); err != nil {
			return err
		}
		return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtEditName)
	case "2":
		p.State = StateAwaitDOB
		p.EditingField = "dob"
		if err := store.SaveProfile(ctx, p); err != nil {
			return err
		}
		return bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtEditDOB, "HTML", EmptyInlineKeyboard())
	case "3":
		p.State = StateAwaitCit
		p.EditingField = "citizenship"
		if err := store.SaveProfile(ctx, p); err != nil {
			return err
		}
		return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtEditCitizen)
	case "4":
		p.State = StateAwaitPhone
		p.EditingField = "phone"
		if err := store.SaveProfile(ctx, p); err != nil {
			return err
		}
		return bot.Deps.Telegram.SendMessageWithReplyKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtEditPhone, PhoneKeyboard())
	case "5":
		if !p.CityChangedAt.IsZero() && time.Since(p.CityChangedAt) < 14*24*time.Hour {
			nextAllowed := p.CityChangedAt.Add(14 * 24 * time.Hour)
			return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, msg.Chat.ID,
				fmt.Sprintf(TxtCityChangeCooldown, nextAllowed.Format("02.01.2006")))
		}
		p.State = StateAwaitLoc
		p.EditingField = "location"
		if err := store.SaveProfile(ctx, p); err != nil {
			return err
		}
		return bot.Deps.Telegram.SendMessageWithReplyKeyboard(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtEditLocation, LocationKeyboard())
	default:
		return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtEditBadNum)
	}
}

func handleMyJobs(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message) error {
	rdb := bot.Deps.Redis.Raw()
	token := bot.Config.Secrets.Token

	vacancies, err := GetUserApplicationVacancies(ctx, rdb, msg.From.ID)
	if err != nil || len(vacancies) == 0 {
		return bot.Deps.Telegram.SendMessage(ctx, token, msg.Chat.ID, TxtMyJobsEmpty)
	}

	var lines []string
	for i, v := range vacancies {
		minEarning := v.HourlyRate * v.MinHours
		statusLabel := "⏳ ожидает"
		if app, _ := GetUserApplication(ctx, rdb, v.ID, msg.From.ID); app != nil && app.Status == "accepted" {
			statusLabel = "✅ принят"
		}
		addrPart := v.Address
		if v.MapsURL != "" {
			addrPart = fmt.Sprintf(`<a href="%s">%s</a>`, v.MapsURL, v.Address)
		}
		line := fmt.Sprintf(
			"%d. %s\n📍 %s, %s\n🕐 Начало: %s\n💼 %s\n💰 %d ₽/час · мин. %d ч. · от %d ₽",
			i+1, statusLabel, v.City, addrPart, v.StartTime, v.Description,
			v.HourlyRate, v.MinHours, minEarning,
		)
		if v.WhatToBring != "" {
			line += "\n🎒 Взять: " + v.WhatToBring
		}
		lines = append(lines, line)
	}
	text := fmt.Sprintf(TxtMyJobsFmt, strings.Join(lines, "\n\n"))
	keyboard := MyJobsKeyboard(vacancies)
	return bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, token, msg.Chat.ID, text, "HTML", keyboard)
}

func handleCallback(ctx context.Context, bot *bots.BotRuntime, cq *telegram.CallbackQuery) error {
	data := cq.Data
	parts := strings.SplitN(data, ":", 5)
	if len(parts) < 2 || parts[0] != "workbot" {
		return nil
	}

	store := NewRedisStore(bot.Deps.Redis.Raw())
	chatID := int64(0)
	msgID := int64(0)
	if cq.Message != nil {
		chatID = cq.Message.Chat.ID
		msgID = cq.Message.MessageID
	}

	action := parts[1]
	token := bot.Config.Secrets.Token

	switch action {
	case "confirm":
		return handleConfirmProfile(ctx, bot, cq, store, chatID, token)

	case "edit":
		p, err := store.GetProfile(ctx, cq.From.ID)
		if err != nil {
			return err
		}
		p.State = StateAwaitEdit
		if err := store.SaveProfile(ctx, p); err != nil {
			return err
		}
		_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, "")
		return bot.Deps.Telegram.SendMessage(ctx, token, chatID, TxtEditPrompt)

	case "apply":
		if len(parts) < 4 {
			return nil
		}
		vacancyID := parts[2]
		var count int
		fmt.Sscanf(parts[3], "%d", &count)
		return handleApplyRequest(ctx, bot, cq, store, chatID, msgID, token, vacancyID, count)

	case "confirm_apply":
		if len(parts) < 4 {
			return nil
		}
		vacancyID := parts[2]
		var count int
		fmt.Sscanf(parts[3], "%d", &count)
		return handleConfirmApply(ctx, bot, cq, store, chatID, msgID, token, vacancyID, count)

	case "cancel_apply":
		if len(parts) < 3 {
			return nil
		}
		vacancyID := parts[2]
		v, err := GetVacancy(ctx, bot.Deps.Redis.Raw(), vacancyID)
		if err != nil {
			_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, "Вакансия не найдена")
			return nil
		}
		available := v.SlotsNeeded - v.SlotsFilled
		keyboard := VacancyKeyboard(vacancyID, available)
		_ = bot.Deps.Telegram.EditMessageReplyMarkup(ctx, token, chatID, msgID, keyboard)
		return bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, TxtApplyCancelled)

	case "cancel_myjob":
		if len(parts) < 3 {
			return nil
		}
		vacancyID := parts[2]
		return handleCancelMyJob(ctx, bot, cq, chatID, msgID, token, vacancyID)

	case "profile_edit":
		p, err := store.GetProfile(ctx, cq.From.ID)
		if err != nil {
			return err
		}
		p.State = StateAwaitEdit
		if err := store.SaveProfile(ctx, p); err != nil {
			return err
		}
		_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, "")
		return bot.Deps.Telegram.SendMessage(ctx, token, chatID, TxtEditPrompt)
	}

	return nil
}

func handleConfirmProfile(ctx context.Context, bot *bots.BotRuntime, cq *telegram.CallbackQuery, store *RedisStore, chatID int64, token string) error {
	p, err := store.GetProfile(ctx, cq.From.ID)
	if err != nil {
		return err
	}
	p.State = StateRegistered
	p.RegisteredAt = time.Now()
	p.EditingField = ""
	if err := store.RegisterUser(ctx, p); err != nil {
		return err
	}
	_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, "")
	return bot.Deps.Telegram.SendMessageWithReplyKeyboard(ctx, token, chatID, TxtRegistered, MainMenuKeyboard())
}

func handleApplyRequest(ctx context.Context, bot *bots.BotRuntime, cq *telegram.CallbackQuery, store *RedisStore, chatID, msgID int64, token, vacancyID string, count int) error {
	p, err := store.GetProfile(ctx, cq.From.ID)
	if err != nil {
		return err
	}
	if p.State != StateRegistered {
		_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, TxtNotReg)
		return nil
	}

	v, err := GetVacancy(ctx, bot.Deps.Redis.Raw(), vacancyID)
	if err != nil {
		_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, "Вакансия не найдена")
		return nil
	}

	text := fmt.Sprintf(TxtConfirmApplyFmt, v.City, v.Address)
	keyboard := ApplyConfirmKeyboard(vacancyID, count)
	_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, "")
	return bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, token, chatID, text, "HTML", keyboard)
}

func handleConfirmApply(ctx context.Context, bot *bots.BotRuntime, cq *telegram.CallbackQuery, store *RedisStore, chatID, msgID int64, token, vacancyID string, count int) error {
	alreadyApplied, full, tooMany, err := Apply(ctx, bot.Deps.Redis.Raw(), vacancyID, cq.From.ID, count)
	if err != nil {
		return err
	}

	_ = bot.Deps.Telegram.EditMessageReplyMarkup(ctx, token, chatID, msgID, EmptyInlineKeyboard())

	switch {
	case alreadyApplied:
		return bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, TxtAlreadyApply)
	case full:
		return bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, TxtVacancyFull)
	case tooMany:
		_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, "")
		return bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, token, chatID, TxtTooManyApplications, "", EmptyInlineKeyboard())
	default:
		_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, "")
		v, err := GetVacancy(ctx, bot.Deps.Redis.Raw(), vacancyID)
		if err != nil {
			return bot.Deps.Telegram.SendMessage(ctx, token, chatID, TxtApplyOk)
		}
		minEarning := v.HourlyRate * v.MinHours
		receipt := fmt.Sprintf(TxtApplyReceiptFmt,
			v.City, v.Address,
			v.StartTime,
			v.HourlyRate, v.MinHours, minEarning,
		)
		return bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, token, chatID, receipt, "HTML", EmptyInlineKeyboard())
	}
}

func handleAwaitCityManual(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message, store *RedisStore) error {
	city := strings.TrimSpace(msg.Text)
	if city == "" {
		return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, msg.Chat.ID, TxtBadCityManual)
	}
	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	p.City = city
	p.CityNorm = NormCity(city)
	if p.EditingField == "location" {
		p.CityChangedAt = time.Now()
	}
	p.EditingField = ""
	p.State = StateAwaitConfirm
	if err := store.SaveProfile(ctx, p); err != nil {
		return err
	}
	return sendConfirmSummary(ctx, bot, msg.Chat.ID, p)
}

func handleVacancies(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message) error {
	rdb := bot.Deps.Redis.Raw()
	token := bot.Config.Secrets.Token
	store := NewRedisStore(rdb)

	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	if p.State != StateRegistered {
		return bot.Deps.Telegram.SendMessage(ctx, token, msg.Chat.ID, TxtNotReg)
	}
	if p.CityNorm == "" {
		return bot.Deps.Telegram.SendMessage(ctx, token, msg.Chat.ID, TxtNoCitySet)
	}

	activeCount, _ := CountUserActiveApplications(ctx, rdb, msg.From.ID)
	if activeCount >= MaxActiveApplicationsPerUser {
		return bot.Deps.Telegram.SendMessage(ctx, token, msg.Chat.ID, TxtTooManyApplications)
	}

	all, err := GetActiveVacanciesForCity(ctx, rdb, p.CityNorm)
	if err != nil {
		return bot.Deps.Telegram.SendMessage(ctx, token, msg.Chat.ID, TxtVacanciesEmpty)
	}

	var vacancies []*Vacancy
	for _, v := range all {
		if !store.IsApplied(ctx, v.ID, msg.From.ID) {
			vacancies = append(vacancies, v)
		}
	}

	if len(vacancies) == 0 {
		return bot.Deps.Telegram.SendMessage(ctx, token, msg.Chat.ID, TxtVacanciesEmpty)
	}

	_ = bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, token, msg.Chat.ID, TxtVacanciesHeader, "HTML", EmptyInlineKeyboard())
	for _, v := range vacancies {
		available := v.SlotsNeeded - v.SlotsFilled
		text := formatVacancyMessage(v)
		kb := VacancyKeyboard(v.ID, available)
		_ = bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, token, msg.Chat.ID, text, "HTML", kb)
	}
	return nil
}

func handleProfile(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message) error {
	token := bot.Config.Secrets.Token
	store := NewRedisStore(bot.Deps.Redis.Raw())

	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	if p.State != StateRegistered {
		return bot.Deps.Telegram.SendMessage(ctx, token, msg.Chat.ID, TxtNotReg)
	}

	text := fmt.Sprintf(TxtProfileFmt, p.FullName, p.DOB, p.Citizenship, p.Phone, p.City)
	return bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, token, msg.Chat.ID, text, "HTML", ProfileKeyboard())
}

func handleSupport(ctx context.Context, bot *bots.BotRuntime, msg *telegram.Message) error {
	token := bot.Config.Secrets.Token
	store := NewRedisStore(bot.Deps.Redis.Raw())

	p, err := store.GetProfile(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	if p.State != StateRegistered && p.State != StateSupport {
		return bot.Deps.Telegram.SendMessage(ctx, token, msg.Chat.ID, TxtNotReg)
	}
	if p.State == StateSupport {
		return bot.Deps.Telegram.SendMessage(ctx, token, msg.Chat.ID, TxtSupportMode)
	}

	p.State = StateSupport
	if err := store.SaveProfile(ctx, p); err != nil {
		return err
	}
	return bot.Deps.Telegram.SendMessageRemoveKeyboard(ctx, token, msg.Chat.ID, TxtSupportMode)
}

func handleCancelMyJob(ctx context.Context, bot *bots.BotRuntime, cq *telegram.CallbackQuery, chatID, msgID int64, token, vacancyID string) error {
	rdb := bot.Deps.Redis.Raw()

	// Получаем вакансию до отмены, чтобы иметь данные для уведомления
	vacancy, _ := GetVacancy(ctx, rdb, vacancyID)

	if err := CancelApply(ctx, rdb, vacancyID, cq.From.ID); err != nil {
		bot.Logger.Error("cancel apply", "vacancy_id", vacancyID, "user_id", cq.From.ID, "error", err)
		_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, TxtCancelApplyFail)
		return nil
	}

	// Уведомляем оператора вакансии
	if vacancy != nil {
		store := NewRedisStore(rdb)
		p, _ := store.GetProfile(ctx, cq.From.ID)
		username := ""
		fullName := fmt.Sprintf("id:%d", cq.From.ID)
		if p != nil {
			fullName = p.FullName
			username = p.Username
		}

		// Telegram-алерт если у оператора настроен chat ID
		if adminChatID := GetForemanTelegramChatID(ctx, rdb, vacancy.ForemanID); adminChatID != 0 {
			notifyText := fmt.Sprintf(TxtAdminCancelNotify,
				fullName, username, cq.From.ID,
				vacancy.ID,
				vacancy.City, vacancy.Address,
				vacancy.StartTime,
				vacancy.Description,
			)
			go bot.Deps.Telegram.SendMessageWithInlineKeyboard(ctx, token, adminChatID, notifyText, "HTML", EmptyInlineKeyboard())
		}

		// Веб-алерт в панель оператора
		_ = SaveAdminNotification(ctx, rdb, vacancy.ForemanID, AdminNotification{
			Type:        "cancel_apply",
			VacancyID:   vacancy.ID,
			UserID:      cq.From.ID,
			FullName:    fullName,
			Username:    username,
			City:        vacancy.City,
			Address:     vacancy.Address,
			StartTime:   vacancy.StartTime,
			Description: vacancy.Description,
		})
	}

	_ = bot.Deps.Telegram.AnswerCallbackQuery(ctx, token, cq.ID, TxtCancelApplyOk)

	vacancies, err := GetUserApplicationVacancies(ctx, rdb, cq.From.ID)
	if err != nil || len(vacancies) == 0 {
		_ = bot.Deps.Telegram.EditMessageText(ctx, token, chatID, msgID, TxtMyJobsEmpty, "", EmptyInlineKeyboard())
		return nil
	}

	var lines []string
	for i, v := range vacancies {
		minEarning := v.HourlyRate * v.MinHours
		statusLabel := "⏳ ожидает"
		if app, _ := GetUserApplication(ctx, rdb, v.ID, cq.From.ID); app != nil && app.Status == "accepted" {
			statusLabel = "✅ принят"
		}
		addrPart := v.Address
		if v.MapsURL != "" {
			addrPart = fmt.Sprintf(`<a href="%s">%s</a>`, v.MapsURL, v.Address)
		}
		line := fmt.Sprintf(
			"%d. %s\n📍 %s, %s\n🕐 Начало: %s\n💼 %s\n💰 %d ₽/час · мин. %d ч. · от %d ₽",
			i+1, statusLabel, v.City, addrPart, v.StartTime, v.Description,
			v.HourlyRate, v.MinHours, minEarning,
		)
		if v.WhatToBring != "" {
			line += "\n🎒 Взять: " + v.WhatToBring
		}
		lines = append(lines, line)
	}
	text := fmt.Sprintf(TxtMyJobsFmt, strings.Join(lines, "\n\n"))
	keyboard := MyJobsKeyboard(vacancies)
	_ = bot.Deps.Telegram.EditMessageText(ctx, token, chatID, msgID, text, "HTML", keyboard)
	return nil
}
