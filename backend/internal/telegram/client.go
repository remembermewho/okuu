package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL — адрес Bot API. В тестах подменяется адресом фейкового сервера (httptest).
const DefaultBaseURL = "https://api.telegram.org"

// API — методы Bot API, которые нужны боту. Реализует *Client; в тестах — тот же Client
// на фейковом сервере.
type API interface {
	SendMessage(ctx context.Context, p SendMessageParams) (Message, error)
	EditMessageText(ctx context.Context, p EditMessageTextParams) error
	AnswerCallbackQuery(ctx context.Context, p AnswerCallbackQueryParams) error
	GetUpdates(ctx context.Context, p GetUpdatesParams) ([]Update, error)
	SetWebhook(ctx context.Context, p SetWebhookParams) error
}

// APIError — ответ Telegram с ok=false.
type APIError struct {
	Method      string
	Code        int // error_code: 400, 403, 429, ...
	Description string
	RetryAfter  time.Duration // parameters.retry_after для 429
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

// IsBlocked — Telegram ответил 403: пользователь заблокировал бота или удалил аккаунт.
func IsBlocked(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Code == http.StatusForbidden
}

// Client — HTTP-клиент Bot API. Токен есть только в пути запроса: в ошибки и логи он не попадает.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// ClientOptions — необязательные настройки клиента.
type ClientOptions struct {
	BaseURL    string       // по умолчанию DefaultBaseURL
	HTTPClient *http.Client // по умолчанию без общего таймаута: сроки задаёт ctx каждого запроса
}

// NewClient создаёт клиент с токеном бота.
func NewClient(token string, opt ClientOptions) *Client {
	c := &Client{base: strings.TrimRight(opt.BaseURL, "/"), token: token, http: opt.HTTPClient}
	if c.base == "" {
		c.base = DefaultBaseURL
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	return c
}

// String и GoString не дают напечатать токен вместе с клиентом.
func (c *Client) String() string   { return "telegram.Client(" + c.base + ")" }
func (c *Client) GoString() string { return c.String() }

// requestTimeout — срок обычного запроса; getUpdates ждёт дольше на время long polling.
const requestTimeout = 15 * time.Second

func (c *Client) SendMessage(ctx context.Context, p SendMessageParams) (Message, error) {
	var m Message
	err := c.call(ctx, "sendMessage", p, &m, requestTimeout)
	return m, err
}

// EditMessageText меняет текст сообщения бота. «message is not modified» (повторное нажатие
// той же кнопки) — не ошибка.
func (c *Client) EditMessageText(ctx context.Context, p EditMessageTextParams) error {
	err := c.call(ctx, "editMessageText", p, nil, requestTimeout)
	var e *APIError
	if errors.As(err, &e) && e.Code == http.StatusBadRequest && strings.Contains(e.Description, "message is not modified") {
		return nil
	}
	return err
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, p AnswerCallbackQueryParams) error {
	return c.call(ctx, "answerCallbackQuery", p, nil, requestTimeout)
}

func (c *Client) GetUpdates(ctx context.Context, p GetUpdatesParams) ([]Update, error) {
	var u []Update
	err := c.call(ctx, "getUpdates", p, &u, requestTimeout+time.Duration(p.Timeout)*time.Second)
	return u, err
}

func (c *Client) SetWebhook(ctx context.Context, p SetWebhookParams) error {
	return c.call(ctx, "setWebhook", p, nil, requestTimeout)
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// maxResponseBytes — предел ответа Telegram (getUpdates — до 100 апдейтов).
const maxResponseBytes = 8 << 20

func (c *Client) call(ctx context.Context, method string, params, result any, timeout time.Duration) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return c.sanitize(method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return c.sanitize(method, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return c.sanitize(method, err)
	}

	var r apiResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return &APIError{Method: method, Code: resp.StatusCode, Description: "некорректный ответ: " + http.StatusText(resp.StatusCode)}
	}
	if !r.OK {
		e := &APIError{Method: method, Code: r.ErrorCode, Description: r.Description}
		if e.Code == 0 {
			e.Code = resp.StatusCode
		}
		if r.Parameters != nil && r.Parameters.RetryAfter > 0 {
			e.RetryAfter = time.Duration(r.Parameters.RetryAfter) * time.Second
		}
		return e
	}
	if result != nil {
		if err := json.Unmarshal(r.Result, result); err != nil {
			return fmt.Errorf("telegram %s: разбор ответа: %w", method, err)
		}
	}
	return nil
}

// sanitize убирает из ошибки URL запроса (в нём токен). Отмена и таймаут ctx остаются
// распознаваемыми через errors.Is.
func (c *Client) sanitize(method string, err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		err = uerr.Err
	}
	if c.token != "" && strings.Contains(err.Error(), c.token) {
		err = errors.New(strings.ReplaceAll(err.Error(), c.token, "<token>"))
	}
	return fmt.Errorf("telegram %s: %w", method, err)
}
