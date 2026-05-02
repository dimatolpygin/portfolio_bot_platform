package workbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/example/bots-platform/internal/telegram"
)

const MaxActiveApplicationsPerUser = 3

type Vacancy struct {
	ID                 string    `json:"id"`
	ForemanID          string    `json:"foreman_id,omitempty"`
	City               string    `json:"city"`
	CityNorm           string    `json:"city_norm"`
	Address            string    `json:"address"`
	MapsURL            string    `json:"maps_url"`
	Description        string    `json:"description"`
	StartTime          string    `json:"start_time"`
	HourlyRate         int       `json:"hourly_rate"`
	MinHours           int       `json:"min_hours"`
	SlotsNeeded        int       `json:"slots_needed"`
	SlotsFilled        int       `json:"slots_filled"`
	Status             string    `json:"status"` // "active" | "closed" | "filled"
	CreatedAt          time.Time `json:"created_at"`
	StartAt            time.Time `json:"start_at,omitempty"`
	Timezone           string    `json:"timezone,omitempty"`
	WhatToBring        string    `json:"what_to_bring,omitempty"`
	CitizenshipRequired string   `json:"citizenship_required,omitempty"`
	PassportRFRequired bool      `json:"passport_rf_required,omitempty"`
}

type Application struct {
	VacancyID string    `json:"vacancy_id"`
	UserID    int64     `json:"user_id"`
	Count     int       `json:"count"`
	AppliedAt time.Time `json:"applied_at"`
	Status    string    `json:"status"` // "pending" | "accepted" | "rejected"
}

func vacancyKey(id string) string {
	return fmt.Sprintf("workbot:vacancy:%s", id)
}

func applicantsKey(vacancyID string) string {
	return fmt.Sprintf("workbot:vacancy:%s:applicants", vacancyID)
}

func applyLockKey(vacancyID string, userID int64) string {
	return fmt.Sprintf("workbot:apply:lock:%s:%d", vacancyID, userID)
}

func userApplicationsKey(userID int64) string {
	return fmt.Sprintf("workbot:user:%d:applications", userID)
}

func CreateVacancy(ctx context.Context, rdb *goredis.Client, v *Vacancy) error {
	seq, err := rdb.Incr(ctx, "workbot:seq:vacancy").Result()
	if err != nil {
		return fmt.Errorf("generate vacancy id: %w", err)
	}
	v.ID = fmt.Sprintf("wb_v_%d", seq)
	v.Status = "active"
	v.CreatedAt = time.Now()

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode vacancy: %w", err)
	}

	pipe := rdb.Pipeline()
	pipe.Set(ctx, vacancyKey(v.ID), data, 0)
	pipe.ZAdd(ctx, "workbot:vacancies:all", goredis.Z{Score: float64(v.CreatedAt.Unix()), Member: v.ID})
	pipe.SAdd(ctx, "workbot:vacancies:active", v.ID)
	_, err = pipe.Exec(ctx)
	return err
}

func GetVacancy(ctx context.Context, rdb *goredis.Client, id string) (*Vacancy, error) {
	data, err := rdb.Get(ctx, vacancyKey(id)).Bytes()
	if err == goredis.Nil {
		return nil, fmt.Errorf("vacancy %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get vacancy: %w", err)
	}
	var v Vacancy
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("decode vacancy: %w", err)
	}
	return &v, nil
}

func saveVacancy(ctx context.Context, rdb *goredis.Client, v *Vacancy) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode vacancy: %w", err)
	}
	return rdb.Set(ctx, vacancyKey(v.ID), data, 0).Err()
}

func ListAllVacancies(ctx context.Context, rdb *goredis.Client) ([]*Vacancy, error) {
	ids, err := rdb.ZRevRange(ctx, "workbot:vacancies:all", 0, -1).Result()
	if err != nil {
		return nil, err
	}
	vacancies := make([]*Vacancy, 0, len(ids))
	for _, id := range ids {
		v, err := GetVacancy(ctx, rdb, id)
		if err != nil {
			continue
		}
		vacancies = append(vacancies, v)
	}
	return vacancies, nil
}

func CountActiveVacancies(ctx context.Context, rdb *goredis.Client) (int64, error) {
	return rdb.SCard(ctx, "workbot:vacancies:active").Result()
}

func GetActiveVacanciesForCity(ctx context.Context, rdb *goredis.Client, cityNorm string) ([]*Vacancy, error) {
	ids, err := rdb.SMembers(ctx, "workbot:vacancies:active").Result()
	if err != nil {
		return nil, err
	}
	var result []*Vacancy
	for _, id := range ids {
		v, err := GetVacancy(ctx, rdb, id)
		if err != nil {
			continue
		}
		if v.CityNorm == cityNorm {
			result = append(result, v)
		}
	}
	return result, nil
}

// GetForemanTelegramChatID reads the foreman's Telegram chat ID from their Redis profile.
// Returns 0 if foreman has no chat ID configured or not found.
func GetForemanTelegramChatID(ctx context.Context, rdb *goredis.Client, foremanID string) int64 {
	if foremanID == "" {
		return 0
	}
	data, err := rdb.Get(ctx, fmt.Sprintf("workbot:foreman:%s", foremanID)).Bytes()
	if err != nil {
		return 0
	}
	var f struct {
		TelegramChatID int64 `json:"telegram_chat_id"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return 0
	}
	return f.TelegramChatID
}

// CountUserActiveApplications returns the number of active (non-rejected, non-closed) applications for a user.
func CountUserActiveApplications(ctx context.Context, rdb *goredis.Client, userID int64) (int64, error) {
	vacancies, err := GetUserApplicationVacancies(ctx, rdb, userID)
	if err != nil {
		return 0, err
	}
	return int64(len(vacancies)), nil
}

// Apply adds a user to a vacancy with an optimistic lock to prevent duplicates.
// Returns (alreadyApplied, full, tooMany, error).
func Apply(ctx context.Context, rdb *goredis.Client, vacancyID string, userID int64, count int) (alreadyApplied bool, full bool, tooMany bool, err error) {
	exists, err := rdb.HExists(ctx, applicantsKey(vacancyID), fmt.Sprintf("%d", userID)).Result()
	if err != nil {
		return false, false, false, err
	}
	if exists {
		return true, false, false, nil
	}

	activeCount, err := rdb.SCard(ctx, userApplicationsKey(userID)).Result()
	if err != nil {
		return false, false, false, err
	}
	if activeCount >= MaxActiveApplicationsPerUser {
		return false, false, true, nil
	}

	// Optimistic lock (30s TTL)
	locked, err := rdb.SetNX(ctx, applyLockKey(vacancyID, userID), "1", 30*time.Second).Result()
	if err != nil {
		return false, false, false, err
	}
	if !locked {
		return true, false, false, nil
	}

	v, err := GetVacancy(ctx, rdb, vacancyID)
	if err != nil {
		return false, false, false, err
	}

	available := v.SlotsNeeded - v.SlotsFilled
	if count > available {
		count = available
	}
	if count <= 0 {
		return false, true, false, nil
	}

	app := Application{
		VacancyID: vacancyID,
		UserID:    userID,
		Count:     count,
		AppliedAt: time.Now(),
		Status:    "pending",
	}
	appData, err := json.Marshal(app)
	if err != nil {
		return false, false, false, err
	}

	pipe := rdb.Pipeline()
	pipe.HSet(ctx, applicantsKey(vacancyID), fmt.Sprintf("%d", userID), appData)
	pipe.SAdd(ctx, userApplicationsKey(userID), vacancyID)
	_, err = pipe.Exec(ctx)
	return false, false, false, err
}

// AcceptApplication sets application status to "accepted" and increments SlotsFilled.
// Returns IDs of pending applicants displaced when the vacancy becomes filled.
func AcceptApplication(ctx context.Context, rdb *goredis.Client, vacancyID string, userID int64) ([]int64, error) {
	userKey := fmt.Sprintf("%d", userID)
	appData, err := rdb.HGet(ctx, applicantsKey(vacancyID), userKey).Bytes()
	if err != nil {
		return nil, err
	}
	var app Application
	if err := json.Unmarshal(appData, &app); err != nil {
		return nil, err
	}
	if app.Status == "accepted" {
		return nil, nil
	}
	v, err := GetVacancy(ctx, rdb, vacancyID)
	if err != nil {
		return nil, err
	}
	if v.SlotsFilled >= v.SlotsNeeded {
		return nil, fmt.Errorf("vacancy is full")
	}
	app.Status = "accepted"
	v.SlotsFilled += app.Count
	if v.SlotsFilled >= v.SlotsNeeded {
		v.Status = "filled"
	}
	newAppData, _ := json.Marshal(app)
	vacData, _ := json.Marshal(v)
	pipe := rdb.Pipeline()
	pipe.HSet(ctx, applicantsKey(vacancyID), userKey, newAppData)
	pipe.Set(ctx, vacancyKey(vacancyID), vacData, 0)

	var displaced []int64
	if v.Status == "filled" {
		allApps, _ := GetApplicants(ctx, rdb, vacancyID)
		for _, a := range allApps {
			if a.UserID != userID && a.Status == "pending" {
				displaced = append(displaced, a.UserID)
				a.Status = "rejected"
				rejData, _ := json.Marshal(a)
				pipe.HSet(ctx, applicantsKey(vacancyID), fmt.Sprintf("%d", a.UserID), rejData)
				pipe.SRem(ctx, userApplicationsKey(a.UserID), vacancyID)
			}
		}
		pipe.SRem(ctx, "workbot:vacancies:active", vacancyID)
	}
	_, err = pipe.Exec(ctx)
	return displaced, err
}

// RejectApplication sets application status to "rejected".
func RejectApplication(ctx context.Context, rdb *goredis.Client, vacancyID string, userID int64) error {
	userKey := fmt.Sprintf("%d", userID)
	appData, err := rdb.HGet(ctx, applicantsKey(vacancyID), userKey).Bytes()
	if err == goredis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	var app Application
	if err := json.Unmarshal(appData, &app); err != nil {
		return err
	}
	app.Status = "rejected"
	newAppData, _ := json.Marshal(app)
	pipe := rdb.Pipeline()
	pipe.HSet(ctx, applicantsKey(vacancyID), userKey, newAppData)
	pipe.SRem(ctx, userApplicationsKey(userID), vacancyID)
	_, err = pipe.Exec(ctx)
	return err
}

// GetUserApplication returns the user's application for a specific vacancy.
func GetUserApplication(ctx context.Context, rdb *goredis.Client, vacancyID string, userID int64) (*Application, error) {
	data, err := rdb.HGet(ctx, applicantsKey(vacancyID), fmt.Sprintf("%d", userID)).Bytes()
	if err == goredis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var app Application
	if err := json.Unmarshal(data, &app); err != nil {
		return nil, err
	}
	return &app, nil
}

// CancelApply removes a user from a vacancy applicants list and decrements SlotsFilled.
func CancelApply(ctx context.Context, rdb *goredis.Client, vacancyID string, userID int64) error {
	userKey := fmt.Sprintf("%d", userID)

	appData, err := rdb.HGet(ctx, applicantsKey(vacancyID), userKey).Bytes()
	if err == goredis.Nil {
		return nil
	}
	if err != nil {
		return err
	}

	var app Application
	if err := json.Unmarshal(appData, &app); err != nil {
		return err
	}

	v, err := GetVacancy(ctx, rdb, vacancyID)
	if err != nil {
		return err
	}

	pipe := rdb.Pipeline()
	pipe.HDel(ctx, applicantsKey(vacancyID), userKey)
	pipe.SRem(ctx, userApplicationsKey(userID), vacancyID)

	if app.Status == "accepted" {
		wasFilledBefore := v.Status == "filled"
		v.SlotsFilled -= app.Count
		if v.SlotsFilled < 0 {
			v.SlotsFilled = 0
		}
		if v.Status != "closed" {
			v.Status = "active"
		}
		vacData, _ := json.Marshal(v)
		pipe.Set(ctx, vacancyKey(vacancyID), vacData, 0)
		if wasFilledBefore && v.Status == "active" {
			pipe.SAdd(ctx, "workbot:vacancies:active", vacancyID)
		}
	}

	_, err = pipe.Exec(ctx)
	return err
}

func CloseVacancy(ctx context.Context, rdb *goredis.Client, id string) error {
	v, err := GetVacancy(ctx, rdb, id)
	if err != nil {
		return err
	}
	v.Status = "closed"
	pipe := rdb.Pipeline()
	data, _ := json.Marshal(v)
	pipe.Set(ctx, vacancyKey(id), data, 0)
	pipe.SRem(ctx, "workbot:vacancies:active", id)
	_, err = pipe.Exec(ctx)
	return err
}

func GetApplicants(ctx context.Context, rdb *goredis.Client, vacancyID string) ([]*Application, error) {
	vals, err := rdb.HGetAll(ctx, applicantsKey(vacancyID)).Result()
	if err != nil {
		return nil, err
	}
	apps := make([]*Application, 0, len(vals))
	for _, v := range vals {
		var app Application
		if err := json.Unmarshal([]byte(v), &app); err == nil {
			apps = append(apps, &app)
		}
	}
	return apps, nil
}

// GetUserApplicationVacancies returns active vacancies the user has applied to (not rejected).
// Falls back to scanning all vacancies if user set is empty (handles pre-moderation data).
func GetUserApplicationVacancies(ctx context.Context, rdb *goredis.Client, userID int64) ([]*Vacancy, error) {
	ids, err := rdb.SMembers(ctx, userApplicationsKey(userID)).Result()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		allIDs, _ := rdb.ZRange(ctx, "workbot:vacancies:all", 0, -1).Result()
		for _, vid := range allIDs {
			exists, _ := rdb.HExists(ctx, applicantsKey(vid), fmt.Sprintf("%d", userID)).Result()
			if exists {
				ids = append(ids, vid)
				rdb.SAdd(ctx, userApplicationsKey(userID), vid)
			}
		}
	}
	vacancies := make([]*Vacancy, 0, len(ids))
	for _, id := range ids {
		app, _ := GetUserApplication(ctx, rdb, id, userID)
		if app != nil && app.Status == "rejected" {
			continue
		}
		v, err := GetVacancy(ctx, rdb, id)
		if err != nil {
			continue
		}
		if v.Status == "closed" {
			continue
		}
		vacancies = append(vacancies, v)
	}
	return vacancies, nil
}

// SendVacancyNotification sends the vacancy message to all workers in the vacancy's city.
func SendVacancyNotification(
	ctx context.Context,
	store *RedisStore,
	tg *telegram.APIClient,
	httpClient *http.Client,
	botToken string,
	v *Vacancy,
	logger *slog.Logger,
) error {
	userIDs, err := store.GetCityMembers(ctx, v.CityNorm)
	if err != nil {
		return fmt.Errorf("get city members: %w", err)
	}

	available := v.SlotsNeeded - v.SlotsFilled
	text := formatVacancyMessage(v)
	keyboard := VacancyKeyboard(v.ID, available)

	sent := 0
	for _, uid := range userIDs {
		if store.IsApplied(ctx, v.ID, uid) {
			continue
		}
		if err := tg.SendMessageWithInlineKeyboard(ctx, botToken, uid, text, "HTML", keyboard); err != nil {
			logger.Warn("failed to notify worker", "user_id", uid, "vacancy_id", v.ID, "error", err)
			continue
		}
		sent++
	}
	logger.Info("vacancy notification sent", "vacancy_id", v.ID, "city", v.City, "sent", sent, "total", len(userIDs))
	return nil
}

func formatVacancyMessage(v *Vacancy) string {
	slots := fmt.Sprintf("%d/%d", v.SlotsFilled, v.SlotsNeeded)
	statusIcon := "🟡"
	if v.SlotsFilled >= v.SlotsNeeded {
		statusIcon = "🔴"
		slots = "Мест нет"
	} else if v.SlotsFilled == 0 {
		statusIcon = "✅"
	}

	addrLine := v.Address
	if v.MapsURL != "" {
		addrLine = fmt.Sprintf(`<a href="%s">%s</a>`, v.MapsURL, v.Address)
	}

	minEarning := v.HourlyRate * v.MinHours

	msg := fmt.Sprintf(
		"• <b>%s</b>: Нужны %s %s\n"+
			"• Адрес: 👉 %s\n"+
			"• Что делать: %s\n"+
			"• Начало: %s\n"+
			"• Оплата: %d ₽/час, минимум %d ч.\n"+
			"• Получишь минимум <b>%d ₽</b>",
		v.City, slots, statusIcon,
		addrLine,
		v.Description,
		v.StartTime,
		v.HourlyRate, v.MinHours,
		minEarning,
	)
	if v.CitizenshipRequired != "" {
		msg += "\n• Гражданство: " + v.CitizenshipRequired
	}
	if v.PassportRFRequired {
		msg += "\n• Нужен паспорт РФ"
	}
	if v.WhatToBring != "" {
		msg += "\n• Взять с собой: " + v.WhatToBring
	}
	return msg
}
