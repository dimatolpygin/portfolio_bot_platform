package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

type APIClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

type sendMessageRequest struct {
	ChatID      int64       `json:"chat_id"`
	Text        string      `json:"text"`
	ParseMode   string      `json:"parse_mode,omitempty"`
	ReplyMarkup interface{} `json:"reply_markup,omitempty"`
}

type answerCallbackQueryRequest struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
}

type editMessageReplyMarkupRequest struct {
	ChatID      int64                `json:"chat_id"`
	MessageID   int64                `json:"message_id"`
	ReplyMarkup InlineKeyboardMarkup `json:"reply_markup"`
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

func NewClient(baseURL string, httpClient *http.Client, logger *slog.Logger) *APIClient {
	if logger == nil {
		logger = slog.Default()
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &APIClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		logger:     logger,
	}
}

func (c *APIClient) doPost(ctx context.Context, botToken, method string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s request: %w", method, err)
	}

	url := fmt.Sprintf("%s/bot%s/%s", c.baseURL, botToken, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send %s request: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("telegram api returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("decode telegram response: %w", err)
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram api error: %s", apiResp.Description)
	}

	return nil
}

func (c *APIClient) SendMessage(ctx context.Context, botToken string, chatID int64, text string) error {
	err := c.doPost(ctx, botToken, "sendMessage", sendMessageRequest{
		ChatID: chatID,
		Text:   text,
	})
	if err != nil {
		return err
	}
	c.logger.Debug("telegram message sent", "chat_id", chatID)
	return nil
}

func (c *APIClient) SendMessageWithReplyKeyboard(ctx context.Context, botToken string, chatID int64, text string, keyboard ReplyKeyboardMarkup) error {
	err := c.doPost(ctx, botToken, "sendMessage", sendMessageRequest{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: keyboard,
	})
	if err != nil {
		return err
	}
	c.logger.Debug("telegram message with reply keyboard sent", "chat_id", chatID)
	return nil
}

func (c *APIClient) SendMessageWithInlineKeyboard(ctx context.Context, botToken string, chatID int64, text, parseMode string, keyboard InlineKeyboardMarkup) error {
	err := c.doPost(ctx, botToken, "sendMessage", sendMessageRequest{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   parseMode,
		ReplyMarkup: keyboard,
	})
	if err != nil {
		return err
	}
	c.logger.Debug("telegram message with inline keyboard sent", "chat_id", chatID)
	return nil
}

func (c *APIClient) SendMessageRemoveKeyboard(ctx context.Context, botToken string, chatID int64, text string) error {
	err := c.doPost(ctx, botToken, "sendMessage", sendMessageRequest{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: ReplyKeyboardRemove{RemoveKeyboard: true},
	})
	if err != nil {
		return err
	}
	c.logger.Debug("telegram message with keyboard remove sent", "chat_id", chatID)
	return nil
}

func (c *APIClient) AnswerCallbackQuery(ctx context.Context, botToken, callbackQueryID, toastText string) error {
	return c.doPost(ctx, botToken, "answerCallbackQuery", answerCallbackQueryRequest{
		CallbackQueryID: callbackQueryID,
		Text:            toastText,
	})
}

func (c *APIClient) EditMessageReplyMarkup(ctx context.Context, botToken string, chatID, messageID int64, keyboard InlineKeyboardMarkup) error {
	return c.doPost(ctx, botToken, "editMessageReplyMarkup", editMessageReplyMarkupRequest{
		ChatID:      chatID,
		MessageID:   messageID,
		ReplyMarkup: keyboard,
	})
}

type editMessageTextRequest struct {
	ChatID      int64                `json:"chat_id"`
	MessageID   int64                `json:"message_id"`
	Text        string               `json:"text"`
	ParseMode   string               `json:"parse_mode,omitempty"`
	ReplyMarkup InlineKeyboardMarkup `json:"reply_markup"`
}

func (c *APIClient) EditMessageText(ctx context.Context, botToken string, chatID, messageID int64, text, parseMode string, keyboard InlineKeyboardMarkup) error {
	return c.doPost(ctx, botToken, "editMessageText", editMessageTextRequest{
		ChatID:      chatID,
		MessageID:   messageID,
		Text:        text,
		ParseMode:   parseMode,
		ReplyMarkup: keyboard,
	})
}
