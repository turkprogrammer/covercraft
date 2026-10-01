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
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/turkprogrammer/covercraft/frontend"
	"github.com/turkprogrammer/covercraft/internal/audit"
	"github.com/turkprogrammer/covercraft/internal/cover"
	"github.com/turkprogrammer/covercraft/internal/fit"
	"github.com/turkprogrammer/covercraft/internal/llm"
	"github.com/turkprogrammer/covercraft/internal/prompt"
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
	// FitLLM — LLM для извлечения требований вакансии (internal/fit);
	// если nil — тот же LLM, что и для письма.
	FitLLM LLMFunc
	// ComposeLLM — LLM-вызов композера системного промпта (POST
	// /api/prompt/compose); не-стриминговый. Если nil — тот же LLM, что и
	// для письма (тесты: заданный в Config.LLM).
	ComposeLLM LLMFunc
	// FitEngine — движок матчинга фита: "" / "deterministic" — матчер на
	// правилах; "llm" — гибрид «модель размечает покрытие, код проверяет
	// цитаты» (internal/fit/map.go). При ошибке модели — всегда откат на
	// детерминированный вердикт.
	FitEngine string
}

// Handler обрабатывает запросы UI.
type Handler struct {
	cfg Config
	// concepts — динамические концепты, инициализируются лениво при
	// первом generate() через sync.Once (initConcepts). Хранятся
	// в Handler, чтобы не пересчитывать на каждый запрос.
	concepts        []fit.Concept
	conceptsOnce    sync.Once
	conceptsInitErr error
}

// New собирает хендлер. LLM == nil означает «прод»: LLMStream — realLLMStream.
// Заданный в тестах LLM без LLMStream буферизуется (одна дельта в конце).
func New(cfg Config) *Handler {
	testLLM := cfg.LLM // заданный в тестах LLM без LLMStream буферизуем
	if cfg.LLM == nil {
		cfg.LLM = realLLM
	}
	if cfg.FitLLM == nil {
		if testLLM != nil {
			cfg.FitLLM = testLLM // тесты: извлечение через тот же fake
		} else {
			cfg.FitLLM = realLLM // прод: тот же клиент, отдельный вызов
		}
	}
	if cfg.ComposeLLM == nil {
		if testLLM != nil {
			cfg.ComposeLLM = testLLM
		} else {
			cfg.ComposeLLM = realLLM
		}
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
	// Паника в хендлере не должна ронять соединение и оставлять UI в
	// неопределённом состоянии (пользователю приходилось закрывать
	// приложение — живой кейс, сентябрь 2026). Ловим, логируем и отвечаем
	// ошибкой: клиент покажет её в статусной строке и остановит цикл.
	defer func() {
		if rc := recover(); rc != nil {
			log.Printf("паника в %s %s: %v\n%s", r.Method, r.URL.Path, rc, debug.Stack())
			defer func() { _ = recover() }() // ответ мог уже начаться
			http.Error(w, "внутренняя ошибка сервера — детали в логе", http.StatusInternalServerError)
		}
	}()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		io.WriteString(w, frontend.IndexHTML)
	case r.Method == http.MethodGet && r.URL.Path == "/api/settings":
		h.getSettings(w)
	case r.Method == http.MethodPost && r.URL.Path == "/api/settings":
		h.postSettings(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/generate":
		h.generate(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/prompt/compose":
		h.composePrompt(w, r)
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
	// FitFix — режим автоправки по результату fit: письмо + список
	// «впиши в письмо» caveat от fit-engine. Модель дописывает недостающие
	// факты в существующие буллеты, сохраняя остальной текст без изменений.
	FitFix     bool     `json:"fitFix,omitempty"`
	FitCaveats []string `json:"fitCaveats,omitempty"`
	// FitMaxIter — бюджет итераций автоправки от UI. Ноль/отсутствие — один
	// проход: каждая итерация стоит генерацию (до 60 сек) плюс fitMapTimeout
	// (90 сек), и три прохода — это ~7,5 минут молчания (живой кейс, 2026-09).
	FitMaxIter int `json:"fitMaxIter,omitempty"`
	// DropSections — разделы профиля, вырезаемые из user-промпта (решил
	// композер). Пусто — промпт как раньше.
	DropSections []prompt.Drop `json:"dropSections,omitempty"`
}

// sseDelta — событие дельты в SSE-потоке /api/generate: кусок письма
// по мере генерации.
type sseDelta struct {
	Delta string `json:"delta"`
}

// sseDone — финальное событие done в SSE-потоке /api/generate.
// ElapsedMs — сколько отвечала модель. Warnings — результаты постпроверки
// письма (internal/audit): пустой срез, если письмо чистое.
// FitFixable — сколько caveats вида «впиши в письмо» ещё остались после
// генерации; UI использует это для решения, показывать ли кнопку fit-fix.
type sseDone struct {
	Done      bool     `json:"done"`
	Letter    string   `json:"letter"`
	ElapsedMs int64    `json:"elapsedMs"`
	Warnings  []string `json:"warnings,omitempty"`
	Fit       *fit.Fit `json:"fit,omitempty"`
	// FitFixable всегдаserialизуется (без omitempty): фронтенд проверяет
	// presence of the field to decide whether to show the fit-fix button.
	// При 0 поле равно 0, не опускается — JS видит 0 и скрывает кнопку.
	FitFixable int `json:"fitFixable"`
	// ProfileWarning — профиля нет (папка context/*.md отсутствует или пуста):
	// письмо написано без фактов о кандидате. Отдельное поле, а не warnings,
	// потому что warnings возвращаются модели при автоправке — инструкция
	// «профиль пуст» заставила бы её выдумывать факты.
	ProfileWarning string `json:"profileWarning,omitempty"`
	// UsedSystemPrompt — фактически отправленный системный промпт
	// (кастом или дефолт): для отладки, что реально применялось.
	UsedSystemPrompt string `json:"usedSystemPrompt"`
}

// sseError — событие ошибки внутри SSE-потока: после старта стрима код
// HTTP уже 200, поэтому ошибки доходят событием, а не статус-кодом.
type sseError struct {
	Error string `json:"error"`
}

// resolveSystemPrompt — единственная точка выбора системного промпта:
// непустой кастом полностью заменяет дефолт (D2). Пустое поле — дефолт.
func resolveSystemPrompt(system string) string {
	if strings.TrimSpace(system) != "" {
		return system
	}
	return settings.DefaultSystemPrompt
}

// timeoutFromSettings — единое правило таймаута для generate и compose:
// дефолт 60, clamp 900 (защита от битого файла), 0/отсутствие — дефолт.
// context.WithTimeout строит вызывающий — хелпер не трогает контекст.
func timeoutFromSettings() int {
	timeoutSec := 60
	if s, err := settings.Load(); err == nil && s.TimeoutSec > 0 {
		timeoutSec = s.TimeoutSec
		if timeoutSec > 900 {
			timeoutSec = 900
		}
	}
	return timeoutSec
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

	// conceptsOnce — единственная точка записи в h.concepts. Fallback на
	// дефолты живёт внутри initConcepts (см. его конец), поэтому после
	// Once.Do поле неизменяемо и читается без синхронизации.
	//
	// Lazy-инициализация блокирующим LLM-вызовом при первом generate()
	// вместо New() — иначе тесты server_test.go с fake-LLM висели бы
	// 30 секунд на старте.
	h.conceptsOnce.Do(h.initConcepts)
	if h.conceptsInitErr != nil {
		log.Printf("init concepts failed: %v — using defaults", h.conceptsInitErr)
	}

	// Таймаут из настроек (UI ограничивает 5–900, дефолт 60): free-tier
	// не должен висеть минутами — по истечении понятная ошибка.
	timeoutSec := timeoutFromSettings()
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// System-промпт: кастомный из сессии или дефолт (всегда восстанавливается).
	system := resolveSystemPrompt(req.SystemPrompt)

	// Режим автоправки: письмо + замечания аудита → модель переписывает.
	// Контекст профиля не пересобираем — правки только по списку замечаний.
	// Валидация до извлечения: битый запрос не должен трогать LLM вообще.
	if req.AuditFix && (strings.TrimSpace(req.Letter) == "" || len(req.Warnings) == 0) {
		http.Error(w, "auditFix требует letter и warnings", http.StatusBadRequest)
		return
	}
	// FitFix аналогично: требуется и письмо, и список caveat строк.
	if req.FitFix && (strings.TrimSpace(req.Letter) == "" || len(req.FitCaveats) == 0) {
		http.Error(w, "fitFix требует letter и fitCaveats", http.StatusBadRequest)
		return
	}
	// Бюджет итераций: клиент вправе попросить больше одного прохода, но не
	// больше потолка — иначе цикл растянется на минуты.
	if req.FitMaxIter > maxFitIterations {
		req.FitMaxIter = maxFitIterations
	}
	if req.FitMaxIter < 1 {
		req.FitMaxIter = 1
	}

	// Извлечение требований вакансии — ДО генерации письма: must-have идут
	// в промпт письма чек-листом, а тот же разбор переиспользуется в фите
	// (один LLM-вызов на извлечение, второго нет). Ошибка разбора — не
	// фатальна: письмо генерируется без чек-листа, вердикта не будет.
	reqs, extractOK := fit.Requirements{}, false
	if r, err := fit.ExtractRequirements(ctx, fit.LLMFunc(h.cfg.FitLLM), req.Vacancy); err == nil {
		reqs, extractOK = r, true
	}
	musts := make([]string, 0, len(reqs.MustHave))
	for _, m := range reqs.MustHave {
		musts = append(musts, m.Text)
	}

	// Режим автоправки: письмо + замечания аудита → модель переписывает.
	var user string
	switch {
	case req.AuditFix:
		user = fixPrompt(req.Letter, req.Warnings) + "\n\n" +
			cover.BuildUserPrompt(h.cfg.ContextDir, req.Vacancy, musts, req.DropSections)
	case req.FitFix:
		// Только релевантные caveat секции профиля, не все 85 КБ: на полном
		// профиле модель тонет и отвечает эхом (живой баг, октябрь 2026).
		user = fitFixPrompt(req.Letter, req.FitCaveats) + "\n\n" +
			cover.BuildFitFixUserPrompt(h.cfg.ContextDir, req.Vacancy, musts, req.FitCaveats, req.DropSections)
	default:
		// User-промпт собирает сервер: context/*.md + вакансия + чек-лист.
		user = cover.BuildUserPrompt(h.cfg.ContextDir, req.Vacancy, musts, req.DropSections)
	}

	// Движок фита: "llm" — гибридная разметка покрытия моделью (отдельный
	// LLM-вызов после письма, всегда с откатом на детерминированный матчер),
	// иначе — прежний матчер на правилах, без дополнительных вызовов.
	var mapFn fit.MapFunc
	if strings.EqualFold(h.cfg.FitEngine, "llm") {
		mapFn = func(ctx context.Context, concepts []fit.Concept, reqs fit.Requirements, profile, letter, vac string) (fit.Fit, error) {
			return fit.MapCoverage(ctx, fit.LLMFunc(h.cfg.FitLLM), concepts, reqs, profile, letter, vac)
		}
	}

	streamGenerate(w, ctx, h.cfg.LLMStream, system, user, req.Vacancy, fit.LoadProfile(h.cfg.ContextDir, req.DropSections), reqs, extractOK, mapFn, h.concepts, timeoutSec, req.FitFix, req.Letter)
}

// fitMapTimeout — бюджет гибридной разметки покрытия. Разметка идёт после
// генерации письма, и медленный маппинг не должен оставить панель без вердикта
// — по таймауту откат на детерминированный матчер. Отмена запроса пользователем
// (Esc) отменяет и разметку: продолжать платить токены за невидимый результат
// незачем.
const fitMapTimeout = 90 * time.Second

// fitVerdict — вердикт фита: гибридный матчинг (если задан mapFn) с откатом
// на детерминированный Evaluate при ошибке модели, таймауте или битом JSON.
func fitVerdict(ctx context.Context, concepts []fit.Concept, reqs fit.Requirements, profile, letter, vacancy string, mapFn fit.MapFunc) fit.Fit {
	if mapFn != nil {
		mctx, cancel := context.WithTimeout(ctx, fitMapTimeout)
		defer cancel()
		if f, err := mapFn(mctx, concepts, reqs, profile, letter, vacancy); err == nil && f.Verdict != "" {
			return f
		}
	}
	return fit.Evaluate(concepts, reqs, profile, letter, vacancy)
}

// streamGenerate вызывает LLM и пишет ответ как SSE: дельты по мере
// генерации (каждая с flush — UI обновляется живьём), в конце событие
// done с полным текстом, elapsedMs, warnings постпроверки и вердиктом
// фита. Ошибки после старта стрима идут событием {"error": ...}:
// заголовки уже отправлены.
//
// reqs/extractOK — уже выполненный шаг 1 фита (см. generate): при
// extractOK вердикт считается детерминированно, без новых LLM-вызовов.
func streamGenerate(w http.ResponseWriter, ctx context.Context, fn LLMStreamFunc, system, user, vacancy, profile string, reqs fit.Requirements, extractOK bool, mapFn fit.MapFunc, concepts []fit.Concept, timeoutSec int, fitFix bool, origLetter string) {
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
	// Эхо промпта: модель на входе из 80 КБ профиля иногда возвращает кусок
	// самого промпта вместо письма (живой кейс — повторный фит, сентябрь 2026).
	// Тогда UI показывал в LETTER.OUT весь файл профиля, а цикл автоправки
	// крутился дальше. Исходное письмо — актив пользователя, выводом модели
	// его не заменяем: отдаём письмо как было и предупреждаем.
	//
	// Условие fitFix здесь больше НЕ проверяется. Живой баг (сентябрь 2026):
	// защита стояла только под автоправкой, поэтому на первом обычном проходе
	// модель отдала кусок context/01 (заголовки «### 1.3 Bundle ID Service»,
	// пометки «🔴 Не переносить…») — UI показал файл профиля как письмо, цикл
	// fit-fix накручивал на него новое, и приложение падало. Эхо — это эхо
	// независимо от режима генерации.
	//
	// Отсечка по размеру: ответ за пределами правдоподобного письма —
	// это мусор (профиль, вакансия, повтор промпта), а не письмо. Письмо —
	// 200–400 слов, то есть до ~4 КБ; берём потолок с запасом на длинный
	// перечень фактов. Скриншот живого бага показывал 11 742 символа.
	echoWarning := ""
	if len([]rune(letter)) > maxLetterRunes {
		letter = ""
		echoWarning = "модель вернула ответ неприемлемого размера (" +
			strconv.Itoa(len([]rune(letter))) + " символов вместо ~" +
			strconv.Itoa(maxLetterRunes) + ") — похоже на кусок профиля или промпта. Письмо не сохранено, повтори генерацию"
	}
	if echoWarning == "" && isPromptEcho(letter, user, origLetter) {
		if strings.TrimSpace(origLetter) != "" {
			// fit-fix и audit-fix передают исходное письмо — это актив
			// пользователя, откатываем на него (и при audit-fix тоже: раньше
			// откат стоял только под fitFix, и audit-fix терял письмо целиком).
			letter = origLetter
			echoWarning = "модель вернула эхо промпта вместо письма — исходное письмо сохранено, правь вручную или повтори автоправку"
		} else {
			// Исходного письма нет: откатывать не на что, поэтому наружу не
			// отдаём мусор — пустое письмо плюс явное объяснение.
			letter = ""
			echoWarning = "модель вернула эхо промпта вместо письма (вместо текста пришло содержимое профиля) — письмо не сохранено, повтори генерацию. Если повторяется — смени модель или сократи профиль"
		}
	}
	// elapsedMs — счётчик времени ответа модели (замер пользователя).
	// Постпроверка: теряемые факты и запрещённые паттерны видны в UI.
	var warnings []string
	if echoWarning != "" {
		// При отклонённом эхо аудит НЕ запускаем: letter может быть пустым или
		// исходным — Check по нему выдаёт замечания-мусор («нет обязательной
		// секции», «Потерян факт»), которые пользователь принимает за реальные
		// дефекты письма. Наружу — ровно одна причина: эхо.
		warnings = []string{echoWarning}
	} else {
		warnings = audit.Check(letter, vacancy).Warnings
		// Сверка заявленных фактов с профилем: Check профиль не читает и выдумку
		// («XSSI sanitization», «basic auth») пропускает — письмо уходило с ложью.
		warnings = append(warnings, audit.CheckProfile(letter, profile).Warnings...)
	}

	// Пустой профиль — не ошибка запроса, но письмо без единого факта о
	// кандидате; пользователь должен узнать об этом до отправки письма.
	profileWarning := ""
	if strings.TrimSpace(profile) == "" {
		profileWarning = "профиль пуст: не найдено ни одного context/*.md — письмо написано без фактов о вас. Создайте папку context рядом с бинарником (или в текущем каталоге) и повторите."
	}

	// Шаг 2 фита — вердикт; разбор вакансии уже готов (выполнен до генерации
	// письма и переиспользуется). Детерминированный матчер — ноль токенов;
	// гибрид (FitEngine="llm") добавляет один вызов модели на разметку
	// покрытия с откатом на матчер при любой ошибке.
	var verdict *fit.Fit
	if extractOK {
		f := fitVerdict(ctx, concepts, reqs, profile, letter, vacancy, mapFn)
		if f.Verdict != "" {
			verdict = &f
		}
	}

	if !writeErr {
		fixableN := 0
		if verdict != nil {
			fixableN = len(fit.FitFixableCaveats(*verdict))
		}
		_ = writeSSE(w, sseDone{
			Done: true, Letter: letter, ElapsedMs: elapsed.Milliseconds(),
			Warnings: warnings, Fit: verdict, FitFixable: fixableN,
			ProfileWarning: profileWarning, UsedSystemPrompt: system,
		}) // best-effort
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

// composeResponse — ответ POST /api/prompt/compose: отбор разделов профиля,
// нерелевантных вакансии. Системный промпт здесь НЕ возвращается и моделью не
// переписывается: поле #systemPrompt — актив пользователя, и compose его не
// трогает (прежний контракт затирал рукописный промпт обрезанной до 5 КБ
// производной). sections/droppedBytes — чтобы UI показал «было 74 КБ → стало
// N КБ», а не магию.
type composeResponse struct {
	DropSections []prompt.Drop `json:"dropSections"`
	Reason       string        `json:"reason"`
	Sections     int           `json:"sections"`
	DroppedBytes int           `json:"droppedBytes"`
	ElapsedMs    int64         `json:"elapsedMs"`
}

// composePrompt — один не-стриминговый LLM-вызов: по текущей вакансии выбирает
// разделы профиля, которые в неё не попадают. Системный промпт не
// переписывается — поле остаётся за пользователем. Пустая вакансия → 400 до LLM
// (прецедент generate): пустое не должно стоить вызова.
func (h *Handler) composePrompt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Vacancy string `json:"vacancy"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		http.Error(w, "битый JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Vacancy) == "" {
		http.Error(w, "vacancy пусто", http.StatusBadRequest)
		return
	}

	timeoutSec := timeoutFromSettings()
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	start := time.Now()

	// Заголовки профиля вместо 67 КБ контента.
	var sections []prompt.Section
	for _, s := range cover.ProfileSections(h.cfg.ContextDir) {
		if file, heading, ok := strings.Cut(s, " :: "); ok {
			sections = append(sections, prompt.Section{File: file, Heading: heading})
		}
	}

	// must-have для промпта композера; отказ разбора не фатален — роль "",
	// musts nil (прецедент generate).
	var role string
	var musts []string
	if reqs, err := fit.ExtractRequirements(ctx, fit.LLMFunc(h.cfg.FitLLM), req.Vacancy); err == nil {
		role = reqs.Role
		for _, m := range reqs.MustHave {
			musts = append(musts, m.Text)
		}
	}

	system, user := prompt.Compose(req.Vacancy, role, musts, sections)
	raw, err := h.cfg.ComposeLLM(ctx, system, user)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "композер не ответил: " + err.Error()})
		return
	}
	res, err := prompt.Parse(raw, sections)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	// Сколько профиля сэкономлено: замер на собранном user-промпте до/после.
	full := cover.BuildUserPrompt(h.cfg.ContextDir, req.Vacancy, musts, nil)
	cut := cover.BuildUserPrompt(h.cfg.ContextDir, req.Vacancy, musts, res.Drop)

	writeJSON(w, http.StatusOK, composeResponse{
		DropSections: res.Drop,
		Reason:       res.Reason,
		Sections:     len(sections),
		DroppedBytes: len(full) - len(cut),
		ElapsedMs:    time.Since(start).Milliseconds(),
	})
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

// fitFixPrompt — user-промпт режима автоправки по результату fit.
// Модель получает конкретный список «чего не хватило в письме» от fit-engine
// и инструкцию дописать недостающие факты в существующие буллеты, не
// переписывая письмо заново.
func fitFixPrompt(letter string, caveats []string) string {
	var b strings.Builder
	b.WriteString("Ниже — сопроводительное письмо и список того, чего fit-движок не нашёл в письме.\n")
	b.WriteString("Каждая строка — это требование, у которого в профиле ЕСТЬ факт, но он НЕ попал в письмо.\n")
	b.WriteString("Твоя задача: добавить отсутствующие факты в соответствующие буллеты, сохранив остальной текст без изменений.\n")
	b.WriteString("Правила (важно):\n")
	b.WriteString("- НЕ меняй уже написанные буллеты — только ДОПОЛНЯЙ недостающие факты;\n")
	b.WriteString("- Не добавляй новые буллеты, если факт можно вписать в существующий;\n")
	b.WriteString("- Все факты бери ТОЛЬКО из профиля кандидата ниже — НЕ выдумывай;\n")
	b.WriteString("- Верни полный исправленный текст письма, без пояснений.\n\n")
	b.WriteString("Что нужно добавить в письмо:\n")
	for _, c := range caveats {
		b.WriteString("- ")
		b.WriteString(c)
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

// conceptsCachePath — путь к кэшу концептов. Тот же XDG, что и settings.json
// (консистентно с settings.go:59–63).
func conceptsCachePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "covercraft")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "concepts.json"), nil
}

// initConcepts — ленивая инициализация концептов: читает кэш, при
// несовпадении хэша или отсутствии файла — пробует LLM-генерацию.
// При любой ошибке (нет LLM, таймаут, битый JSON) использует DefaultConcepts.
func (h *Handler) initConcepts() {
	defer func() {
		if r := recover(); r != nil {
			h.conceptsInitErr = fmt.Errorf("panic: %v", r)
		}
	}()
	cachePath, err := conceptsCachePath()
	if err != nil {
		h.conceptsInitErr = err
		return
	}
	hash := fit.ProfileHash(h.cfg.ContextDir)
	if hash != "" {
		if concepts, savedHash, err := fit.LoadConcepts(cachePath); err == nil && savedHash == hash {
			h.concepts = concepts
			return
		}
	}
	// Нет валидного кэша. LLM-генерация концептов — только в гибридном
	// режиме: детерминированный движок не требует дополнительных вызовов
	// модели (TestGenerateDeterministicFitEngineDefault проверяет это).
	if strings.EqualFold(h.cfg.FitEngine, "llm") && h.cfg.FitLLM != nil && h.cfg.ContextDir != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		profile := fit.LoadProfile(h.cfg.ContextDir, nil)
		if profile != "" {
			concepts, raw, err := fit.GenerateConcepts(ctx, fit.LLMFunc(h.cfg.FitLLM), profile)
			if err == nil && len(concepts) > 0 {
				h.concepts = concepts
				// raw-структура нужна для roundtrip Save → Load: без неё
				// кэш запишет только имена и LoadConcepts отбросит всё
				// (P0: ранее здесь сохранялся мусор).
				if saveErr := fit.SaveConceptsFile(cachePath, raw, hash); saveErr != nil {
					log.Printf("сохранение кэша концептов: %v", saveErr)
				}
				return
			}
		}
	}
	// Fallback на дефолты — лучше устаревший набор, чем ничего.
	h.concepts = fit.DefaultConcepts()
}

// isPromptEcho — ответ модели является эхом собственного промпта, а не письмом.
//
// Живой кейс (вакансия IAM, сентябрь 2026): в режиме автоправки по фиту в
// user-промпт уходит весь профиль (80 КБ), и модель возвращала его кусок
// вместо письма. UI показывал в LETTER.OUT файл профиля, а цикл fit-fix
// продолжал крутиться — отсюда «виснет и выводит полный контент профиля».
//
// Признак эха — длинный дословный фрагмент входа. Берём самые длинные строки
// промпта (заголовки разделов и строки чек-листа) и ищем их в ответе: при
// точном попадании модель явно цитирует вход, а не пишет письмо. Порог по
// длине строки отсекает короткие совпадения вроде «### Вакансия» в письме
// про вакансию.
func isPromptEcho(letter, user, origLetter string) bool {
	// norm снимает знаки-обвязки markdown и схлопывает пробелы: «**### Вакансия**»
	// и «### Вакансия» — одно и то же эхо, а не совпадение по форме.
	norm := func(s string) string {
		s = strings.Map(func(r rune) rune {
			if r == '*' || r == '`' || r == '#' || r == '_' {
				return -1
			}
			return r
		}, s)
		return strings.Join(strings.Fields(strings.ToLower(s)), " ")
	}
	lowUser := norm(user)
	if lowUser == "" {
		return false
	}
	// fit-fix/audit-fix вкладывают исходное письмо в user («Письмо: …»), и
	// ответ модели — это же письмо с правкой. Живой регресс (октябрь 2026):
	// доля дословного была 100% при ЛЮБОЙ содержательной правке, и автофикс
	// откатывал каждую попытку — «автофикс не может исправить никак».
	// Строки самого письма эхом не считаются: эхо — это дословное повторение
	// СВЕРХ письма (профиль, инструкции, вакансия).
	lowOrig := norm(origLetter)
	// Эхо — это когда ответ ПОЧТИ ЦЕЛИКОМ дословно из промпта, а не когда
	// одна строка совпала. Живой баг (октябрь 2026): контактная строка письма
	// (105 символов) дословно лежит в context/00-контакты.md, и построчный
	// детектор отклонял ВАЛИДНОЕ письмо: done.letter = "", копирование
	// блокировалось, аудит по пустой строке выдавал 7 замечаний-мусора, а
	// fit-fix откатывал каждую исправленную версию — «деградация после
	// нескольких попыток». Контакты, строка стека и цитаты фактов нормально
	// воспроизводят профиль дословно — это письмо, а не эхо.
	var hit, total int
	for _, raw := range strings.Split(letter, "\n") {
		line := norm(raw)
		if len(line) < echoMinLineLen {
			continue
		}
		if lowOrig != "" && strings.Contains(lowOrig, line) {
			continue
		}
		total += len(line)
		if strings.Contains(lowUser, line) {
			hit += len(line)
		}
	}
	if total > 0 && float64(hit) >= float64(total)*echoHitRatio {
		return true
	}
	// Структурный признак эха: ответ выглядит как кусок профиля, а не как
	// письмо. Живой случай (сентябрь 2026): модель переписала вход с
	// переформатированием — «### 1.3 Bundle ID Service — классификация
	// приложений» вместо заголовка профиля, пометки с эмодзи, двоеточия после
	// заголовков. Дословного совпадения строк не осталось, и построчный
	// детектор молчал, а UI показал файл профиля как письмо.
	//
	// Признаки, которые не пересекаются с настоящим письмом:
	//   - ≥3 markdown-заголовка уровня ##/### (письмо использует жирные
	//     подзаголовки «**Go и highload:**», а не решётки);
	//   - при этом ни одного письменного обращения и ни подписи.
	headings := 0
	for _, raw := range strings.Split(letter, "\n") {
		if sectionHeadingRe.MatchString(raw) {
			headings++
		}
	}
	if headings >= echoMinHeadings && !looksLikeLetter(letter) {
		return true
	}
	return false
}

// sectionHeadingRe — markdown-заголовок уровня ## или ###.
var sectionHeadingRe = regexp.MustCompile(`(?m)^\s{0,4}#{2,3}\s+\S`)

// letterMarkerRe — признаки настоящего письма: обращение и подпись.
var letterMarkerRe = regexp.MustCompile(`(?i)здравствуйте|добрый день|доброе утро|уважением|с уважением|спасибо за внимание|буду рад|рад вас|отправляю`)

// looksLikeLetter — в ответе есть обращение или подпись.
func looksLikeLetter(s string) bool {
	return letterMarkerRe.MatchString(s)
}

// echoHitRatio — какая доля длины ответа должна быть дословно из промпта,
// чтобы считать ответ эхом. Живое письмо с контактной строкой и цитатами
// фактов даёт ~10–30% дословного; эхо профиля — почти целиком (≥ 80%).
const echoHitRatio = 0.45

// echoMinLineLen — минимальная длина строки ответа (после нормализации),
// при которой её дословное совпадение с промптом считается эхом. Короткие
// строки не ловятся намеренно: «### Вакансия» есть и в нормальном письме.
const echoMinLineLen = 40

// echoMinHeadings — сколько markdown-заголовков раздела в ответе означают
// «это кусок профиля, а не письмо». Письмо оформляется жирными подзаголовками,
// решётки использует только профиль.
const echoMinHeadings = 3

// maxFitIterations — потолк итераций автоправки по фиту. Три прохода по
// 150 секунд — это ~7,5 минут молчания (живой кейс, сентябрь 2026), поэтому
// дефолт — один проход, а предел жёсткий.
const maxFitIterations = 3

// maxLetterRunes — потолок размера письма. Письмо — до 200–400 слов, то есть
// порядка 2–4 КБ символов; 8 КБ берём с запасом на развёрнутый перечень
// фактов. Всё, что длиннее, — не письмо.
const maxLetterRunes = 8000
