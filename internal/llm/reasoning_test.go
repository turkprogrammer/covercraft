package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReasoningEffortNoneIsSent(t *testing.T) {
	// glm-5.3 и другие reasoning-модели без этого параметра думают минутами;
	// "none" (OpenAI-стиль) сокращает размышления в разы.
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &got)
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, Model: "m", ReasoningEffort: "none", Timeout: time.Second}
	if _, err := c.Generate(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if got["reasoning_effort"] != "none" {
		t.Errorf("reasoning_effort не отправлен: %v", got)
	}
}

func TestReasoningEffortEmptyOmitted(t *testing.T) {
	// Провайдеры без поддержки параметра не должны его получать.
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &got)
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, Model: "m", Timeout: time.Second}
	if _, err := c.Generate(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["reasoning_effort"]; ok {
		t.Errorf("reasoning_effort не должен попадать в JSON, если пуст: %v", got)
	}
}

func TestReasoningContentNullFallsBackToEmpty(t *testing.T) {
	// glm с маленьким max_tokens может вернуть content:null — это не ошибка
	// парсинга, просто пустой ответ.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":null},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	c := Client{BaseURL: srv.URL, Model: "m", Timeout: time.Second}
	letter, err := c.Generate(context.Background(), "s", "u")
	if err != nil {
		t.Fatalf("content:null не должен ломать Generate: %v", err)
	}
	if letter != "" {
		t.Errorf("letter = %q, хочу пусто при content:null", letter)
	}
}
