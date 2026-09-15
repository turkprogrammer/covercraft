// Package llm — минимальный клиент OpenAI-совместимого API (/chat/completions).
// Один формат покрывает Ollama, OpenAI, OpenRouter и т.д.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client вызывает OpenAI-совместимый endpoint. Поле APIKey пустое для
// локальных провайдеров без авторизации (Ollama). ReasoningEffort пустой
// («»), чтобы не ломать провайдеры без поддержки; «none» отключает
// многоминутные размышления reasoning-моделей (glm-5.3 и др.).
type Client struct {
	BaseURL         string // например http://127.0.0.1:11434/v1 — без хвостового /
	APIKey          string
	Model           string
	ReasoningEffort string // "", "none", "low", "medium", "high"
	Timeout         time.Duration
}

// do отправляет chat-запрос на /chat/completions и возвращает ответ.
// Общий для Generate и GenerateStream: заголовки, UA, авторизация,
// таймаут. accept — значение заголовка Accept ("" — не отправлять).
func (c Client) do(ctx context.Context, req chatRequest, accept string) (*http.Response, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	base := strings.TrimSuffix(c.BaseURL, "/")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Явный UA: некоторые WAF провайдеров режут дефолтный Go-http-client.
	httpReq.Header.Set("User-Agent", "covercraft/1.0")
	if accept != "" {
		httpReq.Header.Set("Accept", accept)
	}
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	timeout := c.Timeout
	if timeout == 0 {
		timeout = 300 * time.Second // reasoning-модели думают минутами
	}
	hc := &http.Client{Timeout: timeout}

	resp, err := hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("не удалось связаться с %s: %w", base, err)
	}
	return resp, nil
}

// prompt — стандартная пара сообщений для /chat/completions.
func prompt(system, user string) []message {
	return []message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
}

// Generate отправляет system+user промпт и возвращает текст ответа.
func (c Client) Generate(ctx context.Context, system, user string) (string, error) {
	req := chatRequest{
		Model:           c.Model,
		Messages:        prompt(system, user),
		ReasoningEffort: c.ReasoningEffort,
	}
	resp, err := c.do(ctx, req, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("API вернул %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out chatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("API вернул не-JSON (код %d): %w", resp.StatusCode, err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("API вернул ответ без choices: %s", strings.TrimSpace(string(body)))
	}
	return out.Choices[0].Message.Content, nil
}

// GenerateStream отправляет system+user промпт с "stream": true и отдаёт
// текст по мере генерации через onDelta (SSE-протокол провайдера). Возвращает
// накопленный полный текст; при обрыве — то, что успело прийти, и ошибку.
func (c Client) GenerateStream(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
	req := chatRequest{
		Model:           c.Model,
		Messages:        prompt(system, user),
		ReasoningEffort: c.ReasoningEffort,
		Stream:          true,
	}
	resp, err := c.do(ctx, req, "text/event-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// Ошибка провайдера приходит до первой дельты — можно отдать обычную
	// ошибку с телом; UI ничего не потеряет.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		return "", fmt.Errorf("API вернул %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var sb strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20) // строки SSE могут быть длинными
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // комментарии, keep-alive, пустые строки
		}
		// Спека SSE разрешает и "data: x", и "data:x" — часть провайдеров
		// шлёт второй вариант.
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue // пустое событие ("data:") — валидно по спеке SSE
		}
		if payload == "[DONE]" {
			break
		}
		var ev deltaEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return sb.String(), fmt.Errorf("битый SSE-чанк: %w", err)
		}
		if ev.Error.Message != "" {
			// Часть провайдеров шлёт ошибку внутри потока.
			return sb.String(), fmt.Errorf("API вернул ошибку в потоке: %s", ev.Error.Message)
		}
		if len(ev.Choices) > 0 && ev.Choices[0].Delta.Content != "" {
			sb.WriteString(ev.Choices[0].Delta.Content)
			if onDelta != nil {
				onDelta(ev.Choices[0].Delta.Content)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return sb.String(), fmt.Errorf("обрыв потока: %w", err)
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("API не прислал ни одной дельты (код %d)", resp.StatusCode)
	}
	return sb.String(), nil
}

// deltaEvent — чанк SSE-потока /chat/completions при stream: true.
type deltaEvent struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

type chatRequest struct {
	Model           string    `json:"model"`
	Messages        []message `json:"messages"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	Stream          bool      `json:"stream,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}
