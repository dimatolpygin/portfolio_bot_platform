package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	redisinfra "github.com/example/bots-platform/internal/infra/redis"
	"github.com/example/bots-platform/internal/bots/modules/workbot"
)

type ForemanProfile struct {
	ID             string    `json:"id"`
	Login          string    `json:"login"`
	PassHash       string    `json:"pass_hash"`
	Name           string    `json:"name"`
	City           string    `json:"city"`
	Timezone       string    `json:"timezone,omitempty"`
	TelegramChatID int64     `json:"telegram_chat_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type ApplicantView struct {
	UserID             int64
	FullName           string
	Username           string
	Phone              string
	Count              int
	AppliedAt          time.Time
	Status             string
	ActiveApplications int
}

type AdminStore struct {
	rdb *goredis.Client
	ws  *workbot.RedisStore
}

func newAdminStore(redisClient *redisinfra.Client) *AdminStore {
	rdb := redisClient.Raw()
	return &AdminStore{
		rdb: rdb,
		ws:  workbot.NewRedisStore(rdb),
	}
}

func foremanKey(id string) string {
	return fmt.Sprintf("workbot:foreman:%s", id)
}

func foremanVacanciesKey(id string) string {
	return fmt.Sprintf("workbot:foreman:%s:vacancies", id)
}

func (s *AdminStore) CreateForeman(ctx context.Context, login, password, name, city, timezone string, telegramChatID int64) (*ForemanProfile, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	seq, err := s.rdb.Incr(ctx, "workbot:seq:foreman").Result()
	if err != nil {
		return nil, fmt.Errorf("generate foreman id: %w", err)
	}
	if timezone == "" {
		timezone = "Europe/Moscow"
	}
	f := &ForemanProfile{
		ID:             fmt.Sprintf("wb_f_%d", seq),
		Login:          login,
		PassHash:       string(hash),
		Name:           name,
		City:           city,
		Timezone:       timezone,
		TelegramChatID: telegramChatID,
		CreatedAt:      time.Now(),
	}
	data, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, foremanKey(f.ID), data, 0)
	pipe.SAdd(ctx, "workbot:foremans:all", f.ID)
	_, err = pipe.Exec(ctx)
	return f, err
}

func (s *AdminStore) DeleteForeman(ctx context.Context, id string) error {
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, foremanKey(id))
	pipe.SRem(ctx, "workbot:foremans:all", id)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *AdminStore) GetForeman(ctx context.Context, id string) (*ForemanProfile, error) {
	data, err := s.rdb.Get(ctx, foremanKey(id)).Bytes()
	if err == goredis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f ForemanProfile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *AdminStore) ListForemans(ctx context.Context) ([]*ForemanProfile, error) {
	ids, err := s.rdb.SMembers(ctx, "workbot:foremans:all").Result()
	if err != nil {
		return nil, err
	}
	foremans := make([]*ForemanProfile, 0, len(ids))
	for _, id := range ids {
		f, err := s.GetForeman(ctx, id)
		if err != nil || f == nil {
			continue
		}
		foremans = append(foremans, f)
	}
	return foremans, nil
}

func (s *AdminStore) FindForemanByLogin(ctx context.Context, login string) (*ForemanProfile, error) {
	ids, err := s.rdb.SMembers(ctx, "workbot:foremans:all").Result()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		f, err := s.GetForeman(ctx, id)
		if err != nil || f == nil {
			continue
		}
		if f.Login == login {
			return f, nil
		}
	}
	return nil, nil
}

func (s *AdminStore) WorkerCount(ctx context.Context) (int64, error) {
	return s.ws.CountWorkers(ctx)
}

func (s *AdminStore) ActiveVacancyCount(ctx context.Context) (int64, error) {
	return workbot.CountActiveVacancies(ctx, s.rdb)
}

func (s *AdminStore) TodayApplicationCount(ctx context.Context) (int64, error) {
	return s.ws.CountTodayApplications(ctx)
}

func (s *AdminStore) ListWorkers(ctx context.Context) ([]*workbot.UserProfile, error) {
	return s.ws.ListAllProfiles(ctx)
}

func (s *AdminStore) ListWorkersForForeman(ctx context.Context, foremanID string) ([]*workbot.UserProfile, error) {
	vacIDs, err := s.rdb.SMembers(ctx, foremanVacanciesKey(foremanID)).Result()
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	profiles := make([]*workbot.UserProfile, 0)
	for _, vid := range vacIDs {
		apps, err := workbot.GetApplicants(ctx, s.rdb, vid)
		if err != nil {
			continue
		}
		for _, app := range apps {
			if seen[app.UserID] {
				continue
			}
			seen[app.UserID] = true
			p, err := s.ws.GetProfile(ctx, app.UserID)
			if err != nil || p == nil {
				continue
			}
			profiles = append(profiles, p)
		}
	}
	return profiles, nil
}

func (s *AdminStore) ListAllVacancies(ctx context.Context) ([]*workbot.Vacancy, error) {
	return workbot.ListAllVacancies(ctx, s.rdb)
}

func (s *AdminStore) ListForemanVacancies(ctx context.Context, foremanID string) ([]*workbot.Vacancy, error) {
	ids, err := s.rdb.SMembers(ctx, foremanVacanciesKey(foremanID)).Result()
	if err != nil {
		return nil, err
	}
	vacancies := make([]*workbot.Vacancy, 0, len(ids))
	for _, id := range ids {
		v, err := workbot.GetVacancy(ctx, s.rdb, id)
		if err != nil {
			continue
		}
		vacancies = append(vacancies, v)
	}
	return vacancies, nil
}

func (s *AdminStore) GetVacancy(ctx context.Context, id string) (*workbot.Vacancy, error) {
	return workbot.GetVacancy(ctx, s.rdb, id)
}

func (s *AdminStore) GetApplicants(ctx context.Context, vacancyID string) ([]*workbot.Application, error) {
	return workbot.GetApplicants(ctx, s.rdb, vacancyID)
}

func (s *AdminStore) GetApplicantsWithProfiles(ctx context.Context, vacancyID string) ([]*ApplicantView, error) {
	apps, err := workbot.GetApplicants(ctx, s.rdb, vacancyID)
	if err != nil {
		return nil, err
	}
	views := make([]*ApplicantView, 0, len(apps))
	for _, app := range apps {
		view := &ApplicantView{
			UserID:    app.UserID,
			Count:     app.Count,
			AppliedAt: app.AppliedAt,
			Status:    app.Status,
		}
		p, err := s.ws.GetProfile(ctx, app.UserID)
		if err == nil && p != nil {
			view.FullName = p.FullName
			view.Phone = p.Phone
			view.Username = p.Username
		}
		if n, err := workbot.CountUserActiveApplications(ctx, s.rdb, app.UserID); err == nil {
			view.ActiveApplications = int(n)
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *AdminStore) CreateVacancy(ctx context.Context, v *workbot.Vacancy) error {
	if err := workbot.CreateVacancy(ctx, s.rdb, v); err != nil {
		return err
	}
	if v.ForemanID != "" {
		return s.rdb.SAdd(ctx, foremanVacanciesKey(v.ForemanID), v.ID).Err()
	}
	return nil
}

func (s *AdminStore) CloseVacancy(ctx context.Context, id string) error {
	return workbot.CloseVacancy(ctx, s.rdb, id)
}

func (s *AdminStore) AcceptApplication(ctx context.Context, vacancyID string, userID int64) ([]int64, error) {
	return workbot.AcceptApplication(ctx, s.rdb, vacancyID, userID)
}

func (s *AdminStore) RejectApplication(ctx context.Context, vacancyID string, userID int64) error {
	return workbot.RejectApplication(ctx, s.rdb, vacancyID, userID)
}

func (s *AdminStore) DeleteWorker(ctx context.Context, userID int64) error {
	return s.ws.DeleteWorker(ctx, userID)
}

func (s *AdminStore) GetWorkerProfile(ctx context.Context, userID int64) (*workbot.UserProfile, error) {
	return s.ws.GetProfile(ctx, userID)
}

func (s *AdminStore) GetNotifications(ctx context.Context, foremanID string) ([]workbot.AdminNotification, error) {
	return workbot.GetAdminNotifications(ctx, s.rdb, foremanID)
}

func (s *AdminStore) CountUnreadNotifications(ctx context.Context, foremanID string) int64 {
	return workbot.CountUnreadAdminNotifications(ctx, s.rdb, foremanID)
}

func (s *AdminStore) MarkNotificationsRead(ctx context.Context, foremanID string) error {
	return workbot.MarkAdminNotificationsRead(ctx, s.rdb, foremanID)
}

func (s *AdminStore) GetChatHistory(ctx context.Context, userID int64) ([]workbot.ChatMessage, error) {
	return workbot.GetChatHistory(ctx, s.rdb, userID)
}

func (s *AdminStore) SaveChatMessage(ctx context.Context, userID int64, msg workbot.ChatMessage) error {
	return workbot.SaveChatMessage(ctx, s.rdb, userID, msg)
}

func (s *AdminStore) ListWorkersByCity(ctx context.Context, cityNorm string) ([]*workbot.UserProfile, error) {
	ids, err := s.ws.GetCityMembers(ctx, cityNorm)
	if err != nil {
		return nil, err
	}
	profiles := make([]*workbot.UserProfile, 0, len(ids))
	for _, id := range ids {
		p, err := s.ws.GetProfile(ctx, id)
		if err == nil && p != nil {
			profiles = append(profiles, p)
		}
	}
	return profiles, nil
}
