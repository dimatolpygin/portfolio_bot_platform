package workbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	StateNew        = ""
	StateAwaitName  = "await_name"
	StateAwaitDOB   = "await_dob"
	StateAwaitCit   = "await_citizen"
	StateAwaitPhone = "await_phone"
	StateAwaitLoc   = "await_loc"
	StateAwaitConfirm = "await_confirm"
	StateAwaitEdit      = "await_edit"
	StateRegistered     = "registered"
	StateAwaitCityManual = "await_city_manual"
	StateSupport        = "support"
)

type UserProfile struct {
	TelegramID    int64     `json:"telegram_id"`
	FirstName     string    `json:"first_name"`
	Username      string    `json:"username,omitempty"`
	FullName      string    `json:"full_name"`
	DOB           string    `json:"dob"`
	Citizenship   string    `json:"citizenship"`
	Phone         string    `json:"phone"`
	City          string    `json:"city"`
	CityNorm      string    `json:"city_norm"`
	Lat           float64   `json:"lat"`
	Lon           float64   `json:"lon"`
	State         string    `json:"state"`
	EditingField  string    `json:"editing_field,omitempty"`
	RegisteredAt  time.Time `json:"registered_at,omitempty"`
	CityChangedAt time.Time `json:"city_changed_at,omitempty"`
}

type RedisStore struct {
	rdb *goredis.Client
}

func NewRedisStore(rdb *goredis.Client) *RedisStore {
	return &RedisStore{rdb: rdb}
}

func profileKey(userID int64) string {
	return fmt.Sprintf("workbot:user:%d:profile", userID)
}

func cityKey(cityNorm string) string {
	return fmt.Sprintf("workbot:city:%s:workers", cityNorm)
}

func NormCity(city string) string {
	return strings.ToLower(strings.ReplaceAll(city, " ", "_"))
}

func (s *RedisStore) GetProfile(ctx context.Context, userID int64) (*UserProfile, error) {
	data, err := s.rdb.Get(ctx, profileKey(userID)).Bytes()
	if err == goredis.Nil {
		return &UserProfile{TelegramID: userID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	var p UserProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	return &p, nil
}

func (s *RedisStore) SaveProfile(ctx context.Context, p *UserProfile) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode profile: %w", err)
	}
	return s.rdb.Set(ctx, profileKey(p.TelegramID), data, 0).Err()
}

func (s *RedisStore) GetState(ctx context.Context, userID int64) (string, error) {
	p, err := s.GetProfile(ctx, userID)
	if err != nil {
		return "", err
	}
	return p.State, nil
}

func (s *RedisStore) RegisterUser(ctx context.Context, p *UserProfile) error {
	if err := s.SaveProfile(ctx, p); err != nil {
		return err
	}
	pipe := s.rdb.Pipeline()
	pipe.SAdd(ctx, "workbot:workers:all", p.TelegramID)
	pipe.SAdd(ctx, cityKey(p.CityNorm), p.TelegramID)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) GetCityMembers(ctx context.Context, cityNorm string) ([]int64, error) {
	vals, err := s.rdb.SMembers(ctx, cityKey(cityNorm)).Result()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(vals))
	for _, v := range vals {
		var id int64
		if _, err := fmt.Sscanf(v, "%d", &id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *RedisStore) CountWorkers(ctx context.Context) (int64, error) {
	return s.rdb.SCard(ctx, "workbot:workers:all").Result()
}

func (s *RedisStore) DeleteWorker(ctx context.Context, userID int64) error {
	p, err := s.GetProfile(ctx, userID)
	if err != nil {
		return err
	}
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, profileKey(userID))
	pipe.SRem(ctx, "workbot:workers:all", userID)
	if p.CityNorm != "" {
		pipe.SRem(ctx, cityKey(p.CityNorm), userID)
	}
	pipe.Del(ctx, fmt.Sprintf("workbot:user:%d:applications", userID))
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) IsApplied(ctx context.Context, vacancyID string, userID int64) bool {
	exists, _ := s.rdb.HExists(ctx, applicantsKey(vacancyID), fmt.Sprintf("%d", userID)).Result()
	return exists
}

func (s *RedisStore) CountTodayApplications(ctx context.Context) (int64, error) {
	today := time.Now().Format("2006-01-02")
	activeIDs, err := s.rdb.SMembers(ctx, "workbot:vacancies:active").Result()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, vid := range activeIDs {
		vals, err := s.rdb.HGetAll(ctx, fmt.Sprintf("workbot:vacancy:%s:applicants", vid)).Result()
		if err != nil {
			continue
		}
		for _, v := range vals {
			var app Application
			if json.Unmarshal([]byte(v), &app) == nil && strings.HasPrefix(app.AppliedAt.Format("2006-01-02"), today) {
				total++
			}
		}
	}
	return total, nil
}

func (s *RedisStore) ListAllProfiles(ctx context.Context) ([]*UserProfile, error) {
	ids, err := s.rdb.SMembers(ctx, "workbot:workers:all").Result()
	if err != nil {
		return nil, err
	}
	profiles := make([]*UserProfile, 0, len(ids))
	for _, idStr := range ids {
		var id int64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
			continue
		}
		p, err := s.GetProfile(ctx, id)
		if err != nil {
			continue
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}
