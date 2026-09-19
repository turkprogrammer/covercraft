// Динамические концепты из профиля кандидата: концепты генерируются
// LLM из context/*.md и кэшируются в ~/.config/covercraft/concepts.json
// с привязкой к SHA-256 хэшу профиля. При недоступности LLM или битом
// JSON используются DefaultConcepts() — захардкоженный fallback, который
// сохраняет поведение системы до рефакторинга.
//
// Контракт: LLM возвращает списки терминов (простые строки), код
// собирает из них *regexp.Regexp через regexp.QuoteMeta. Это снимает
// с модели ответственность за корректный Go-regex-синтаксис и
// позволяет валидировать семантику (минимальная длина термина).
package fit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Concept — экспортируемая версия внутреннего концепта: триггер
// (когда требование вакансии говорит об этом концепте) + сигналы
// (факты из профиля, которые подтверждают наличие навыка).
type Concept struct {
	Name    string
	Trigger *regexp.Regexp
	Signals []Signal
}

// Signal — одна группа фактов внутри концепта. Каждая группа считается
// за один хит: «в письме есть at-least-once», «в профиле есть идемпотентность».
type Signal struct {
	Label string
	Re    *regexp.Regexp
}

// RawConcept — форма из JSON, до валидации и компиляции regex.
type RawConcept struct {
	Name         string      `json:"name"`
	TriggerTerms []string    `json:"trigger_terms"`
	Signals      []RawSignal `json:"signals"`
}

type RawSignal struct {
	Label string   `json:"label"`
	Terms []string `json:"terms"`
}

// rawConceptsFile — верхний уровень concepts.json: концепты + хэш
// профиля на момент генерации (для инвалидации кэша).
type rawConceptsFile struct {
	ProfileHash string       `json:"profile_hash"`
	Concepts    []RawConcept `json:"concepts"`
}

// minTermLen — минимальная длина термина в рунах. Термины короче
// (например «go», «ml», «api») дают substring-шум без границ слова —
// найдутся в любом тексте и закроют чужие требования.
const minTermLen = 4

// minTriggerTerms / minSignalGroups — минимальное наполнение концепта:
// один-два термина легко ловят случайные совпадения, а с 2+ группами
// сигналов хит хотя бы по двум измерениям подтверждает навык.
const minTriggerTerms = 2
const minSignalGroups = 2

// BuildConcept — компилирует один RawConcept в валидный Concept.
// Валидация: имя непустое, минимум 2 trigger_terms, минимум 2 signal-группы,
// каждый term непустой и длиной ≥ 4 руны. Ошибка возвращается, концепт
// отбрасывается: остальные продолжают работать.
func BuildConcept(raw RawConcept) (Concept, error) {
	name := strings.TrimSpace(raw.Name)
	if name == "" {
		return Concept{}, errors.New("пустое имя концепта")
	}
	if len(raw.TriggerTerms) < minTriggerTerms {
		return Concept{}, fmt.Errorf("концепт %q: нужно минимум %d trigger_terms, получено %d", name, minTriggerTerms, len(raw.TriggerTerms))
	}
	trigger, err := buildPattern(raw.TriggerTerms)
	if err != nil {
		return Concept{}, fmt.Errorf("концепт %q: %w", name, err)
	}
	if len(raw.Signals) < minSignalGroups {
		return Concept{}, fmt.Errorf("концепт %q: нужны минимум %d signal-группы, получено %d", name, minSignalGroups, len(raw.Signals))
	}
	signals := make([]Signal, 0, len(raw.Signals))
	for _, rs := range raw.Signals {
		label := strings.TrimSpace(rs.Label)
		if label == "" {
			return Concept{}, fmt.Errorf("концепт %q: пустой label в signal", name)
		}
		re, err := buildPattern(rs.Terms)
		if err != nil {
			return Concept{}, fmt.Errorf("концепт %q, signal %q: %w", name, label, err)
		}
		signals = append(signals, Signal{Label: label, Re: re})
	}
	return Concept{Name: name, Trigger: trigger, Signals: signals}, nil
}

// buildPattern — собирает (?i)term1|term2|... из списка терминов.
// Каждый термин проходит через regexp.QuoteMeta (экранирование спецсимволов)
// и проверку минимальной длины. Пустой результат допустим только при
// пустом входном списке (вызывающий решает, что это ошибка отдельно).
func buildPattern(terms []string) (*regexp.Regexp, error) {
	var quoted []string
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" {
			return nil, errors.New("пустой термин")
		}
		// Считаем руны, а не байты: кириллица занимает 2 байта, но
		// 4 руны — это и «kafka», и «финтех», и «платежи».
		if utf8RuneCount(t) < minTermLen {
			return nil, fmt.Errorf("термин %q короче %d рун", t, minTermLen)
		}
		quoted = append(quoted, regexp.QuoteMeta(t))
	}
	if len(quoted) == 0 {
		return nil, errors.New("нет ни одного валидного термина")
	}
	return regexp.Compile("(?i)" + strings.Join(quoted, "|"))
}

// utf8RuneCount — длина строки в рунах без импорта unicode/utf8
// (он тянет лишний код, тут достаточно счётчика).
func utf8RuneCount(s string) int {
	return len([]rune(s))
}

// LoadConcepts — читает concepts.json, парсит, валидирует, возвращает
// готовые []Concept + сохранённый profile_hash. При любой ошибке
// возвращает nil, "" и err: вызывающий решает, падать или fallback.
func LoadConcepts(path string) ([]Concept, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("чтение %s: %w", path, err)
	}
	var file rawConceptsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, "", fmt.Errorf("битый JSON %s: %w", path, err)
	}
	concepts := make([]Concept, 0, len(file.Concepts))
	for _, rc := range file.Concepts {
		c, err := BuildConcept(rc)
		if err != nil {
			// Бракованный концепт пропускаем: один битый элемент
			// не должен ронять весь файл.
			continue
		}
		concepts = append(concepts, c)
	}
	if len(concepts) == 0 {
		return nil, "", errors.New("ни одного валидного концепта в файле")
	}
	return concepts, file.ProfileHash, nil
}

// ProfileHash — SHA-256 от конкатенации содержимого context/*.md в
// алфавитном порядке (как LoadProfile). Если каталог пуст или не
// существует — возвращает "" без ошибки: тесты server_test.go создают
// TempDir без файлов, и падать тут нельзя.
func ProfileHash(contextDir string) string {
	if contextDir == "" {
		return ""
	}
	entries, err := os.ReadDir(contextDir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(contextDir, name))
		if err != nil {
			continue
		}
		h.Write(raw)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// DefaultConcepts — захардкоженный fallback, идентичный текущему
// набору концептов до рефакторинга. Вызывается при отсутствии
// concepts.json, битом JSON, сбое LLM или таймауте. Семантика
// поведения сохраняется: "Evaluate с DefaultConcepts() == старый Evaluate".
func DefaultConcepts() []Concept {
	// Набор концептов перенесён из старого var concepts (fit.go до
	// рефакторинга). Это намеренный fallback: семантика поведения
	// сохраняется — "Evaluate с DefaultConcepts() == старый Evaluate".
	return []Concept{
		{
			Name:    "распределённые системы",
			Trigger: regexp.MustCompile(`(?i)распредел[её]нн|event-driven|микросервисн|микросервис`),
			Signals: []Signal{
				{Label: "Kafka/брокеры сообщений", Re: regexp.MustCompile(`(?i)\bkafka\b|очеред|брокер`)},
				{Label: "event-driven паттерны", Re: regexp.MustCompile(`(?i)event-driven|consumer group|at-least-once|партици|offset|dead letter`)},
				{Label: "микросервисы/RPC", Re: regexp.MustCompile(`(?i)микросервис|grpc|rpc`)},
			},
		},
		{
			Name:    "высоконагруженные системы",
			Trigger: regexp.MustCompile(`(?i)высоконагруж|нагруженн|критичн|highload|производительн`),
			Signals: []Signal{
				{Label: "нагрузочные метрики RPS/QPS", Re: regexp.MustCompile(`(?i)\brps\b|\bqps\b|highload|запросов в секунду`)},
				{Label: "латентность P99/P95", Re: regexp.MustCompile(`(?i)\bp99\b|\bp95\b|latency|эл/с`)},
				{Label: "идемпотентность/лимиты", Re: regexp.MustCompile(`(?i)идемпотент|rate limit|нагрузочн`)},
			},
		},
		{
			Name:    "эксплуатация и observability",
			Trigger: regexp.MustCompile(`(?i)эксплуатац|мониторинг|отказоустойч|деградац|observability|наблюдае|алерт|инцидент`),
			Signals: []Signal{
				{Label: "Prometheus/Grafana", Re: regexp.MustCompile(`(?i)prometheus|grafana|метрик|монитор`)},
				{Label: "алертинг по SLO", Re: regexp.MustCompile(`(?i)алерт|p99|p95|error rate|дашборд`)},
				{Label: "SQL-Top/профайлинг", Re: regexp.MustCompile(`(?i)sql.?top|профайлер|pg_stat|explain`)},
			},
		},
		{
			Name:    "архитектурное управление",
			Trigger: regexp.MustCompile(`(?i)архитектурн|архитектор|техническое ревью|code review|прототип`),
			Signals: []Signal{
				{Label: "ADR", Re: regexp.MustCompile(`(?i)\badr\b|архитектурн`)},
				{Label: "архитектурные стили/ревью", Re: regexp.MustCompile(`(?i)hexagonal|ddd|ревью|review`)},
				{Label: "прототипы/стандарты", Re: regexp.MustCompile(`(?i)прототип|стандарт|рефакторин`)},
			},
		},
		{
			Name:    "модернизация legacy",
			Trigger: regexp.MustCompile(`(?i)модернизац|legacy|наследи`),
			Signals: []Signal{
				{Label: "миграции", Re: regexp.MustCompile(`(?i)миграц|legacy`)},
				{Label: "модернизация без даунтайма", Re: regexp.MustCompile(`(?i)модернизац|рефакторин|даунтайм`)},
			},
		},
		{
			Name:    "многопоточность и жизненный цикл",
			Trigger: regexp.MustCompile(`(?i)многопоточн|мультипоточн|межпроцесс|межпоточн|диспетчеризац|синхронизац|параллельн|конкурентн|жизненн.{0,12}цикл|goroutine`),
			Signals: []Signal{
				{Label: "горутины/каналы", Re: regexp.MustCompile(`(?i)горутин|канал|goroutine|channel|воркер|worker`)},
				{Label: "lock-free/синхронизация", Re: regexp.MustCompile(`(?i)lock-free|lockfree|atomic|mutex|мьютекс|блокировк|синхронизац`)},
				{Label: "диспетчеризация/жизненный цикл", Re: regexp.MustCompile(`(?i)processmanager|process manager|диспетчериз|graceful|пул|pool|shutdown`)},
			},
		},
		{
			Name:    "ООП/SOLID/паттерны",
			Trigger: regexp.MustCompile(`(?i)ооп|solid|паттерн|проектирова.{0,15}шаблон|принципы`),
			Signals: []Signal{
				{Label: "SOLID/GRASP", Re: regexp.MustCompile(`(?i)solid|grasp|ооп|объектно-ориент`)},
				{Label: "паттерны/архитектурные стили", Re: regexp.MustCompile(`(?i)паттерн|шаблон|hexagonal|ddd|strategy|слой|layer`)},
			},
		},
		{
			Name:    "алгоритмы и структуры данных",
			Trigger: regexp.MustCompile(`(?i)алгоритм|структур.{0,15}данн`),
			Signals: []Signal{
				{Label: "алгоритмы/данные в проектах", Re: regexp.MustCompile(`(?i)очеред|приоритет|индекс|классификац|алгоритм|дерев|кэш|хеш`)},
				{Label: "нагрузочная практика", Re: regexp.MustCompile(`(?i)rps|p99|p95|throughput|эл/с`)},
			},
		},
		{
			Name:    "backend-разработка",
			Trigger: regexp.MustCompile(`(?i)backend|бэкенд|бекенд|серверн`),
			Signals: []Signal{
				{Label: "серверные языки", Re: regexp.MustCompile(`(?i)\bgo\b|\bgolang\b|php|python|java`)},
				{Label: "API/сервисы", Re: regexp.MustCompile(`(?i)api|grpc|http|сервис`)},
			},
		},
		{
			Name:    "гарантии консистентности и идемпотентности",
			Trigger: regexp.MustCompile(`(?i)консистентн|идемпотентн|целостн|гарантии доставки|exactly-once`),
			Signals: []Signal{
				{Label: "at-least-once/досылка", Re: regexp.MustCompile(`(?i)at-least-once|at least once|досылк|буфериз|retry|повторн`)},
				{Label: "идемпотентность/Upsert", Re: regexp.MustCompile(`(?i)идемпотент|idempotency|upsert`)},
				{Label: "транзакции/fail-closed", Re: regexp.MustCompile(`(?i)транзакц|fail-closed|exactly-once`)},
			},
		},
		{
			Name:    "интеграция с платёжными процессингами",
			Trigger: regexp.MustCompile(`(?i)платёжн|процессинг|эквайринг|payment|acquiring`),
			Signals: []Signal{
				{Label: "финтех/банкинг", Re: regexp.MustCompile(`(?i)финтех|fintech|банкинг|банковск|эквайринг`)},
				{Label: "платежи/payment", Re: regexp.MustCompile(`(?i)платёж|платеж|payment|биллинг|billing`)},
				{Label: "процессинг/шлюзы", Re: regexp.MustCompile(`(?i)процессинг|шлюз|gateway|webhook|acquiring|эквайринг`)},
			},
		},
		{
			Name:    "тестирование и качество кода",
			Trigger: regexp.MustCompile(`(?i)тестирован|покрытие|модульн.{0,15}тест|\bunit\b|test|тест`),
			Signals: []Signal{
				{Label: "тесты в проектах", Re: regexp.MustCompile(`(?i)test|тест|unit|e2e|phpunit|покрытие`)},
				{Label: "TDD/методологии", Re: regexp.MustCompile(`(?i)\btdd\b|red.?green|модульн|интеграц|тестован`)},
				{Label: "тестовая инфраструктура", Re: regexp.MustCompile(`(?i)dockertest|table.?driven|fuzz|race|покрытие`)},
			},
		},
		{
			Name:    "системная интеграция и API",
			Trigger: regexp.MustCompile(`(?i)системн.{0,20}интеграц|интеграц|межсервис|взаимодейств.{0,15}сервис`),
			Signals: []Signal{
				{Label: "REST/gRPC API", Re: regexp.MustCompile(`(?i)\brest\b|\bgrpc\b|\bapi\b|endpoint|http`)},
				{Label: "event-driven/брокеры", Re: regexp.MustCompile(`(?i)event-driven|kafka|очеред|брокер|consumer group|webhook`)},
				{Label: "микросервисы/интеграции", Re: regexp.MustCompile(`(?i)микросервис|сервисн.{0,15}архитектур|интеграц`)},
			},
		},
		{
			Name:    "процессы разработки и Agile",
			Trigger: regexp.MustCompile(`(?i)\bagile\b|scrum|kanban|спринт|гибк.{0,15}методолог`),
			Signals: []Signal{
				{Label: "Scrum/Kanban", Re: regexp.MustCompile(`(?i)scrum|kanban|скрам|канбан`)},
				{Label: "ритуалы спринта", Re: regexp.MustCompile(`(?i)спринт|стендап|stand-?up|ретро|retrospect|груминг|grooming|планировани`)},
				{Label: "оценка и бэклог", Re: regexp.MustCompile(`(?i)story ?point|бэклог|backlog|оценк|estimation|user story`)},
			},
		},
		{
			Name:    "реляционные БД и SQL",
			Trigger: regexp.MustCompile(`(?i)реляционн|база данных|баз данных|\bбд\b|\bsql\b|sql-запрос|sql запрос|эффективн.{0,15}запрос`),
			Signals: []Signal{
				{Label: "СУБД", Re: regexp.MustCompile(`(?i)postgres|postgresql|mysql|mariadb|oracle|sqlite`)},
				{Label: "оптимизация SQL", Re: regexp.MustCompile(`(?i)pg_stat|stat.?statements|explain|execution plan|индекс|b-tree|gin|covering|shared_buffers|work_mem|autovacuum|sql.?top|профайлер sql`)},
			},
		},
	}
}

// SaveConceptsFile — сохраняет концепты + хэш профиля в файл.
// Используется сервером после успешной GenerateConcepts: roundtrip
// Save → Load должен возвращать идентичный []Concept, иначе кэш
// бесполезен (P0: теряются trigger_terms и signals).
//
// Принимает raw-структуру с терминами, как их вернула модель. Декомпилировать
// скомпилированный *regexp.Regexp обратно в термины нельзя — это и был
// источник бага: предыдущая версия SaveConceptsFile заполняла только Name,
// и LoadConcepts отбраковывал всё при чтении.
func SaveConceptsFile(path string, raw *rawConceptsFile, profileHash string) error {
	if raw == nil {
		return errors.New("пустой raw — нечего сохранять")
	}
	raw.ProfileHash = profileHash
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// conceptsGenerateSystem — системный промпт для генерации концептов.
// Модель должна вернуть JSON-массив терминов, не regex: regexp.Compile
// делает сам код через buildPattern. Это снимает с модели ответственность
// за корректный Go-regex-синтаксис.
const conceptsGenerateSystem = `Ты — аудитор профиля кандидата. Извлеки из текста ниже навыки и домены, которые:
1. Не описываются одной конкретной технологией (нет точного названия вроде Kafka, PostgreSQL, Redis, Docker).
2. Проявляются через 2+ связанных термина в профиле (например, «платежи», «биллинг», «эквайринг» → концепт «платёжные процессинги»).

Для каждого создай объект JSON:
- name: человекочитаемое название концепта (до 60 символов)
- trigger_terms: массив из 3-7 слов/основ (без окончаний, например «платёжн» вместо «платёжный»), присутствие любого из которых в требовании вакансии говорит об этом концепте
- signals: массив из 2+ групп. Каждая группа: {label: "короткое имя", terms: ["слово1", "слово2", ...]} — термины из профиля кандидата, подтверждающие навык.

Правила:
- НЕ включай концепты, закрываемые одним латинским токеном (Kafka, Kubernetes) — для них работает токен-матчинг.
- НЕ придумывай термины, которых нет в профиле. Каждый term должен быть извлечён из текста.
- Не более 15 концептов.
- Верни ТОЛЬКО JSON вида {"concepts": [...]}.`

// GenerateConcepts — один LLM-вызов: модель читает профиль и возвращает
// JSON с терминами. Код валидирует (длина ≥ 4 руны, минимум 2 группы)
// и компилирует regex. При ошибке парсинга или валидации возвращает
// ошибку: вызывающий решает fallback.
//
// Возвращает пару (concepts, raw): готовые []Concept с скомпилированными
// regex для матчера и сырой rawConceptsFile для roundtrip-сохранения в
// кэш. Декомпилировать regex обратно в термины нельзя, поэтому
// сохраняем именно raw — он содержит термины, как их вернула модель.
func GenerateConcepts(ctx context.Context, fn LLMFunc, profile string) ([]Concept, *rawConceptsFile, error) {
	if fn == nil {
		return nil, nil, errors.New("нет LLM-функции для генерации концептов")
	}
	if profile == "" {
		return nil, nil, errors.New("пустой профиль")
	}
	raw, err := fn(ctx, conceptsGenerateSystem, profile)
	if err != nil {
		return nil, nil, fmt.Errorf("LLM-вызов: %w", err)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, nil, errors.New("в ответе модели нет JSON")
	}
	var file rawConceptsFile
	if err := json.Unmarshal([]byte(raw[start:end+1]), &file); err != nil {
		return nil, nil, fmt.Errorf("битый JSON: %w", err)
	}
	concepts := make([]Concept, 0, len(file.Concepts))
	for _, rc := range file.Concepts {
		c, err := BuildConcept(rc)
		if err != nil {
			// Бракованный концепт пропускаем, не роняем всю генерацию.
			// Из raw его тоже убираем: иначе кэш сохранит битый элемент,
			// при следующей загрузке BuildConcept опять его отбросит,
			// и поведение расходится с тем, что вернула модель.
			continue
		}
		concepts = append(concepts, c)
	}
	if len(concepts) == 0 {
		return nil, nil, errors.New("ни одного валидного концепта от модели")
	}
	// Возвращаем только валидные raw-записи, в том же порядке, что и concepts.
	validRaw := rawConceptsFile{Concepts: make([]RawConcept, 0, len(concepts))}
	for _, c := range concepts {
		for _, rc := range file.Concepts {
			if rc.Name == c.Name {
				validRaw.Concepts = append(validRaw.Concepts, rc)
				break
			}
		}
	}
	return concepts, &validRaw, nil
}

// saveConceptsFromRaw удалён: теперь SaveConceptsFile сам принимает *rawConceptsFile,
// а вся roundtrip-логика живёт в GenerateConcepts + SaveConceptsFile.
