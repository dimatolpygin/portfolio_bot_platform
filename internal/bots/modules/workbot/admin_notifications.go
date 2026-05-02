package workbot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const notificationsKey = "workbot:admin:notifications:%s"
const notificationsLastReadKey = "workbot:admin:notifications:%s:last_read"

type AdminNotification struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // "cancel_apply"
	VacancyID string    `json:"vacancy_id"`
	UserID    int64     `json:"user_id"`
	FullName  string    `json:"full_name"`
	Username  string    `json:"username"`
	City      string    `json:"city"`
	Address   string    `json:"address"`
	StartTime string    `json:"start_time"`
	Description string  `json:"description"`
	CreatedAt time.Time `json:"created_at"`
}

func notifKey(foremanID string) string {
	if foremanID == "" {
		foremanID = "super_admin"
	}
	return fmt.Sprintf(notificationsKey, foremanID)
}

func notifLastReadKey(foremanID string) string {
	if foremanID == "" {
		foremanID = "super_admin"
	}
	return fmt.Sprintf(notificationsLastReadKey, foremanID)
}

func SaveAdminNotification(ctx context.Context, rdb *goredis.Client, foremanID string, n AdminNotification) error {
	n.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	n.CreatedAt = time.Now()
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	pipe := rdb.Pipeline()
	pipe.LPush(ctx, notifKey(foremanID), data)
	pipe.LTrim(ctx, notifKey(foremanID), 0, 99)
	_, err = pipe.Exec(ctx)
	return err
}

func GetAdminNotifications(ctx context.Context, rdb *goredis.Client, foremanID string) ([]AdminNotification, error) {
	vals, err := rdb.LRange(ctx, notifKey(foremanID), 0, 49).Result()
	if err != nil {
		return nil, err
	}
	result := make([]AdminNotification, 0, len(vals))
	for _, v := range vals {
		var n AdminNotification
		if json.Unmarshal([]byte(v), &n) == nil {
			result = append(result, n)
		}
	}
	return result, nil
}

func CountUnreadAdminNotifications(ctx context.Context, rdb *goredis.Client, foremanID string) int64 {
	lastReadStr, err := rdb.Get(ctx, notifLastReadKey(foremanID)).Result()
	if err != nil {
		// Нет метки — все уведомления непрочитаны
		n, _ := rdb.LLen(ctx, notifKey(foremanID)).Result()
		return n
	}
	var lastRead time.Time
	if err := lastRead.UnmarshalText([]byte(lastReadStr)); err != nil {
		return 0
	}
	vals, _ := rdb.LRange(ctx, notifKey(foremanID), 0, -1).Result()
	var count int64
	for _, v := range vals {
		var n AdminNotification
		if json.Unmarshal([]byte(v), &n) == nil && n.CreatedAt.After(lastRead) {
			count++
		}
	}
	return count
}

func MarkAdminNotificationsRead(ctx context.Context, rdb *goredis.Client, foremanID string) error {
	ts, _ := time.Now().MarshalText()
	return rdb.Set(ctx, notifLastReadKey(foremanID), ts, 0).Err()
}
