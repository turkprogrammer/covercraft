package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAPI поднимает OpenAI-совместимый /chat/completions и записывает,
// что в него пришло, чтобы тест мог проверить и запрос, и ответ.
type fakeAPI struct {
	status int
	body   string
	url    string

	gotMethod string
	gotPath   string
	gotAuth   string
	gotReq    chatRequest
}

func newFakeAPI(t *testing.T, status int, body string) *fakeAPI {
	t.Helper()
	f := &fakeAPI{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.gotMethod = r.Method
		f.gotPath = r.URL.Path
		f.gotAuth = r.Header.Get("Authorization")
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.gotReq = req
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

const okBody = `{"choices":[{"message":{"content":"Здравствуйте! Заинтересованная кандидат."}}]}`

func TestGenerateSendsChatCompletionAndReturnsText(t *testing.T) {
	f := newFakeAPI(t, 200, okBody)
	c := Client{BaseURL: f.url, APIKey: "sk-test", Model: "test-model", Timeout: 5 * time.Second}

	got, err := c.Generate(context.Background(),
		"Ты эксперт по Go.", "Вакансия: Go-разработчик.")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "Здравствуйте! Заинтересованная кандидат." {
		t.Errorf("текст ответа = %q", got)
	}

	if f.gotMethod != http.MethodPost {
		t.Errorf("метод = %s, хочу POST", f.gotMethod)
	}
	if f.gotPath != "/chat/completions" {
		t.Errorf("путь = %s, хочу /chat/completions", f.gotPath)
	}
	if f.gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, хочу Bearer sk-test", f.gotAuth)
	}
	if f.gotReq.Model != "test-model" {
		t.Errorf("model = %q", f.gotReq.Model)
	}
	if len(f.gotReq.Messages) != 2 {
		t.Fatalf("messages: %d записей, хочу 2 (system+user)", len(f.gotReq.Messages))
	}
	if f.gotReq.Messages[0].Role != "system" || f.gotReq.Messages[0].Content != "Ты эксперт по Go." {
		t.Errorf("system-сообщение: %+v", f.gotReq.Messages[0])
	}
	if f.gotReq.Messages[1].Role != "user" || f.gotReq.Messages[1].Content != "Вакансия: Go-разработчик." {
		t.Errorf("user-сообщение: %+v", f.gotReq.Messages[1])
	}
}

func TestGenerateWithoutAPIKeyOmitsAuthHeader(t *testing.T) {
	// Ollama работает без ключа; заголовок Authorization не нужен.
	f := newFakeAPI(t, 200, okBody)
	c := Client{BaseURL: f.url, Timeout: 5 * time.Second}

	if _, err := c.Generate(context.Background(), "sys", "user"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if f.gotAuth != "" {
		t.Errorf("Authorization = %q, хочу пусто (без ключа)", f.gotAuth)
	}
}

func TestGenerateSurfacesHTTPErrorWithBody(t *testing.T) {
	f := newFakeAPI(t, 500, `{"error":{"message":"model not found"}}`)
	c := Client{BaseURL: f.url, APIKey: "k", Timeout: 5 * time.Second}

	_, err := c.Generate(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("хочу ошибку на HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("ошибка должна содержать статус и тело: %v", err)
	}
}

func TestGenerateSurfacesInvalidJSON(t *testing.T) {
	f := newFakeAPI(t, 200, `<html>не json</html>`)
	c := Client{BaseURL: f.url, Timeout: 5 * time.Second}

	if _, err := c.Generate(context.Background(), "sys", "user"); err == nil {
		t.Error("хочу ошибку на не-JSON ответе")
	}
}

func TestGenerateSurfacesEmptyChoices(t *testing.T) {
	f := newFakeAPI(t, 200, `{"choices":[]}`)
	c := Client{BaseURL: f.url, Timeout: 5 * time.Second}

	if _, err := c.Generate(context.Background(), "sys", "user"); err == nil {
		t.Error("хочу ошибку на пустых choices")
	}
}
