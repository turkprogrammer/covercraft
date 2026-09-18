package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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

// fakeLLM — реальный HTTP-сервер, отвечающий SSE-потоком /chat/completions.
func fakeLLM(t *testing.T, status int, sse string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // чтобы elapsedMs был замерен, а не 0
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// sseEvents — разобранный ответ /api/generate в тестах.
type sseEvents struct {
	Deltas []string
	Done   *sseDone
	Err    string
}

// parseSSE разбирает события "data: {...}\n\n" из тела ответа сервера.
func parseSSE(t *testing.T, body string) sseEvents {
	t.Helper()
	var ev sseEvents
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var probe struct {
			Delta string `json:"delta"`
			Done  bool   `json:"done"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &probe); err != nil {
			t.Fatalf("битое SSE-событие %q: %v", payload, err)
		}
		switch {
		case probe.Error != "":
			ev.Err = probe.Error
		case probe.Done:
			ev.Done = &sseDone{}
			if err := json.Unmarshal([]byte(payload), ev.Done); err != nil {
				t.Fatalf("битое done-событие: %v", err)
			}
		default:
			ev.Deltas = append(ev.Deltas, probe.Delta)
		}
	}
	return ev
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
			if !strings.Contains(user, "### Обязательный чек-лист") || !strings.Contains(user, "- Go\n") {
				t.Errorf("в промпт не попал чек-лист must-have из разбора вакансии: %q", user)
			}
			time.Sleep(10 * time.Millisecond) // чтобы elapsedMs был замерен, а не 0
			return "Письмо готово.", nil
		},
		// FitLLM — отдельный шов: иначе LLM-швы конкурентно пишут в
		// t-захваты (гонка под -race).
		FitLLM: func(ctx context.Context, system, user string) (string, error) {
			return `{"role":"go-primary","mustHave":[{"text":"Go","kind":"must","category":"stack"}]}`, nil
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
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, хочу text/event-stream", ct)
	}
	ev := parseSSE(t, rec.Body.String())
	if ev.Err != "" {
		t.Fatalf("неожиданная ошибка в стриме: %q", ev.Err)
	}
	if ev.Done == nil {
		t.Fatal("нет done-события")
	}
	if len(ev.Deltas) == 0 || strings.Join(ev.Deltas, "") != ev.Done.Letter {
		t.Errorf("дельты %q должны склеиваться в letter %q", ev.Deltas, ev.Done.Letter)
	}
	if ev.Done.Letter != "Письмо готово." {
		t.Errorf("letter = %q", ev.Done.Letter)
	}
	if ev.Done.ElapsedMs <= 0 {
		t.Errorf("elapsedMs = %d, хочу > 0 — UI показывает время ответа модели", ev.Done.ElapsedMs)
	}
	// Вердикт фита: должен прийти в done вместе с warnings.
	if ev.Done.Fit == nil {
		t.Fatal("нет fit-вердикта в done-событии — извлечение и матчинг не сработали")
	}
	if ev.Done.Fit.Verdict != "apply" && ev.Done.Fit.Verdict != "apply_with_caveats" && ev.Done.Fit.Verdict != "skip" {
		t.Errorf("fit.verdict = %q, хочу одно из трёх состояний", ev.Done.Fit.Verdict)
	}
	if ev.Done.Fit.Score < 0 || ev.Done.Fit.Score > 100 {
		t.Errorf("fit.score = %d, хочу 0..100", ev.Done.Fit.Score)
	}
}

// TestGenerateEndpointExtractCalledOnce — разбор вакансии выполняется
// ровно один LLM-вызов: чек-лист письма и fit-вердикт переиспользуют
// один и тот же результат (второго вызова извлечения быть не должно).
func TestGenerateEndpointExtractCalledOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	extracts := 0
	h := New(Config{
		ContextDir: t.TempDir(),
		LLM: func(ctx context.Context, system, user string) (string, error) {
			return "Письмо.", nil
		},
		FitLLM: func(ctx context.Context, system, user string) (string, error) {
			extracts++
			return `{"role":"go-primary","mustHave":[{"text":"Go","kind":"must","category":"stack"}]}`, nil
		},
	})
	body, _ := json.Marshal(map[string]string{"vacancy": "Нужен Senior Go."})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	ev := parseSSE(t, rec.Body.String())
	if ev.Done == nil {
		t.Fatalf("нет done-события: err=%q", ev.Err)
	}
	if extracts != 1 {
		t.Errorf("извлечение вакансии = %d вызовов, хочу ровно 1 (переиспользование)", extracts)
	}
	if ev.Done.Fit == nil {
		t.Error("вердикта нет — извлечённые требования не дошли до фита")
	}
}

// TestGenerateEndpointFitSurvivesBadExtraction — битый JSON разбора
// вакансии не должен ронять письмо: done приходит без fit-панели.
func TestGenerateEndpointFitSurvivesBadExtraction(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := New(Config{
		ContextDir: t.TempDir(),
		LLM: func(ctx context.Context, system, user string) (string, error) {
			return "Письмо готово.", nil
		},
		FitLLM: func(ctx context.Context, system, user string) (string, error) {
			return "модель ответила прозой без JSON", nil
		},
	})
	body, _ := json.Marshal(map[string]string{"vacancy": "Нужен Senior Go."})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	ev := parseSSE(t, rec.Body.String())
	if ev.Err != "" {
		t.Fatalf("ошибка разбора вакансии не должна попадать в стрим: %q", ev.Err)
	}
	if ev.Done == nil || ev.Done.Letter != "Письмо готово." {
		t.Fatalf("письмо потеряно из-за битого разбора: %+v", ev.Done)
	}
	if ev.Done.Fit != nil {
		t.Errorf("fit должен отсутствовать при битом разборе, got %+v", ev.Done.Fit)
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
	ev := parseSSE(t, rec.Body.String())
	if ev.Done == nil {
		t.Fatal("нет done-события")
	}
	if len(ev.Done.Warnings) == 0 {
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
	}, FitLLM: func(ctx context.Context, system, user string) (string, error) {
		return "{}", nil // пустой разбор: fit не влияет на проверки автоправки
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
	ev := parseSSE(t, rec.Body.String())
	if ev.Done == nil {
		t.Fatal("нет done-события")
	}
	if len(ev.Done.Warnings) != 0 {
		t.Errorf("исправленное письмо не должно иметь замечаний: %v", ev.Done.Warnings)
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

	if rec.Code != http.StatusOK {
		t.Fatalf("ошибка LLM после старта SSE: код = %d, тело: %s", rec.Code, rec.Body.String())
	}
	ev := parseSSE(t, rec.Body.String())
	if !strings.Contains(ev.Err, "модель недоступна") {
		t.Errorf("ошибка должна прийти событием и дойти до UI: %q", ev.Err)
	}
	if ev.Done != nil {
		t.Error("done-события при ошибке быть не должно")
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
		FitLLM: func(ctx context.Context, system, user string) (string, error) {
			return "{}", nil // отдельный шов, чтобы не гонять t-захваты параллельно
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
	// Настройки, сохранённые через /api/settings, должны попадать в LLM-вызов;
	// запрос — со stream: true (прод идёт по стриминговому пути).
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	base := fakeLLM(t, 200, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
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
	ev := parseSSE(t, rec.Body.String())
	if ev.Err != "" || ev.Done == nil || ev.Done.Letter != "ok" {
		t.Fatalf("ответ стрима: err=%q done=%+v", ev.Err, ev.Done)
	}
	if ev.Done.ElapsedMs <= 0 {
		t.Errorf("elapsedMs = %d, хочу > 0", ev.Done.ElapsedMs)
	}
}

// TestGenerateEndpointStreamsDeltas — заданный LLMStream шов отдаёт дельты
// по мере генерации: сервер транслирует их в SSE-события по одной.
func TestGenerateEndpointStreamsDeltas(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := New(Config{
		ContextDir: t.TempDir(),
		LLMStream: func(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
			for _, d := range []string{"Здрав", "ствуйте", ", мир!"} {
				time.Sleep(5 * time.Millisecond) // чтобы elapsedMs был не 0
				onDelta(d)
			}
			return "Здравствуйте, мир!", nil
		},
	})
	body, _ := json.Marshal(map[string]string{"vacancy": "V"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	ev := parseSSE(t, rec.Body.String())
	if want := []string{"Здрав", "ствуйте", ", мир!"}; !reflect.DeepEqual(ev.Deltas, want) {
		t.Errorf("дельты = %q, хочу %q", ev.Deltas, want)
	}
	if ev.Done == nil || ev.Done.Letter != "Здравствуйте, мир!" {
		t.Fatalf("done-событие: %+v", ev.Done)
	}
	if ev.Done.ElapsedMs <= 0 {
		t.Errorf("elapsedMs = %d, хочу > 0", ev.Done.ElapsedMs)
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

	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, тело: %s", rec.Code, rec.Body.String())
	}
	ev := parseSSE(t, rec.Body.String())
	if !strings.Contains(ev.Err, "таймаут") {
		t.Errorf("ошибка должна говорить про таймаут: %q", ev.Err)
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

// Гибридный движок фита (fitEngine="llm"): модель размечает покрытие после
// генерации письма, код проверяет цитату и считает вердикт. Разметка
// отличима от извлечения по системному промпту («аудитор»).
func TestGenerateHybridFitEngine(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctxDir := t.TempDir()
	os.WriteFile(filepath.Join(ctxDir, "01-profile.md"), []byte("Профиль: Go-разработчик."), 0o600)
	mapCalled := false
	h := New(Config{
		ContextDir: ctxDir,
		FitEngine:  "llm",
		LLM: func(ctx context.Context, system, user string) (string, error) {
			return "Письмо готово.", nil
		},
		FitLLM: func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(system, "сопроводительного письма") {
				mapCalled = true
				if !strings.Contains(user, "Письмо готово.") {
					t.Errorf("в промпт разметки не попало письмо: %q", user)
				}
				if !strings.Contains(user, "Платежи") {
					t.Errorf("в промпт разметки не попал список требований: %q", user)
				}
				return `{"items":[{"text":"Платежи","source":"letter","quote":"Письмо готово.","note":"есть"}]}`, nil
			}
			return `{"role":"go-primary","mustHave":[{"text":"Платежи","kind":"must","category":"domain"}]}`, nil
		},
	})

	body, _ := json.Marshal(map[string]string{"vacancy": "Нужны платежи."})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	ev := parseSSE(t, rec.Body.String())
	if ev.Err != "" {
		t.Fatalf("ошибка в стриме: %q", ev.Err)
	}
	if !mapCalled {
		t.Fatal("гибридный матчинг не вызван при fitEngine=llm")
	}
	if ev.Done == nil || ev.Done.Fit == nil {
		t.Fatal("нет вердикта фита в done-событии")
	}
	if len(ev.Done.Fit.Covered) != 1 || !strings.Contains(ev.Done.Fit.Covered[0].Note, "цитата") {
		t.Errorf("вердикт должен быть построен по цитате модели: %+v", ev.Done.Fit)
	}
	if ev.Done.Fit.Verdict != "apply" {
		t.Errorf("verdict = %q, хочу apply", ev.Done.Fit.Verdict)
	}
}

// Ошибка разметки (мусор вместо JSON) — откат на детерминированный матчер:
// панель вердикта не исчезает.
func TestGenerateHybridFitFallsBackOnBadJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctxDir := t.TempDir()
	os.WriteFile(filepath.Join(ctxDir, "01-profile.md"), []byte("Профиль: Go-разработчик."), 0o600)
	h := New(Config{
		ContextDir: ctxDir,
		FitEngine:  "llm",
		LLM: func(ctx context.Context, system, user string) (string, error) {
			return "Письмо готово.", nil
		},
		FitLLM: func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(system, "аудитор") {
				return "извините, не могу", nil // битая разметка
			}
			return `{"role":"go-primary","mustHave":[{"text":"Kubernetes в проде","kind":"must","category":"stack"}]}`, nil
		},
	})

	body, _ := json.Marshal(map[string]string{"vacancy": "Нужен Kubernetes."})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	ev := parseSSE(t, rec.Body.String())
	if ev.Err != "" {
		t.Fatalf("ошибка в стриме: %q", ev.Err)
	}
	if ev.Done == nil || ev.Done.Fit == nil {
		t.Fatal("откат не сработал: вердикта нет")
	}
	if len(ev.Done.Fit.Missing) != 1 {
		t.Errorf("детерминированный вердикт должен найти пробел: %+v", ev.Done.Fit)
	}
	if ev.Done.Fit.Verdict != "apply_with_caveats" {
		t.Errorf("verdict = %q, хочу apply_with_caveats", ev.Done.Fit.Verdict)
	}
}

// Без fitEngine вердикт считается матчером на правилах: разметка не вызывается.
func TestGenerateDeterministicFitEngineDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctxDir := t.TempDir()
	os.WriteFile(filepath.Join(ctxDir, "01-profile.md"), []byte("Профиль: Go-разработчик."), 0o600)
	mapCalled := false
	h := New(Config{
		ContextDir: ctxDir,
		LLM: func(ctx context.Context, system, user string) (string, error) {
			return "Письмо готово.", nil
		},
		FitLLM: func(ctx context.Context, system, user string) (string, error) {
			if strings.Contains(system, "аудитор") {
				mapCalled = true
			}
			return `{"role":"go-primary","mustHave":[{"text":"Go","kind":"must","category":"stack"}]}`, nil
		},
	})

	body, _ := json.Marshal(map[string]string{"vacancy": "Нужен Go."})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if mapCalled {
		t.Error("при пустом fitEngine модель разметки вызываться не должна")
	}
	ev := parseSSE(t, rec.Body.String())
	if ev.Done == nil || ev.Done.Fit == nil || ev.Done.Fit.Verdict == "" {
		t.Fatalf("детерминированный вердикт должен быть на месте: %+v", ev.Done)
	}
}

// TestGenerateEndpointFitFix — режим fitFix: сервер отдаёт письмо с
// fit-caveats обратно модели и возвращает исправленный результат с повторной
// проверкой. Проверяет, что fitFixable считается верно.
func TestGenerateEndpointFitFix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var gotUser string
	h := New(Config{ContextDir: t.TempDir(), LLM: func(ctx context.Context, system, user string) (string, error) {
		gotUser = user
		return "Стек: Go, PHP, ML, PostgreSQL.", nil
	}, FitLLM: func(ctx context.Context, system, user string) (string, error) {
		return "{}", nil
	}})
	body, _ := json.Marshal(map[string]any{
		"vacancy":    "PHP Laravel",
		"fitFix":     true,
		"letter":     "Стек: Go, PHP, ML, Laravel.",
		"fitCaveats": []string{"vet — не упомянуты"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("код = %d, тело: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotUser, "vet") {
		t.Errorf("в промпт fit-fix не попали caveats: %q", gotUser)
	}
	if !strings.Contains(gotUser, "Стек: Go, PHP, ML, Laravel.") {
		t.Errorf("в промпт fit-fix не попало письмо: %q", gotUser)
	}
	ev := parseSSE(t, rec.Body.String())
	if ev.Done == nil {
		t.Fatal("нет done-события")
	}
	if ev.Done.FitFixable < 0 {
		t.Errorf("fitFixable не должен быть отрицательным: %d", ev.Done.FitFixable)
	}
	// Контр-риск F1: omitempty при нуле может схлопнуть поле из JSON,
	// тогда фронтенд не увидит fitFixable и кнопка не появится.
	// Проверяем что сырой JSON содержит ключ "fitFixable".
	raw := rec.Body.String()
	if !strings.Contains(raw, `"fitFixable"`) {
		t.Errorf("fitFixable отсутствует в JSON-ответе (возможно omitempty схлопнул): %s", raw)
	}
}

func TestGenerateEndpointFitFixRequiresFields(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := New(Config{ContextDir: t.TempDir(), LLM: failIfCalled(t)})
	// Нет ничего — 400.
	body, _ := json.Marshal(map[string]any{"vacancy": "PHP", "fitFix": true})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("код = %d, хочу 400 (нет letter/fitCaveats)", rec.Code)
	}
	// Letter есть, fitCaveats нет — тоже 400.
	body2, _ := json.Marshal(map[string]any{"vacancy": "PHP", "fitFix": true, "letter": "текст"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body2))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("код = %d, хочу 400 (letter есть, fitCaveats нет)", rec.Code)
	}
}
