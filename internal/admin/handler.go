package admin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	redisinfra "github.com/example/bots-platform/internal/infra/redis"
	"github.com/example/bots-platform/internal/bots/modules/workbot"
	"github.com/example/bots-platform/internal/telegram"
)

type Handler struct {
	store           *AdminStore
	tg              *telegram.APIClient
	httpClient      *http.Client
	botToken        string
	adminToken      string
	superAdminLogin string
	logger          *slog.Logger
	tmpl            *templateRenderer
}

func NewHandler(
	redisClient *redisinfra.Client,
	tg *telegram.APIClient,
	httpClient *http.Client,
	botToken string,
	adminToken string,
	superAdminLogin string,
	logger *slog.Logger,
) (*Handler, error) {
	tmpl, err := newTemplateRenderer(logger)
	if err != nil {
		return nil, fmt.Errorf("parse admin templates: %w", err)
	}
	return &Handler{
		store:           newAdminStore(redisClient),
		tg:              tg,
		httpClient:      httpClient,
		botToken:        botToken,
		adminToken:      adminToken,
		superAdminLogin: superAdminLogin,
		logger:          logger,
		tmpl:            tmpl,
	}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/login", h.showLogin)
	mux.HandleFunc("POST /admin/login", h.doLogin)
	mux.HandleFunc("GET /admin/logout", h.doLogout)

	mux.Handle("GET /admin/", h.auth(h.dashboard))
	mux.Handle("GET /admin/workers", h.auth(h.listWorkers))
	mux.Handle("GET /admin/vacancies", h.auth(h.listVacancies))
	mux.Handle("GET /admin/vacancies/new", h.auth(h.newVacancyForm))
	mux.Handle("POST /admin/vacancies", h.auth(h.createVacancy))
	mux.Handle("GET /admin/vacancies/{id}", h.auth(h.vacancyDetail))
	mux.Handle("POST /admin/vacancies/{id}/close", h.auth(h.closeVacancy))
	mux.Handle("POST /admin/vacancies/{id}/notify", h.auth(h.notifyVacancy))
	mux.Handle("POST /admin/vacancies/{id}/remind", h.auth(h.remindVacancy))
	mux.Handle("POST /admin/vacancies/{id}/message/{user_id}", h.auth(h.messageWorker))
	mux.Handle("POST /admin/vacancies/{id}/applicants/{user_id}/accept", h.auth(h.acceptApplicant))
	mux.Handle("POST /admin/vacancies/{id}/applicants/{user_id}/reject", h.auth(h.rejectApplicant))

	mux.Handle("GET /admin/notifications", h.auth(h.listNotifications))
	mux.Handle("POST /admin/notifications/read", h.auth(h.markNotificationsRead))

	mux.Handle("GET /admin/workers/{user_id}/chat", h.auth(h.workerChat))
	mux.Handle("POST /admin/workers/{user_id}/chat", h.auth(h.sendWorkerChat))
	mux.Handle("POST /admin/workers/{user_id}/delete", h.superAdminOnly(h.deleteWorker))

	mux.Handle("GET /admin/foremans", h.superAdminOnly(h.listForemans))
	mux.Handle("GET /admin/foremans/new", h.superAdminOnly(h.newForemanForm))
	mux.Handle("POST /admin/foremans", h.superAdminOnly(h.createForeman))
	mux.Handle("POST /admin/foremans/{id}/delete", h.superAdminOnly(h.deleteForeman))
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workerCount, _ := h.store.WorkerCount(ctx)
	activeVacancies, _ := h.store.ActiveVacancyCount(ctx)
	todayApps, _ := h.store.TodayApplicationCount(ctx)

	data := h.pageData(r, "dashboard")
	data["WorkerCount"] = workerCount
	data["ActiveVacancies"] = activeVacancies
	data["TodayApplications"] = todayApps
	data["Flash"] = r.URL.Query().Get("flash")
	h.tmpl.Render(w, "dashboard.html", data)
}

func (h *Handler) listWorkers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFromCtx(r)
	var workers []*workbot.UserProfile
	var err error
	if sess != nil && sess.Role == "foreman" {
		workers, err = h.store.ListWorkersByCity(ctx, sess.CityNorm)
	} else {
		workers, err = h.store.ListWorkers(ctx)
	}
	if err != nil {
		h.logger.Error("list workers", "error", err)
		workers = nil
	}
	phoneQuery := strings.TrimSpace(r.URL.Query().Get("phone"))
	if phoneQuery != "" {
		filtered := workers[:0]
		for _, w := range workers {
			if strings.Contains(w.Phone, phoneQuery) {
				filtered = append(filtered, w)
			}
		}
		workers = filtered
	}
	data := h.pageData(r, "workers")
	data["Workers"] = workers
	data["PhoneQuery"] = phoneQuery
	h.tmpl.Render(w, "workers.html", data)
}

func (h *Handler) listVacancies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFromCtx(r)
	var vacancies []*workbot.Vacancy
	var err error
	if sess != nil && sess.Role == "foreman" {
		vacancies, err = h.store.ListForemanVacancies(ctx, sess.ForemanID)
	} else {
		vacancies, err = h.store.ListAllVacancies(ctx)
	}
	if err != nil {
		h.logger.Error("list vacancies", "error", err)
		vacancies = nil
	}
	data := h.pageData(r, "vacancies")
	data["Vacancies"] = vacancies
	data["Flash"] = r.URL.Query().Get("flash")
	h.tmpl.Render(w, "vacancies.html", data)
}

func (h *Handler) newVacancyForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r, "vacancies")
	data["Flash"] = r.URL.Query().Get("flash")
	h.tmpl.Render(w, "vacancy_form.html", data)
}

func (h *Handler) createVacancy(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	slotsNeeded, _ := strconv.Atoi(r.FormValue("slots_needed"))
	hourlyRate, _ := strconv.Atoi(r.FormValue("hourly_rate"))
	minHours, _ := strconv.Atoi(r.FormValue("min_hours"))

	sess := sessionFromCtx(r)
	city := strings.TrimSpace(r.FormValue("city"))
	if city == "" && sess != nil && sess.Role == "foreman" {
		city = sess.City
	}
	address := strings.TrimSpace(r.FormValue("address"))
	description := strings.TrimSpace(r.FormValue("description"))

	if city == "" || address == "" || description == "" || slotsNeeded < 1 || hourlyRate < 1 || minHours < 1 {
		d := h.pageData(r, "vacancies")
		d["Flash"] = "Заполни все обязательные поля"
		h.tmpl.Render(w, "vacancy_form.html", d)
		return
	}

	foremanID := ""
	if sess != nil && sess.Role == "foreman" {
		foremanID = sess.ForemanID
	}

	timezone := "Europe/Moscow"
	if sess != nil && sess.Timezone != "" {
		timezone = sess.Timezone
	} else if sess != nil && sess.Role == "super_admin" {
		if tz := strings.TrimSpace(r.FormValue("timezone")); tz != "" {
			timezone = tz
		}
	}

	startAtStr := strings.TrimSpace(r.FormValue("start_at"))
	var startAt time.Time
	var startTime string
	if startAtStr != "" {
		loc, locErr := time.LoadLocation(timezone)
		if locErr != nil {
			loc = time.UTC
		}
		if t, parseErr := time.ParseInLocation("2006-01-02T15:04", startAtStr, loc); parseErr == nil {
			startAt = t.UTC()
			startTime = t.In(loc).Format("02.01.2006 15:04")
		}
	}

	v := &workbot.Vacancy{
		City:                city,
		CityNorm:            workbot.NormCity(city),
		Address:             address,
		MapsURL:             strings.TrimSpace(r.FormValue("maps_url")),
		Description:         description,
		StartTime:           startTime,
		HourlyRate:          hourlyRate,
		MinHours:            minHours,
		SlotsNeeded:         slotsNeeded,
		ForemanID:           foremanID,
		StartAt:             startAt,
		Timezone:            timezone,
		WhatToBring:         strings.TrimSpace(r.FormValue("what_to_bring")),
		CitizenshipRequired: strings.TrimSpace(r.FormValue("citizenship_required")),
		PassportRFRequired:  r.FormValue("passport_rf") == "1",
	}

	if err := h.store.CreateVacancy(r.Context(), v); err != nil {
		h.logger.Error("create vacancy", "error", err)
		d := h.pageData(r, "vacancies")
		d["Flash"] = "Ошибка при создании вакансии: " + err.Error()
		h.tmpl.Render(w, "vacancy_form.html", d)
		return
	}

	http.Redirect(w, r, "/admin/vacancies/"+v.ID+"?flash=Вакансия+создана", http.StatusSeeOther)
}

func (h *Handler) vacancyDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	sess := sessionFromCtx(r)

	v, err := h.store.GetVacancy(ctx, id)
	if err != nil {
		http.Error(w, "Вакансия не найдена", http.StatusNotFound)
		return
	}

	if sess != nil && sess.Role == "foreman" && v.ForemanID != sess.ForemanID {
		http.Error(w, "Доступ запрещён", http.StatusForbidden)
		return
	}

	applicants, err := h.store.GetApplicantsWithProfiles(ctx, id)
	if err != nil {
		applicants = nil
	}

	data := h.pageData(r, "vacancies")
	data["Vacancy"] = v
	data["Applicants"] = applicants
	data["Flash"] = r.URL.Query().Get("flash")
	h.tmpl.Render(w, "vacancy.html", data)
}

func (h *Handler) closeVacancy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	sess := sessionFromCtx(r)

	if sess != nil && sess.Role == "foreman" {
		v, err := h.store.GetVacancy(ctx, id)
		if err != nil || v.ForemanID != sess.ForemanID {
			http.Error(w, "Доступ запрещён", http.StatusForbidden)
			return
		}
	}

	if err := h.store.CloseVacancy(ctx, id); err != nil {
		h.logger.Error("close vacancy", "vacancy_id", id, "error", err)
	}
	http.Redirect(w, r, "/admin/vacancies/"+id+"?flash=Вакансия+закрыта", http.StatusSeeOther)
}

func (h *Handler) notifyVacancy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	sess := sessionFromCtx(r)

	v, err := h.store.GetVacancy(ctx, id)
	if err != nil {
		http.Error(w, "Вакансия не найдена", http.StatusNotFound)
		return
	}

	if sess != nil && sess.Role == "foreman" && v.ForemanID != sess.ForemanID {
		http.Error(w, "Доступ запрещён", http.StatusForbidden)
		return
	}

	redisStore := workbot.NewRedisStore(h.store.rdb)
	go func() {
		bgCtx := context.Background()
		if err := workbot.SendVacancyNotification(bgCtx, redisStore, h.tg, h.httpClient, h.botToken, v, h.logger); err != nil {
			h.logger.Error("send vacancy notification", "vacancy_id", id, "error", err)
		}
	}()

	http.Redirect(w, r, "/admin/vacancies/"+id+"?flash=Уведомление+отправлено", http.StatusSeeOther)
}

func (h *Handler) remindVacancy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	sess := sessionFromCtx(r)

	v, err := h.store.GetVacancy(ctx, id)
	if err != nil {
		http.Error(w, "Вакансия не найдена", http.StatusNotFound)
		return
	}

	if sess != nil && sess.Role == "foreman" && v.ForemanID != sess.ForemanID {
		http.Error(w, "Доступ запрещён", http.StatusForbidden)
		return
	}

	applicants, err := h.store.GetApplicants(ctx, id)
	if err != nil {
		h.logger.Error("get applicants for remind", "vacancy_id", id, "error", err)
		http.Redirect(w, r, "/admin/vacancies/"+id+"?flash=Ошибка+получения+заявок", http.StatusSeeOther)
		return
	}

	minEarning := v.HourlyRate * v.MinHours
	text := fmt.Sprintf(
		"🔔 <b>Напоминание о смене</b>\n\n"+
			"📍 %s, %s\n"+
			"🕐 Начало: %s\n"+
			"💰 %d ₽/час, минимум %d ч. → от <b>%d ₽</b>\n\n"+
			"Ждём тебя! Если не сможешь — сообщи заранее.",
		v.City, v.Address,
		v.StartTime,
		v.HourlyRate, v.MinHours, minEarning,
	)

	go func() {
		bgCtx := context.Background()
		sent := 0
		for _, app := range applicants {
			if app.Status != "accepted" {
				continue
			}
			if err := h.tg.SendMessageWithInlineKeyboard(bgCtx, h.botToken, app.UserID, text, "HTML", workbot.EmptyInlineKeyboard()); err != nil {
				h.logger.Warn("remind worker failed", "user_id", app.UserID, "error", err)
			} else {
				sent++
			}
		}
		h.logger.Info("remind sent", "vacancy_id", id, "count", sent)
	}()

	http.Redirect(w, r, "/admin/vacancies/"+id+"?flash=Напоминание+отправлено", http.StatusSeeOther)
}

func (h *Handler) messageWorker(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	userIDStr := r.PathValue("user_id")
	ctx := r.Context()
	sess := sessionFromCtx(r)

	v, err := h.store.GetVacancy(ctx, id)
	if err != nil {
		http.Error(w, "Вакансия не найдена", http.StatusNotFound)
		return
	}

	if sess != nil && sess.Role == "foreman" && v.ForemanID != sess.ForemanID {
		http.Error(w, "Доступ запрещён", http.StatusForbidden)
		return
	}

	var userID int64
	if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err != nil {
		http.Error(w, "Неверный user_id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	message := strings.TrimSpace(r.FormValue("message"))
	if message == "" {
		http.Redirect(w, r, "/admin/vacancies/"+id+"?flash=Сообщение+не+может+быть+пустым", http.StatusSeeOther)
		return
	}

	foremanName := "Бригадир"
	if sess != nil {
		foremanName = sess.ForemanName
	}

	text := fmt.Sprintf("[%s, оператор]:\n%s", foremanName, message)
	if err := h.tg.SendMessage(ctx, h.botToken, userID, text); err != nil {
		h.logger.Error("message worker", "user_id", userID, "error", err)
		http.Redirect(w, r, "/admin/vacancies/"+id+"?flash=Ошибка+отправки+сообщения", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin/vacancies/"+id+"?flash=Сообщение+отправлено", http.StatusSeeOther)
}

func (h *Handler) listForemans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	foremans, err := h.store.ListForemans(ctx)
	if err != nil {
		h.logger.Error("list foremans", "error", err)
		foremans = nil
	}
	data := h.pageData(r, "foremans")
	data["Foremans"] = foremans
	data["Flash"] = r.URL.Query().Get("flash")
	h.tmpl.Render(w, "foremans.html", data)
}

func (h *Handler) newForemanForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r, "foremans")
	data["Flash"] = r.URL.Query().Get("flash")
	h.tmpl.Render(w, "foreman_form.html", data)
}

func (h *Handler) createForeman(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	login := strings.TrimSpace(r.FormValue("login"))
	password := strings.TrimSpace(r.FormValue("password"))
	name := strings.TrimSpace(r.FormValue("name"))
	city := strings.TrimSpace(r.FormValue("city"))

	if login == "" || password == "" || name == "" {
		d := h.pageData(r, "foremans")
		d["Flash"] = "Логин, пароль и имя обязательны"
		h.tmpl.Render(w, "foreman_form.html", d)
		return
	}

	timezone := strings.TrimSpace(r.FormValue("timezone"))
	var tgChatID int64
	fmt.Sscanf(strings.TrimSpace(r.FormValue("telegram_chat_id")), "%d", &tgChatID)
	if _, err := h.store.CreateForeman(r.Context(), login, password, name, city, timezone, tgChatID); err != nil {
		h.logger.Error("create foreman", "error", err)
		d := h.pageData(r, "foremans")
		d["Flash"] = "Ошибка при создании: " + err.Error()
		h.tmpl.Render(w, "foreman_form.html", d)
		return
	}

	http.Redirect(w, r, "/admin/foremans?flash=Бригадир+создан", http.StatusSeeOther)
}

func (h *Handler) deleteForeman(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteForeman(r.Context(), id); err != nil {
		h.logger.Error("delete foreman", "id", id, "error", err)
	}
	http.Redirect(w, r, "/admin/foremans?flash=Бригадир+удалён", http.StatusSeeOther)
}

func (h *Handler) acceptApplicant(w http.ResponseWriter, r *http.Request) {
	vacancyID := r.PathValue("id")
	userIDStr := r.PathValue("user_id")
	ctx := r.Context()
	sess := sessionFromCtx(r)

	v, err := h.store.GetVacancy(ctx, vacancyID)
	if err != nil {
		http.Error(w, "Вакансия не найдена", http.StatusNotFound)
		return
	}
	if sess != nil && sess.Role == "foreman" && v.ForemanID != sess.ForemanID {
		http.Error(w, "Доступ запрещён", http.StatusForbidden)
		return
	}

	var userID int64
	fmt.Sscanf(userIDStr, "%d", &userID)

	displaced, err := h.store.AcceptApplication(ctx, vacancyID, userID)
	if err != nil {
		h.logger.Error("accept applicant", "error", err)
		http.Redirect(w, r, "/admin/vacancies/"+vacancyID+"?flash=Ошибка+при+принятии", http.StatusSeeOther)
		return
	}

	v, _ = h.store.GetVacancy(ctx, vacancyID)
	if v != nil {
		text := fmt.Sprintf(workbot.TxtApplicationAccepted, v.City, v.Address, v.StartTime, v.HourlyRate, v.MinHours, v.HourlyRate*v.MinHours)
		go h.tg.SendMessageWithInlineKeyboard(context.Background(), h.botToken, userID, text, "HTML", workbot.EmptyInlineKeyboard())

		if len(displaced) > 0 {
			notifyText := fmt.Sprintf(workbot.TxtVacancyFilledNotify, v.City, v.Address)
			go func(ids []int64, msg string) {
				bgCtx := context.Background()
				for _, uid := range ids {
					_ = h.tg.SendMessage(bgCtx, h.botToken, uid, msg)
				}
			}(displaced, notifyText)
		}
	}
	http.Redirect(w, r, "/admin/vacancies/"+vacancyID+"?flash=Рабочий+принят", http.StatusSeeOther)
}

func (h *Handler) deleteWorker(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("user_id")
	var userID int64
	fmt.Sscanf(userIDStr, "%d", &userID)
	if err := h.store.DeleteWorker(r.Context(), userID); err != nil {
		h.logger.Error("delete worker", "user_id", userID, "error", err)
	}
	http.Redirect(w, r, "/admin/workers?flash=Рабочий+удалён", http.StatusSeeOther)
}

func (h *Handler) workerChat(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("user_id")
	var userID int64
	fmt.Sscanf(userIDStr, "%d", &userID)

	ctx := r.Context()
	sess := sessionFromCtx(r)

	p, _ := h.store.GetWorkerProfile(ctx, userID)
	if sess != nil && sess.Role == "foreman" && p != nil && p.CityNorm != sess.CityNorm {
		http.Error(w, "Доступ запрещён", http.StatusForbidden)
		return
	}

	msgs, _ := h.store.GetChatHistory(ctx, userID)
	data := h.pageData(r, "workers")
	data["Worker"] = p
	data["UserID"] = userID
	data["Messages"] = msgs
	data["Flash"] = r.URL.Query().Get("flash")
	h.tmpl.Render(w, "worker_chat.html", data)
}

func (h *Handler) sendWorkerChat(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("user_id")
	var userID int64
	fmt.Sscanf(userIDStr, "%d", &userID)

	ctx := r.Context()
	sess := sessionFromCtx(r)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		http.Redirect(w, r, fmt.Sprintf("/admin/workers/%d/chat", userID), http.StatusSeeOther)
		return
	}

	adminName := "Оператор"
	if sess != nil {
		adminName = sess.ForemanName
	}

	msg := workbot.ChatMessage{
		FromAdmin: true,
		Text:      text,
		SentAt:    time.Now(),
		AdminName: adminName,
	}
	if err := h.store.SaveChatMessage(ctx, userID, msg); err != nil {
		h.logger.Error("save chat message", "user_id", userID, "error", err)
	}

	tgText := fmt.Sprintf("[%s]:\n%s", adminName, text)
	go h.tg.SendMessage(context.Background(), h.botToken, userID, tgText)

	http.Redirect(w, r, fmt.Sprintf("/admin/workers/%d/chat", userID), http.StatusSeeOther)
}

func (h *Handler) rejectApplicant(w http.ResponseWriter, r *http.Request) {
	vacancyID := r.PathValue("id")
	userIDStr := r.PathValue("user_id")
	ctx := r.Context()
	sess := sessionFromCtx(r)

	v, err := h.store.GetVacancy(ctx, vacancyID)
	if err != nil {
		http.Error(w, "Вакансия не найдена", http.StatusNotFound)
		return
	}
	if sess != nil && sess.Role == "foreman" && v.ForemanID != sess.ForemanID {
		http.Error(w, "Доступ запрещён", http.StatusForbidden)
		return
	}

	var userID int64
	fmt.Sscanf(userIDStr, "%d", &userID)

	if err := h.store.RejectApplication(ctx, vacancyID, userID); err != nil {
		h.logger.Error("reject applicant", "error", err)
		http.Redirect(w, r, "/admin/vacancies/"+vacancyID+"?flash=Ошибка+при+отклонении", http.StatusSeeOther)
		return
	}

	go h.tg.SendMessage(context.Background(), h.botToken, userID, workbot.TxtApplicationRejected)
	http.Redirect(w, r, "/admin/vacancies/"+vacancyID+"?flash=Заявка+отклонена", http.StatusSeeOther)
}

// pageData returns a base data map pre-filled with session and unread notification count.
func (h *Handler) pageData(r *http.Request, page string) map[string]any {
	sess := sessionFromCtx(r)
	foremanID := ""
	if sess != nil {
		foremanID = sess.ForemanID
	}
	unread := h.store.CountUnreadNotifications(r.Context(), foremanID)
	return map[string]any{
		"Page":         page,
		"Session":      sess,
		"UnreadAlerts": unread,
	}
}

func (h *Handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := sessionFromCtx(r)
	foremanID := ""
	if sess != nil {
		foremanID = sess.ForemanID
	}

	notifications, err := h.store.GetNotifications(ctx, foremanID)
	if err != nil {
		h.logger.Error("get notifications", "error", err)
		notifications = nil
	}
	_ = h.store.MarkNotificationsRead(ctx, foremanID)

	data := h.pageData(r, "notifications")
	data["Notifications"] = notifications
	data["UnreadAlerts"] = 0 // только что прочитали
	h.tmpl.Render(w, "notifications.html", data)
}

func (h *Handler) markNotificationsRead(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r)
	foremanID := ""
	if sess != nil {
		foremanID = sess.ForemanID
	}
	_ = h.store.MarkNotificationsRead(r.Context(), foremanID)
	http.Redirect(w, r, "/admin/notifications", http.StatusSeeOther)
}
