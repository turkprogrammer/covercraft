// Package llm — минимальный клиент OpenAI-совместимого API (/chat/completions).
// Один формат покрывает Ollama, OpenAI, OpenRouter и т.д.
package llm

import (
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

// Generate отправляет system+user промпт и возвращает текст ответа.
func (c Client) Generate(ctx context.Context, system, user string) (string, error) {
	req := chatRequest{
		Model: c.Model,
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ReasoningEffort: c.ReasoningEffort,
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	base := strings.TrimSuffix(c.BaseURL, "/")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Явный UA: некоторые WAF провайдеров режут дефолтный Go-http-client.
	httpReq.Header.Set("User-Agent", "covercraft/1.0")
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
		return "", fmt.Errorf("не удалось связаться с %s: %w", base, err)
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

type chatRequest struct {
	Model           string    `json:"model"`
	Messages        []message `json:"messages"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
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
