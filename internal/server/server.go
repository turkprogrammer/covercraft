// Package server — HTTP-сервер CoverCraft на 127.0.0.1 со случайным портом.
//
// UI (frontend/index.html, зашит через embed) ходит по относительным URL,
// поэтому порт в JS не инъектируется. Настройки хранит Go
// (internal/settings), а не LocalStorage: случайный порт меняет origin
// при каждом запуске и LocalStorage терял бы всё.
//
// Системный промпт НЕ персистится: GET /api/settings всегда отдаёт
// defaultSystemPrompt, кастомный промпт UI присылает в POST /api/generate
// и живёт он только в текущей сессии.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/turkprogrammer/covercraft/frontend"
	"github.com/turkprogrammer/covercraft/internal/audit"
	"github.com/turkprogrammer/covercraft/internal/cover"
	"github.com/turkprogrammer/covercraft/internal/llm"
	"github.com/turkprogrammer/covercraft/internal/settings"
)

// LLMFunc — шов для тестов: получает готовые system и user промпты.
type LLMFunc func(ctx context.Context, system, user string) (string, error)

// LLMStreamFunc — шов стриминга: дельты текста приходят через onDelta по
// мере генерации, полный текст — возвращаемым значением. Если в Config
// задан только LLM, сервер оборачивает его: одна дельта в конце.
type LLMStreamFunc func(ctx context.Context, system, user string, onDelta func(string)) (string, error)

// Config — зависимости хендлера.
type Config struct {
	ContextDir string        // папка context/*.md; может не существовать
	LLM        LLMFunc       // если nil — используется реальный клиент из settings
	LLMStream  LLMStreamFunc // если nil — буферизованный LLM (без стриминга)
}

// Handler обрабатывает запросы UI.
type Handler struct {
	cfg Config
}

// New собирает хендлер. LLM == nil означает «прод»: LLMStream — realLLMStream.
// Заданный в тестах LLM без LLMStream буферизуется (одна дельта в конце).
func New(cfg Config) *Handler {
	testLLM := cfg.LLM // заданный в тестах LLM без LLMStream буферизуем
	if cfg.LLM == nil {
		cfg.LLM = realLLM
	}
	if cfg.LLMStream == nil {
		if testLLM != nil {
			cfg.LLMStream = bufferedLLMStream(testLLM)
		} else {
			cfg.LLMStream = realLLMStream
		}
	}
	return &Handler{cfg: cfg}
}

// bufferedLLMStream — фоллбэк: обычный LLMFunc без стриминга, дельта одна.
func bufferedLLMStream(fn LLMFunc) LLMStreamFunc {
	return func(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
		letter, err := fn(ctx, system, user)
		if err != nil {
			return "", err
		}
		if letter != "" && onDelta != nil {
			onDelta(letter)
		}
		return letter, nil
	}
}

// ServeHTTP — единая точка входа: без внешних роутеров (KISS).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		io.WriteString(w, frontend.IndexHTML)
	case r.Method == http.MethodGet && r.URL.Path == "/api/settings":
		h.getSettings(w)
	case r.Method == http.MethodPost && r.URL.Path == "/api/settings":
		h.postSettings(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/generate":
		h.generate(w, r)
	default:
		http.Error(w, "не найдено", http.StatusNotFound)
	}
}

// settingsView — ответ GET /api/settings: настройки + дефолтный промпт.
type settingsView struct {
	settings.Settings
	DefaultSystemPrompt string `json:"defaultSystemPrompt"`
}

func (h *Handler) getSettings(w http.ResponseWriter) {
	s, err := settings.Load()
	if err != nil {
		http.Error(w, "не удалось прочитать настройки: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, settingsView{Settings: s, DefaultSystemPrompt: settings.DefaultSystemPrompt})
}

func (h *Handler) postSettings(w http.ResponseWriter, r *http.Request) {
	var s settings.Settings
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&s); err != nil {
		http.Error(w, "битый JSON в настройках", http.StatusBadRequest)
		return
	}
	// SystemPrompt не входит в Settings: даже если UI пришлёт его, он
	// не попадёт на диск — при следующем запуске будет дефолт.
	if err := settings.Save(s); err != nil {
		http.Error(w, "не удалось сохранить настройки: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, settingsView{Settings: s, DefaultSystemPrompt: settings.DefaultSystemPrompt})
}

type generateRequest struct {
	Vacancy      string `json:"vacancy"`
	SystemPrompt string `json:"systemPrompt"` // кастомный из UI; пусто → дефолт
	// AuditFix — режим автоправки: письмо + список замечаний аудита.
	// Модель получает их как инструкцию «исправь и верни полный текст».
	AuditFix bool     `json:"auditFix,omitempty"`
	Letter   string   `json:"letter,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// sseDelta — событие дельты в SSE-потоке /api/generate: кусок письма
// по мере генерации.
type sseDelta struct {
	Delta string `json:"delta"`
}

// sseDone — финальное событие done в SSE-потоке /api/generate.
// ElapsedMs — сколько отвечала модель. Warnings — результаты постпроверки
// письма (internal/audit): пустой срез, если письмо чистое.
type sseDone struct {
	Done      bool     `json:"done"`
	Letter    string   `json:"letter"`
	ElapsedMs int64    `json:"elapsedMs"`
	Warnings  []string `json:"warnings,omitempty"`
}

// sseError — событие ошибки внутри SSE-потока: после старта стрима код
// HTTP уже 200, поэтому ошибки доходят событием, а не статус-кодом.
type sseError struct {
	Error string `json:"error"`
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	var req generateRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		http.Error(w, "битый JSON", http.StatusBadRequest)
		return
	}
	if req.Vacancy == "" {
		http.Error(w, "vacancy пусто", http.StatusBadRequest)
		return
	}

	// Таймаут из настроек (UI ограничивает 5–900, дефолт 60): free-tier
	// не должен висеть минутами — по истечении понятная ошибка.
	timeoutSec := 60
	if s, err := settings.Load(); err == nil && s.TimeoutSec > 0 {
		timeoutSec = s.TimeoutSec
		if timeoutSec > 900 {
			timeoutSec = 900 // защита от битого файла
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// System-промпт: кастомный из сессии или дефолт (всегда восстанавливается).
	system := req.SystemPrompt
	if strings.TrimSpace(system) == "" {
		system = settings.DefaultSystemPrompt
	}

	// Режим автоправки: письмо + замечания аудита → модель переписывает.
	// Контекст профиля не пересобираем — правки только по списку замечаний.
	var user string
	if req.AuditFix {
		if strings.TrimSpace(req.Letter) == "" || len(req.Warnings) == 0 {
			http.Error(w, "auditFix требует letter и warnings", http.StatusBadRequest)
			return
		}
		user = fixPrompt(req.Letter, req.Warnings) + "\n\n" +
			cover.BuildUserPrompt(h.cfg.ContextDir, req.Vacancy)
	} else {
		// User-промпт собирает сервер: context/*.md + вакансия.
		user = cover.BuildUserPrompt(h.cfg.ContextDir, req.Vacancy)
	}

	streamGenerate(w, ctx, h.cfg.LLMStream, system, user, req.Vacancy, timeoutSec)
}

// streamGenerate вызывает LLM и пишет ответ как SSE: дельты по мере
// генерации (каждая с flush — UI обновляется живьём), в конце событие
// done с полным текстом, elapsedMs и warnings постпроверки. Ошибки после
// старта стрима идут событием {"error": ...}: заголовки уже отправлены.
// Если клиент отвалился (esc-abort), записи прекращаются, а upstream
// отменяется через ctx: он derived от r.Context(), который http-сервер
// отменяет при обрыве соединения (GenerateStream учитывает ctx в c.do).
func streamGenerate(w http.ResponseWriter, ctx context.Context, fn LLMStreamFunc, system, user, vacancy string, timeoutSec int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	// writeErr — клиент отключился; дальше в мёртвый writer не пишем.
	writeErr := false
	start := time.Now()
	letter, err := fn(ctx, system, user, func(delta string) {
		if writeErr {
			return
		}
		if e := writeSSE(w, sseDelta{Delta: delta}); e != nil {
			writeErr = true
			return
		}
		flush()
	})
	elapsed := time.Since(start)

	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			msg = fmt.Sprintf("таймаут: модель не ответила за %d сек — выберите модель быстрее или поднимите таймаут в api.config", timeoutSec)
		}
		if !writeErr {
			_ = writeSSE(w, sseError{Error: msg}) // best-effort: клиент мог отвалиться
		}
		flush()
		return
	}
	// elapsedMs — счётчик времени ответа модели (замер пользователя).
	// Постпроверка: теряемые факты и запрещённые паттерны видны в UI.
	warnings := audit.Check(letter, vacancy).Warnings
	if !writeErr {
		_ = writeSSE(w, sseDone{Done: true, Letter: letter, ElapsedMs: elapsed.Milliseconds(), Warnings: warnings}) // best-effort
	}
	flush()
}

// writeSSE — одно событие протокола Server-Sent Events. Ошибка обычно
// означает, что клиент отключился; json.Marshal здесь упасть не может
// (простые структуры), поэтому любая ошибка трактуется как обрыв записи.
func writeSSE(w http.ResponseWriter, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(w, "data: %s\n\n", b)
	return werr
}

// fixPrompt — user-промпт режима автоправки: письмо + замечания аудита.
// Модель получает конкретный список, а не весь контекст профиля: правки
// точечные, риск заново что-то выдумать или потерять меньше.
func fixPrompt(letter string, warnings []string) string {
	var b strings.Builder
	b.WriteString("Ниже — сопроводительное письмо и замечания автоматической проверки.\n")
	b.WriteString("Исправь ТОЛЬКО перечисленное, сохранив остальной текст, структуру и стиль письма без изменений.\n")
	b.WriteString("Все факты для исправлений бери из раздела «Контекст кандидата» ниже — ничего не выдумывай.\n")
	b.WriteString("Правила исправления (важно):\n")
	b.WriteString("- «одновременно в буллетах и в пробелах»: убери технологию из секции «Честно о пробелах», в буллетах оставь как есть;\n")
	b.WriteString("- «в строке стека»: удали слово из строки «Стек:» — переносить никуда не нужно, оно уже есть в буллетах;\n")
	b.WriteString("- «потерян факт»: добавь недостающий факт одной короткой строкой в подходящий буллет.\n")
	b.WriteString("Верни полный исправленный текст письма, без пояснений и комментариев.\n\n")
	b.WriteString("Замечания:\n")
	for _, w := range warnings {
		b.WriteString("- ")
		b.WriteString(w)
		b.WriteString("\n")
	}
	b.WriteString("\nПисьмо:\n")
	b.WriteString(letter)
	return b.String()
}

// clientFromSettings — LLM-клиент из сохранённых настроек; общий для
// буферизованного и стримингового путей.
func clientFromSettings() (llm.Client, error) {
	s, err := settings.Load()
	if err != nil {
		return llm.Client{}, fmt.Errorf("не удалось прочитать настройки: %w", err)
	}
	return llm.Client{
		BaseURL:         s.BaseURL,
		APIKey:          s.APIKey,
		Model:           s.Model,
		ReasoningEffort: s.ReasoningEffort,
		Timeout:         time.Duration(s.TimeoutSec) * time.Second,
	}, nil
}

// realLLM — буферизованный продовый клиент (фоллбэк без стриминга).
func realLLM(ctx context.Context, system, user string) (string, error) {
	c, err := clientFromSettings()
	if err != nil {
		return "", err
	}
	return c.Generate(ctx, system, user)
}

// realLLMStream — продовая реализация стриминга: клиент из настроек;
// промпты уже собраны сервером, дельты летят в UI по мере генерации.
func realLLMStream(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
	c, err := clientFromSettings()
	if err != nil {
		return "", err
	}
	return c.GenerateStream(ctx, system, user, onDelta)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// ListenAndServeRandomPort слушает 127.0.0.1:0 и возвращает фактический URL.
// Порт выделяет ОС (свободный), каждый запрос логируется в stderr.
func ListenAndServeRandomPort(h http.Handler) (addr string, err error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		h.ServeHTTP(w, r)
	}))
	return "http://127.0.0.1:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port), nil
}
