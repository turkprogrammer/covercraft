package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/turkprogrammer/covercraft/frontend"
	"github.com/turkprogrammer/covercraft/internal/settings"
)

func TestServeIndex(t *testing.T) {
	h := New(Config{ContextDir: t.TempDir()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, хочу 200", rec.Code)
	}
	if rec.Body.String() != frontend.IndexHTML {
		t.Error("сервер должен отдавать index.html байт-в-байт (embed)")
	}
}

func TestSettingsGetPost(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	h := New(Config{ContextDir: t.TempDir()})

	// До сохранения — дефолты + дефолтный промпт в отдельном поле.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/settings: %d", rec.Code)
	}
	var s struct {
		settings.Settings
		DefaultSystemPrompt string `json:"defaultSystemPrompt"`
	}
	json.Unmarshal(rec.Body.Bytes(), &s)
	if s.BaseURL == "" {
		t.Error("GET до сохранения должен вернуть дефолты")
	}
	if s.DefaultSystemPrompt == "" {
		t.Error("GET должен отдавать defaultSystemPrompt — UI показывает его в поле промпта")
	}

	// POST сохраняет (промпт в Settings не входит).
	want := settings.Settings{
		BaseURL: "http://x/v1", APIKey: "k", Model: "m", ReasoningEffort: "none",
	}
	body, _ := json.Marshal(want)
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST /api/settings: %d", rec.Code)
	}
	got, err := settings.Load()
	if err != nil || got != want {
		t.Errorf("после POST настройки на диске: %+v, err %v", got, err)
	}

	// Кастомный промпт из POST не должен попадать на диск: GET после — дефолт.
	bad := `{"baseUrl":"http://x/v1","apiKey":"k","model":"m","systemPrompt":"кастом","reasoningEffort":""}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(bad))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST с systemPrompt: %d", rec.Code)
	}
	var after struct {
		DefaultSystemPrompt string `json:"defaultSystemPrompt"`
	}
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	json.Unmarshal(rec2.Body.Bytes(), &after)
	if after.DefaultSystemPrompt != s.DefaultSystemPrompt {
		t.Errorf("дефолтный промпт должен восстанавливаться: got %.30q…", after.DefaultSystemPrompt)
	}
}

func TestSettingsRejectsBadJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := New(Config{ContextDir: t.TempDir()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader("{битый"))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код = %d, хочу 400", rec.Code)
	}
}

// fakeLLM — реальный HTTP-сервер, но отвечает всегда одинаково.
func fakeLLM(t *testing.T, status int, resp string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // чтобы elapsedMs был замерен, а не 0
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestGenerateEndpointHappyPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctxDir := t.TempDir()
	os.WriteFile(filepath.Join(ctxDir, "01-profile.md"), []byte("Профиль: Go-разработчик."), 0o600)
	h := New(Config{
		ContextDir: ctxDir,
		LLM: func(ctx context.Context, system, user string) (string, error) {
			if !strings.Contains(user, "Профиль: Go-разработчик.") {
				t.Errorf("в промпт не попал контекст: %q", user)
			}
			if !strings.Contains(user, "Senior Go") {
				t.Errorf("в промпт не попала вакансия: %q", user)
			}
			time.Sleep(10 * time.Millisecond) // чтобы elapsedMs был замерен, а не 0
			return "Письмо готово.", nil
		},
	})

	body, _ := json.Marshal(map[string]string{"vacancy": "Нужен Senior Go."})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("код = %d, тело: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Letter    string `json:"letter"`
		ElapsedMs int64  `json:"elapsedMs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	if out.Letter != "Письмо готово." {
		t.Errorf("letter = %q", out.Letter)
	}
	if out.ElapsedMs <= 0 {
		t.Errorf("elapsedMs = %d, хочу > 0 — UI показывает время ответа модели", out.ElapsedMs)
	}
}

// TestGenerateEndpointAuditWarnings — письмо с фреймворками в стеке при
// PHP-вакансии должно вернуться с warnings (постпроверка internal/audit).
func TestGenerateEndpointAuditWarnings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := New(Config{ContextDir: t.TempDir(), LLM: func(ctx context.Context, system, user string) (string, error) {
		return "Стек: Go, PHP, ML, Laravel, Symfony.", nil
	}})
	body, _ := json.Marshal(map[string]string{"vacancy": "Ищем PHP-разработчика (Senior). Стек: Laravel; PostgreSQL."})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("код = %d, тело: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Letter   string   `json:"letter"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Warnings) == 0 {
		t.Error("фреймворки в стеке письма не помечены предупреждением")
	}
}

// TestGenerateEndpointAuditFix — режим автоправки: сервер отдаёт письмо
// с замечаниями обратно модели (user-промпт содержит и то, и другое) и
// возвращает исправленный результат с повторной проверкой.
func TestGenerateEndpointAuditFix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var gotUser string
	h := New(Config{ContextDir: t.TempDir(), LLM: func(ctx context.Context, system, user string) (string, error) {
		gotUser = user
		return "Стек: Go, PHP, ML, PostgreSQL.\nSymfony 7.2 (E-commerce-Lite), Yii2 production, Lumen; PHPUnit + TDD.", nil // исправленное письмо
	}})
	body, _ := json.Marshal(map[string]any{
		"vacancy":  "Ищем PHP-разработчика (Senior). Laravel.",
		"auditFix": true,
		"letter":   "Стек: Go, PHP, ML, Laravel.",
		"warnings": []string{"Фреймворк «laravel» в строке стека"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("код = %d, тело: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotUser, "Замечания:") || !strings.Contains(gotUser, "laravel") {
		t.Errorf("в промпт автоправки не попали замечания: %q", gotUser)
	}
	if !strings.Contains(gotUser, "Стек: Go, PHP, ML, Laravel.") {
		t.Errorf("в промпт автоправки не попало письмо: %q", gotUser)
	}
	var out struct {
		Letter   string   `json:"letter"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Warnings) != 0 {
		t.Errorf("исправленное письмо не должно иметь замечаний: %v", out.Warnings)
	}
}

func TestGenerateEndpointAuditFixRequiresFields(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := New(Config{ContextDir: t.TempDir(), LLM: failIfCalled(t)})
	body, _ := json.Marshal(map[string]any{"vacancy": "PHP", "auditFix": true})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("код = %d, хочу 400 (нет letter/warnings)", rec.Code)
	}
}

func TestGenerateEndpointRequiresVacancy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := New(Config{ContextDir: t.TempDir(), LLM: failIfCalled(t)})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(`{"vacancy":""}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("пустая вакансия: код = %d, хочу 400", rec.Code)
	}
}

func TestGenerateEndpointSurfacesLLMError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := New(Config{
		ContextDir: t.TempDir(),
		LLM: func(ctx context.Context, system, user string) (string, error) {
			return "", errTest
		},
	})
	body, _ := json.Marshal(map[string]string{"vacancy": "V"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("ошибка LLM: код = %d, хочу 502, тело: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Error string `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.Contains(out.Error, "модель недоступна") {
		t.Errorf("текст ошибки должен доходить до UI: %q", out.Error)
	}
}

// TestGenerateEndpointUsesCustomPrompt — кастомный промпт из запроса
// доходит до LLM; без него — дефолтный из settings.
func TestGenerateEndpointUsesCustomPrompt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var gotSystem string
	h := New(Config{
		ContextDir: t.TempDir(),
		LLM: func(ctx context.Context, system, user string) (string, error) {
			gotSystem = system
			return "ok", nil
		},
	})

	// 1) Кастомный промпт доходит до LLM.
	body, _ := json.Marshal(map[string]string{
		"vacancy": "V", "systemPrompt": "кастом-промпт",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || gotSystem != "кастом-промпт" {
		t.Fatalf("кастом: код=%d system=%q", rec.Code, gotSystem)
	}

	// 2) Без кастомного — дефолтный.
	body, _ = json.Marshal(map[string]string{"vacancy": "V"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || gotSystem != settings.DefaultSystemPrompt {
		t.Fatalf("дефолт: код=%d system=%.30q…, хочу DefaultSystemPrompt", rec.Code, gotSystem)
	}
}

func TestGenerateUsesConfiguredClient(t *testing.T) {
	// Настройки, сохранённые через /api/settings, должны попадать в LLM-вызов.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	base := fakeLLM(t, 200, `{"choices":[{"message":{"content":"ok"}}]}`)
	if err := settings.Save(settings.Settings{
		BaseURL: base, APIKey: "k", Model: "m", ReasoningEffort: "none",
	}); err != nil {
		t.Fatal(err)
	}

	h := New(Config{ContextDir: t.TempDir()})
	body, _ := json.Marshal(map[string]string{"vacancy": "V"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("код = %d, тело: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Letter    string `json:"letter"`
		ElapsedMs int64  `json:"elapsedMs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Letter != "ok" {
		t.Errorf("letter = %q, хочу %q", out.Letter, "ok")
	}
	if out.ElapsedMs <= 0 {
		t.Errorf("elapsedMs = %d, хочу > 0", out.ElapsedMs)
	}
}

// TestGenerateEndpointTimesOut — генерация обрывается по таймауту из
// настроек с понятной ошибкой, а не висит минутами.
func TestGenerateEndpointTimesOut(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := settings.Save(settings.Settings{TimeoutSec: 1}); err != nil {
		t.Fatal(err)
	}
	h := New(Config{
		ContextDir: t.TempDir(),
		LLM: func(ctx context.Context, system, user string) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(3 * time.Second):
				return "не должен успеть", nil
			}
		},
	})

	body, _ := json.Marshal(map[string]string{"vacancy": "V"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("код = %d, хочу 504, тело: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Error string `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.Contains(out.Error, "таймаут") {
		t.Errorf("ошибка должна говорить про таймаут: %q", out.Error)
	}
}

var errTest = errLLM{}

type errLLM struct{}

func (errLLM) Error() string { return "модель недоступна" }

func failIfCalled(t *testing.T) func(ctx context.Context, system, user string) (string, error) {
	return func(ctx context.Context, system, user string) (string, error) {
		t.Error("LLM не должен вызываться без вакансии")
		return "", nil
	}
}
