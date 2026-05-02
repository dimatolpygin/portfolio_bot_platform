package workbot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type ChatMessage struct {
	FromAdmin bool      `json:"from_admin"`
	Text      string    `json:"text"`
	SentAt    time.Time `json:"sent_at"`
	AdminName string    `json:"admin_name,omitempty"`
}

func chatKey(userID int64) string {
	return fmt.Sprintf("workbot:chat:%d", userID)
}

func SaveChatMessage(ctx context.Context, rdb *goredis.Client, userID int64, msg ChatMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	pipe := rdb.Pipeline()
	pipe.LPush(ctx, chatKey(userID), data)
	pipe.LTrim(ctx, chatKey(userID), 0, 99)
	_, err = pipe.Exec(ctx)
	return err
}

func GetChatHistory(ctx context.Context, rdb *goredis.Client, userID int64) ([]ChatMessage, error) {
	vals, err := rdb.LRange(ctx, chatKey(userID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	msgs := make([]ChatMessage, 0, len(vals))
	for _, v := range vals {
		var m ChatMessage
		if json.Unmarshal([]byte(v), &m) == nil {
			msgs = append(msgs, m)
		}
	}
	// Reverse to chronological order (oldest first)
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}
