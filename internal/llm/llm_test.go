package llm

import (
	"context"
	"encoding/json"
	"fmt"
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

/* ---------- GenerateStream ---------- */

const okSSE = "data: {\"choices\":[{\"delta\":{\"content\":\"Здравствуйте\"}}]}\n" +
	"\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\", мир.\"}}]}\n" +
	"\n" +
	"data: [DONE]\n\n"

func TestGenerateStreamCollectsDeltas(t *testing.T) {
	f := newFakeAPI(t, 200, okSSE)
	c := Client{BaseURL: f.url, APIKey: "sk-test", Model: "test-model", Timeout: 5 * time.Second}

	var got []string
	full, err := c.GenerateStream(context.Background(), "sys", "user", func(d string) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	if full != "Здравствуйте, мир." {
		t.Errorf("полный текст = %q", full)
	}
	if len(got) != 2 || got[0] != "Здравствуйте" || got[1] != ", мир." {
		t.Errorf("onDelta получил %q", got)
	}
	if !f.gotReq.Stream {
		t.Error("запрос должен идти со stream: true")
	}
	if f.gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q", f.gotAuth)
	}
	if len(f.gotReq.Messages) != 2 {
		t.Errorf("messages: %d записей, хочу 2", len(f.gotReq.Messages))
	}
}

func TestGenerateStreamSurfacesHTTPError(t *testing.T) {
	f := newFakeAPI(t, 429, `{"error":{"message":"rate limited"}}`)
	c := Client{BaseURL: f.url, Timeout: 5 * time.Second}

	_, err := c.GenerateStream(context.Background(), "sys", "user", nil)
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("ошибка должна содержать статус и тело: %v", err)
	}
}

func TestGenerateStreamSurfacesErrorInsideStream(t *testing.T) {
	// Часть провайдеров шлёт ошибку событием уже внутри 200-потока.
	body := "data: {\"error\":{\"message\":\"quota exceeded\"}}\n\n"
	f := newFakeAPI(t, 200, body)
	c := Client{BaseURL: f.url, Timeout: 5 * time.Second}

	_, err := c.GenerateStream(context.Background(), "sys", "user", nil)
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("хочу ошибку из потока: %v", err)
	}
}

func TestGenerateStreamRejectsEmptyStream(t *testing.T) {
	f := newFakeAPI(t, 200, ": keep-alive\n\n")
	c := Client{BaseURL: f.url, Timeout: 5 * time.Second}

	if _, err := c.GenerateStream(context.Background(), "sys", "user", nil); err == nil {
		t.Error("хочу ошибку, когда дельт не было вовсе")
	}
}

// SSE без пробела после "data:" — разрешено спекой, часть провайдеров
// так шлёт. Дельты должны собираться так же, как с пробелом.
func TestGenerateStream_DataNoSpace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := `data:{"choices":[{"delta":{"content":"%s"}}]}` + "\n\n"
		fmt.Fprint(w, fmt.Sprintf(chunk, "Привет")+fmt.Sprintf(chunk, ", мир")+"data:[DONE]\n\n")
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, Model: "m"}
	got, err := c.GenerateStream(context.Background(), "s", "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Привет, мир" {
		t.Fatalf("got %q", got)
	}
}
