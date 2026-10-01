// Package fit — рекомендация отклика: стоит ли кандидату тратить время
// на эту вакансию. Двухступенчатый, по той же философии, что и audit:
// промпт не лечит — лечит код.
//
// Шаг 1 (LLM): вакансия разбирается на структуру требований
// (must-have / nice-to-have / мягкие, тип роли) — свободный текст,
// который регэкспами не берётся. ExtractRequirements в fit/extract.go.
// Шаг 2 (детерминированный): Evaluate сопоставляет требования с письмом
// и профилем, считает покрытие и выносит вердикт. Чистая функция —
// воспроизводима и тестируема юнит-тестами.
//
// Вердикт — три состояния, не два: бинарное «откликаться/нет» бесполезно,
// большинство вакансий в серой зоне. Скор — «соответствие, %» (взвешенное
// покрытие must-have), а не «вероятность»: калибровать вероятность не на
// чем, а ложная уверенность опаснее отсутствия вердикта.
package fit

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/turkprogrammer/covercraft/internal/cover"
	"github.com/turkprogrammer/covercraft/internal/prompt"
)

// Вердикты Fit.Verdict.
const (
	Apply   = "apply"              // все must-have закрыты или закрываются мостом
	Caveats = "apply_with_caveats" // 1–2 незакрытых, но есть мост / одно «неизвестно»
	Skip    = "skip"               // незакрытый must-have без моста, несколько, или роль другого профиля
)

// Статусы покрытия требования.
const (
	SrcLetter  = "letter"  // факт есть в письме (высшая достоверность)
	SrcProfile = "profile" // факт есть в профиле, но не попал в письмо
	SrcBridge  = "bridge"  // нет факта, но есть мост на соседний опыт
	SrcUnknown = "unknown" // матчинг не нашёл ничего — это «нет данных», не «нет опыта»
	SrcMissing = "missing" // факт отсутствует и моста нет
)

// Requirement — одно требование из разбора вакансии (LLM-шаг).
type Requirement struct {
	Text     string `json:"text"`     // формулировка из вакансии
	Kind     string `json:"kind"`     // "must" | "nice" | "soft"
	Category string `json:"category"` // stack | domain | metric | role | other
}

// Requirements — структура вакансии после разбора.
type Requirements struct {
	Role       string        `json:"role"` // "go-primary", "php-primary", "ml-research", …
	MustHave   []Requirement `json:"mustHave"`
	NiceToHave []Requirement `json:"niceToHave"`
	Soft       []Requirement `json:"soft"`
}

// Req — требование с результатом матчинга (для UI-расшифровки).
type Req struct {
	Text   string `json:"text"`
	Source string `json:"source"` // Src* константа
	Note   string `json:"note"`   // чем закрыто (проект/мост) или что делать
	// Kind — "must" | "duty" | "nice" | "soft". «duty» — обязанность из
	// блока обязанностей: пробел по ней виден, но вердикт до skip не
	// роняет (обязанность ≠ обязательное требование).
	Kind string `json:"kind,omitempty"`
}

// Fit — вердикт рекомендации. Score и Verdict считаются из одной
// таблицы покрытия и не могут противоречить друг другу.
type Fit struct {
	Verdict string   `json:"verdict"`
	Score   int      `json:"score"` // 0–100, взвешенное покрытие must-have
	Role    string   `json:"role,omitempty"`
	Covered []Req    `json:"covered,omitempty"` // закрыто (письмо/профиль)
	Caveats []Req    `json:"caveats,omitempty"` // с оговоркой (мосты, unknown)
	Missing []Req    `json:"missing,omitempty"` // не закрыто вовсе
	Advice  []string `json:"advice,omitempty"`
}

// FitFixableCaveats возвращает подмножество требований Fit, которые LLM может
// исправить добавлением факта в письмо. Ищет маркеры «впиши в письмо» и
// «не упомянуты» в Caveats и Covered — именно в этих полях генератор нот
// ставит маркер, когда факт есть в профиле, но не попал в письмо.
// Soft-требования (check manually) и честные пробелы отсекаются.
func FitFixableCaveats(fit Fit) []Req {
	var fixable []Req
	// Cover both caveats и covered — маркер «впиши в письмо» попадает в
	// обе корзины в зависимости от пути матчинга (bridge vs profile).
	for _, c := range fit.Caveats {
		if strings.Contains(c.Note, "впиши") || strings.Contains(c.Note, "не упомянуты") {
			fixable = append(fixable, c)
		}
	}
	for _, c := range fit.Covered {
		if strings.Contains(c.Note, "впиши") || strings.Contains(c.Note, "не упомянуты") {
			// Не дублируем, если уже добавили из caveats
			dup := false
			for _, fc := range fixable {
				if fc.Text == c.Text && fc.Source == c.Source {
					dup = true
					break
				}
			}
			if !dup {
				fixable = append(fixable, c)
			}
		}
	}
	return fixable
}

// synonyms — альтернативные написания технологий: требование может
// назвать технологию сокращением, профиль — полным именем.
var synonyms = map[string][]string{
	"golang":          {"go", "golang"},
	"k8s":             {"kubernetes", "k8s"},
	"js":              {"javascript", "typescript", "js"},
	"postgres":        {"postgresql", "postgres"},
	"victoriametrics": {"victoriametrics", "vm"},
	"argocd":          {"argocd", "argo cd", "argo-cd"},
	"1c":              {"1с", "1c", "битрикс"},
	"kafka":           {"kafka", "logbroker"}, // Logbroker — «Kafka-like» event bus (формулировка вакансий)
	// Безопасность/аутентификация: требования «JWT, 2FA/TOTP, RBAC»,
	// «PII masking», «idempotency keys» — токены, которых нет в профиле,
	// но факты (JWT HS256/RS256, TOTP, PII masking 152-ФЗ, idempotency
	// через ClickHouse Upsert) закрывают. Кириллические альты —
	// подстраховка на русские словоформы.
	"jwt":         {"jwt", "токен", "token"},
	"totp":        {"totp", "2fa", "two-factor", "многофакторн"},
	"2fa":         {"2fa", "totp", "two-factor", "многофакторн"},
	"rbac":        {"rbac", "роли", "role", "authorization"},
	"pii":         {"pii", "маскирован", "152-фз", "персональн"},
	"idempotency": {"idempotency", "идемпотентн", "идемпотент"},
	"secure":      {"secure", "безопасн", "security"},
	"security":    {"security", "безопасн"},
	// «rate limits» в требовании → «rate limiting» в письме: разные словоформы
	// одного и того же опыта, токен-матчинг без синонима промахивается.
	"limits":  {"limit", "limiting", "rate limit", "rate limiting", "rate-limit", "троттлинг"},
	"retries": {"retry", "retries", "retrial"},
	// Мосты рус↔англ для абстрактных терминов: вакансия на английском,
	// письмо/профиль на русском — токен-матчинг без кириллических альтов
	// промахивается на «query optimization», «tests», «documentation» и т.д.
	"query":        {"query", "запрос"},
	"queries":      {"query", "queries", "запрос"},
	"optimization": {"optimization", "оптимизац"},
	"optimized":    {"optimization", "optimized", "оптимизац"},
	"migrations":   {"migrations", "миграци"},
	"migrate":      {"migrations", "migrate", "миграци"},
	"tests":        {"test", "tests", "тесты", "тест"},
	"test":         {"test", "tests", "тесты", "тест"},
	// «PHPUnit testing» — токен «testing» закрывается альт-списком
	// «test/tests/тест»: кандидат пишет тесты на Go/PHP — это то же самое.
	"testing": {"testing", "test", "tests", "тест"},
	// rest/grpc НЕ в альт-списке api: «Опыт разработки REST API» — два
	// независимых токена (rest и api), тест TestRestAPITokenMatch на это
	// полагается; кириллический альт api — подстраховка на «апи» (редко).
	"api": {"api", "апи"},
	// «Git & GitHub - branching strategies, PR workflows, conflict
	// resolution» — требования к git-практикам: факты «Git», «GitHub»,
	// «PR» в письме/профиле закрывают даже без слов «branching»,
	// «strategies», «conflict», «workflows», «resolution» (в вакансии они
	// — нарратив, а не отдельный навык). Для 8-токенового требования
	// found=3 (git, github, pr) < 50% → до фикса это было missing.
	"git":        {"git", "github"},
	"github":     {"github", "git"},
	"workflows":  {"workflows", "git flow", "ветк"},
	"branching":  {"branching", "ветв", "ветк", "git flow"},
	"strategies": {"strategies", "стратеги", "подход", "ветв", "ветк"},
	"conflict":   {"conflict", "конфликт"},
	"resolution": {"resolution", "разрешен", "решен"},
	// «Experience writing and maintaining technical documentation — API
	// docs, architecture overviews» — нарративные слова «writing»,
	// «maintaining», «technical», «overviews» не технологии. Факты
	// «ADR», «API docs», «architecture» в письме закрывают.
	"writing":       {"writing", "написан", "создан", "разработк"},
	"written":       {"writing", "written", "написан", "создан"},
	"maintaining":   {"maintaining", "обновля", "содержан", "оперир"},
	"documentation": {"documentation", "документаци", "документир", "docs"},
	"technical":     {"technical", "техн"},
	"overviews":     {"overviews", "обзор", "схем"},
	"docs":          {"docs", "documentation", "документаци", "документир"},
	// «PHPUnit - you've written tests» — «phpunit» без прямых фактов
	// закрывается по альт-списку «тест/unit» (если кандидат пишет тесты
	// на Go/PHP — это то же самое).
	"phpunit": {"phpunit", "тест", "tests", "unit"},
	// «Strong understanding of code review culture» — нарративные слова
	// «understanding», «strong», «culture» не навыки. Факты «code review»,
	// «ADR» в письме/профиле закрывают.
	"understanding": {"understanding", "понимани", "понимаю", "понятн"},
	"strong":        {"strong", "сильн", "професс", "продвинут", "уровень"},
	"culture":       {"culture", "культур", "практик", "ревью", "review", "code review"},
	// «Structured approach to writing tests» — «structured», «approach»
	// — нарратив; факты «тесты», «table-driven» закрывают.
	"structured": {"structured", "структурир", "системн", "системат"},
	"approach":   {"approach", "подход", "медиц", "метод", "style"},
	// «PHPUnit: experience writing and believing in tests» — «believing»
	// — нарратив; факты «PHPUnit», «тесты», «TDD» закрывают.
	"believing": {"believing", "believe", "уверенн", "довер"},
	// «Strong Go in production high-load systems» — «high-load»,
	// «systems» — классы систем; факты «RPS», «Kafka», «P99» закрывают.
	"high-load": {"high-load", "highload", "высоконагр", "нагрузк"},
	"systems":   {"systems", "систем", "сервис", "service"},
	// «Working with relational and NoSQL databases» — «relational»,
	// «nosql», «databases» — классы БД; факты «PostgreSQL», «ClickHouse»
	// закрывают.
	"relational": {"relational", "реляционн", "база"},
	// «nosql» — сам термин + кириллический эквивалент. НЕ включаем
	// имена СУБД (clickhouse/redis/mongodb/elasticsearch): они имеют
	// собственные мосты/синонимы, и широкое включение в «nosql»
	// коротит мост TestEvaluateBridge («Опыт с Elasticsearch» должен
	// закрываться мостом через ClickHouse, а не напрямую).
	"nosql": {"nosql", "no-sql", "нереляционн"},
	// «databases» — термин + кириллический. Не «data» (слишком широко,
	// ловит «data pipeline», «data science»).
	"databases": {"databases", "бд", "база данных", "хранилищ"},
	// «Strong Go in production high-load systems» — «production»,
	// «high», «load» — эпитеты; факты «RPS», «P99», «Kafka» закрывают.
	"production": {"production", "прод", "product"},
	"high":       {"high", "highload", "высоконагр"},
	"load":       {"load", "highload", "нагрузк"},
	// «Solid working experience with vanilla PHP» — «vanilla», «pure»,
	// «frameworkless» — одно и то же; факт «pure PHP» / «no framework»
	// в письме закрывает.
	"vanilla": {"vanilla", "чист", "pure", "frameworkless", "без фреймворк"},
	// «Professional working level English» / «Russian minimum A1» —
	// требования к языку общения; альты ловят прямые маркеры.
	"local":        {"local", "локал"},
	"staging":      {"staging", "прод", "staging"},
	"environments": {"environments", "среда", "сред"},
	"development":  {"development", "разработк", "develop"},
	"professional": {"professional", "професс", "коммерч", "production"},
	"working":      {"working", "работ", "work"},
	"level":        {"level", "уровн"},
	"english":      {"english", "английск"},
	"russian":      {"russian", "русск"},
	"mixed":        {"mixed", "смешанн", "разноо"},
	"framework":    {"framework", "фреймворк", "laravel", "symfony", "lumen"},
	"pure":         {"pure", "чист", "vanilla", "frameworkless"},
	"experience":   {"experience", "опыт", "лет"},
	"years":        {"years", "лет", "стаж"},
	// Yii2: фреймворк пишется как «yii2filmcatalog», «yii2shop» — одно
	// слово, \byii\b не матчится. Нормализуем токен «yii» до полного
	// названия проекта, чтобы findText нашёл его по границам слова.
	"yii2filmcatalog": {"yii2filmcatalog", "yii2", "yii", "yiiframework"},
	"yii2shop":        {"yii2shop", "yii2", "yii", "yiiframework"},
	"yii2gallery":     {"yii2gallery", "yii2", "yii", "yiiframework"},
	// Ubuntu — разновидность Linux: токен «ubuntu» нормализуется до
	// «linux», а «linux» уже есть в письме/профиле.
	"ubuntu": {"ubuntu", "linux"},
	// AI-native / coding agents: требования «Cursor, Claude Code, agentic
	// tooling, делегирование задач» — письмо пишет «ИИ-инструменты»,
	// «постановка задач», «архитектурный контроль». Токен tokenRe не
	// извлекает кириллицу, поэтому без альтов fit не видит покрытия.
	"cursor":       {"cursor", "ии-инструмент", "ai-native"},
	"claude":       {"claude code", "claude", "ии-инструмент"},
	"codex":        {"codex", "ии-инструмент", "ai-native"},
	"ai-generated": {"ai-generated", "ai-code", "ии-код"},
	"agentic":      {"agentic", "агентн", "agent harness"},
	"delegat":      {"делегирова", "постановка задач", "delegat"},
	"ai-native":    {"ai-native", "ии-инструмент", "ai native"},
}

// bridge — мост: требование без прямого факта, но с соседним опытом
// в профиле (справочник requirement-mapping из скилла). Kubernetes здесь
// НЕТ намеренно: must-have «K8s в проде» без опыта — реальный риск
// отклика, мост на «docker» его не закрывает.
type bridge struct {
	re   *regexp.Regexp
	note string
}

// bridges — таблица мостов.
var bridges = map[string]bridge{
	"victoriametrics": {regexp.MustCompile(`(?i)prometheus|монитор|метрик`), "мост: опыт мониторинга метрик → VictoriaMetrics"},
	"argocd":          {regexp.MustCompile(`(?i)ci/cd|депло|автоматизац`), "мост: опыт деплоя/CI-CD → Argo CD"},
	"elasticsearch":   {regexp.MustCompile(`(?i)clickhouse|поиск|индекс|логи`), "мост: опыт поисковых индексов → Elasticsearch"},
	"rabbitmq":        {regexp.MustCompile(`(?i)kafka|очеред|брокер`), "мост: опыт брокеров сообщений → RabbitMQ"},
	"rag":             {regexp.MustCompile(`(?i)классификац|машинн|ml|модел`), "мост: ML-опыт → RAG"},
	"outbox":          {regexp.MustCompile(`(?i)at-least-once|идемпотентн|буферизац|polling|событийн.{0,20}журнал|журнал.{0,20}событ`), "мост: событийный журнал в БД с polling-потребителями (geolocation.alerts), буферизованный продюсер и идемпотентный Upsert → transactional outbox (без атомарности с транзакцией PG и брокерной доставки)"},
	"rfc":             {regexp.MustCompile(`(?i)\badr\b|архитектурн.{0,20}решени|design[ _-]?doc|дизайн-документ|документаци|decision record`), "мост: архитектурные решения (ADR, документация в репозиториях) → RFC и дизайн-документы"},
	"observability":   {regexp.MustCompile(`(?i)prometheus|grafana|мониторинг|трейсин|трейс`), "мост: опыт мониторинга метрик и дашбордов → observability"},
	// HTTP — транспорт, а не отдельная технология в стеке. Кандидат пишет
	// «gRPC» и «laravel-api», но слово «HTTP» в письме не употребляет, и
	// требование «Опыт работы с HTTP» уходило в missing. Мост отражает
	// фактическую связь: gRPC (HTTP/2) и REST API работают поверх HTTP.
	// Живой кейс АФЛТ (сентябрь 2026).
	"http": {regexp.MustCompile(`(?i)\bgrpc\b|\brest\b|api|веб-сервис|веб\s+приложен|http|сетев`),
		"мост: gRPC и REST API работают поверх HTTP — транспорт закрыт опытом веб-сервисов"},
}

// softTerms — мягкие требования: не факты и не пробелы. Фит их не
// считает незакрытыми.
var softTerms = regexp.MustCompile(`(?i)самоорганиз|темп|стрессоустойч|командн|коммуника|внимательн|ответственн|инициативн`)

// conceptHonestGap — концептное требование честно названо пробелом: тема
// требования (её regex) упомянута в письме, но только в клаузах под
// отрицанием («С платёжными процессингами не работал»). Это не «нет данных»,
// а раскрытый кандидатом пробел — вердикт не должен наказывать честность
// сильнее, чем молчание.
func conceptHonestGap(concepts []Concept, reqText, letter string) (string, bool) {
	clauses := sentences(letter)
	for _, c := range concepts {
		if !c.Trigger.MatchString(strings.ToLower(reqText)) {
			continue
		}
		negated := false
		for i, cl := range clauses {
			if !c.Trigger.MatchString(strings.ToLower(cl)) {
				continue
			}
			if clauseNegated(clauses, i) {
				negated = true
				continue
			}
			// Тема упомянута позитивно — это не честный пробел.
			return "", false
		}
		if negated {
			return "в письме честно назван пробел («" + c.Name + "») — для вердикта это «нет данных»", true
		}
	}
	return "", false
}

// hasCyrillic — есть ли в строке кириллица (для способа поиска альтов).
func hasCyrillic(s string) bool {
	for _, r := range s {
		if r >= 'а' && r <= 'я' || r >= 'А' && r <= 'Я' || r == 'ё' || r == 'Ё' {
			return true
		}
	}
	return false
}

// conceptHit — требование говорит о концепте, и текст проявляет его
// достаточным числом сигналов. Возвращает имя концепта и метки
// сработавших сигналов. Сигналы, найденные в клаузах под отрицанием,
// не засчитываются: «С платёжными процессингами не работал» не закрывает
// «опыт интеграции с платёжными процессингами».
func conceptHit(concepts []Concept, reqText, text string, minSignals int) (name string, hits []string) {
	clauses := sentences(text)
	for _, c := range concepts {
		if !c.Trigger.MatchString(strings.ToLower(reqText)) {
			continue
		}
		hits = nil
		for _, s := range c.Signals {
			// Ищем сигнал в клаузах: засчитываем, только если он не под отрицанием.
			for i, cl := range clauses {
				if s.Re.MatchString(strings.ToLower(cl)) && !clauseNegated(clauses, i) {
					hits = append(hits, s.Label)
					break // один хит на сигнал достаточно
				}
			}
		}
		if len(hits) >= minSignals {
			return c.Name, hits
		}
	}
	return "", nil
}

// plusRe — «будет плюсом»: такое требование не требует покрытия.
var plusRe = regexp.MustCompile(`(?i)будет плюсом|желательно|nice to have`)

// tokenRe — латинские токены (названия технологий) в тексте требования.
var tokenRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+#.-]{1,30}`)

// stopwords — общеупотребительные слова, не технологии.
// Делится на две группы: (1) универсальные частицы/предлоги; (2)
// нарративные слова англоязычных требований («written tests»,
// «branching strategies», «conflict resolution», «architecture overviews»).
// Группа 2 критична для majority-счёта: «Git & GitHub - branching
// strategies, PR workflows, conflict resolution» — 8 токенов, из них
// «branching», «strategies», «workflows», «conflict», «resolution» —
// нарратив, а не отдельные навыки. Без stopword'ов found=3 < 50% →
// ложный missing. Русские вакансии не страдают: кириллические слова
// не извлекаются tokenRe, и эти стоп-слова для них неактивны.
var stopwords = map[string]bool{
	"the": true, "and": true, "or": true, "of": true, "in": true,
	"at": true, "to": true, "with": true, "for": true, "a": true,
	"senior": true, "middle": true, "junior": true, "lead": true,
	"years": true, "year": true, "experience": true, "work": true,
	"com": true, "https": true, "www": true, "web": true,
	// «http» и «web» вынесены из стоп-слов. Стоп-слово — слово, которое НЕ
	// является технологией никогда («years», «senior», «trade-offs»).
	// «web» — существительное о типе приложения, а не технология. Живой
	// кейс АФЛТ (сентябрь 2026): требование «Разработка производительных
	// сервисов: API для web-приложений, интеграционных и служебных модулей»
	// давало tokens=[api, web]; web в письме нет, found=1 < 2 (порог
	// majority), а правило 50% требует len(tokens) >= 6 — требование уходило
	// в missing, и ОДИН такой ложный пробел ронял вердикт в skip при
	// полностью закрытых остальных must-have.
	//
	// «http» — наоборот, технология, которую кандидат закрывает опытом
	// gRPC/REST API, но не пишет словом «HTTP». Со стоп-словом требование
	// «Опыт работы с HTTP» уходило в ветку len(tokens)==0 и давало unknown
	// «нет данных» — а это ложь: транспорт у кандидата есть. Теперь это
	// мост (bridges["http"]) — честная оговорка, а не пробел.
	// Функциональный шум англоязычных требований: «you've written tests
	// and you believe in them, not just on your CV» — «written tests
	// believe them just cv» не технологии, а нарратив. В reqTokens
	// отбрасываются, чтобы не раздувать majority-порог.
	"you": true, "your": true, "yourself": true, "ve": true,
	"just": true, "not": true, "only": true, "also": true, "can": true,
	"must": true, "should": true, "will": true, "able": true, "level": true,
	"minimum": true, "maximum": true, "basic": true, "strong": true, "good": true,
	"believe": true, "them": true, "him": true, "it": true, "its": true, "us": true, "we": true,
	"cv": true, "resume": true, "candidate": true, "candidates": true,
	// "on" — нарративный предлог («not just on your CV»), не технология.
	"on": true,
	// «2FA» — tokenRe не матчит «2fa» (начинается с цифры), но
	// вырезает хвост «fa». «fa» — не токен, стоп-слово; «TOTP» и
	// «2fa» (из synonyms «totp») закрывают требование.
	"fa": true,
	// «float» и «precision» — не технологии в контексте «никакого float
	// для денег, понимание precision»: кандидат НЕ использует float.
	// Как токены они ложно не закрываются (в письме под отрицанием
	// «float для денег не применяю»). Отбрасываем — токенный путь не
	// работает, идёт concept-путь («точная денежная арифметика»).
	"float": true, "precision": true,
	// «b2c»/«b2b» — маркеры типа продукта, а не технологии. В требовании
	// «fullstack-разработки в реальных B2C/B2B-продуктах» они давали
	// reqTokens=[fullstack b2c b2b], из-за чего концепт-путь (он включается
	// только при малом числе токенов) не срабатывал и требование уходило в
	// «не закрыто ничем», хотя письмо перечисляло продукты с метриками.
	// Живой кейс Fullstack Backend, сентябрь 2026.
	"b2c": true, "b2b": true, "saas": true, "paas": true, "iaas": true,
	// «trade-offs» — термин из требований архитектуры; не технология,
	// поэтому токен «trade-offs» матчится в письме редко (обычно пишут
	// «ADR», «trade-offs analysis»). Отбрасываем как стопворд, чтобы
	// требование шло в концепт-путь («system design и архитектурное
	// проектирование») и закрывалось по ADR/Hexagonal/DDD.
	"trade-offs": true,
	// Нарративные слова git-практик: «branching strategies, PR workflows,
	// conflict resolution» — факты «Git», «GitHub», «PR» закрывают
	// требование даже без этих слов. В рус. вакансиях аналог
	// «ветвление, разрешение конфликтов» не извлекается tokenRe.
	"branching": true, "strategies": true, "workflows": true,
	"conflict": true, "resolution": true, "strategy": true,
	"works": true,
	// Нарративные слова про тесты: «you've written tests and you
	// believe in them» — «written», «writing» — нарратив; факт
	// «PHPUnit»/«unit tests»/«TDD» закрывает требование.
	"written": true, "writing": true, "writes": true, "maintain": true,
	"maintaining": true, "maintains": true, "technical": true,
	// Нарративные слова про документацию: «API docs, architecture
	// overviews» — «overviews», «documentation» без прямых фактов
	// — шум; факт «ADR»/«docs»/«architecture» закрывает.
	"overviews": true, "documentation": true, "docs": true,
	"architecture": true, "architectures": true,
	// Нарративные слова про языки: «Professional working level English»,
	// «Russian — minimum A1» — «professional», «working», «level» —
	// эпитеты; факт «английский»/«русский» в письме/профиле закрывает.
	"professional": true, "working": true,
	// rest/api/sql — НЕ стоп-слова: «Опыт разработки REST API» и
	// «Уверенный SQL» матчатся по токенам (профиль: «REST (JSON)»,
	// «БД и SQL: PostgreSQL»), а не через концепты. sql выведен из
	// стопвордов: в вакансиях он — реальный навык, а не эпитет.
	// rpc/business/critical — операторы и эпитеты, не технологические
	// навыки: токен по ним даёт ложный шум («business critical level»).
	"rpc": true, "business": true, "critical": true,
}

// normToken приводит токен к каноническому имени по таблице синонимов.
// Суффиксы «-like/-based/-style» срезаются: вакансии пишут «Kafka-like»,
// «Go-based» — это тот же стек, а не отдельная технология.
func normToken(tok string) string {
	lt := strings.ToLower(strings.Trim(tok, ".-,#"))
	for _, suf := range []string{"-like", "-based", "-style"} {
		lt = strings.TrimSuffix(lt, suf)
	}
	for canon, alts := range synonyms {
		for _, alt := range alts {
			if lt == alt {
				return canon
			}
		}
	}
	return lt
}

// reqTokens — технологии, названные в требовании.
func reqTokens(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range tokenRe.FindAllString(text, -1) {
		t := normToken(m)
		if t == "" || stopwords[t] || genericTokens[t] || len(t) < 2 {
			continue
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// findText ищет токен в тексте (токен из reqTokens — латиница из
// требования). Альтернативы из synonyms ищем подстрокой, если содержат
// кириллицу (падежи: «оптимизация» / «оптимизации»), или с границами
// слова, если латиница (чтобы «go» не ловился внутри «golang»).
func findText(token, text string) bool {
	lt := strings.ToLower(text)
	// Основной токен — с границами слова (латиница из требования).
	if matchToken(token, lt) {
		return true
	}
	// Альтернативы: кириллица — подстрока, латиница — с границами.
	for _, a := range synonyms[token] {
		if hasCyrillic(a) {
			if strings.Contains(lt, strings.ToLower(a)) {
				return true
			}
			continue
		}
		if matchToken(a, lt) {
			return true
		}
	}
	return false
}

// matchToken — токен найден в тексте с границами слова, во всех написаниях.
func matchToken(tok, lt string) bool {
	for _, v := range tokenVariants(tok) {
		if wordRe(v).MatchString(lt) {
			return true
		}
	}
	return false
}

// tokenVariants — написания одного и того же имени. Дефис, подчёркивание и
// пробел — орфографические варианты одного термина, а не разные термины.
//
// Живой кейс АФЛТ-Системс (октябрь 2026): одно и то же требование
// «Опыт работы с REST-API» закрывалось письмом, где тот же опыт записан как
// «REST API» (через пробел), и уходило в «не закрыто», хотя в профиле десять
// упоминаний «REST API». normToken даёт токен «rest-api», а в тексте письма
// слова «rest» и «api» стоят порознь — подстрочного совпадения не было ни
// одного. Разные прогоны на одних и тех же данных давали противоположные
// вердикты только из-за написания.
func tokenVariants(tok string) []string {
	t := strings.ToLower(strings.TrimSpace(tok))
	if !strings.ContainsAny(t, "-_ ") {
		return []string{t}
	}
	parts := strings.FieldsFunc(t, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	if len(parts) < 2 {
		return []string{t}
	}
	out := make([]string, 0, 4)
	add := func(s string) {
		if s == "" {
			return
		}
		for _, v := range out {
			if v == s {
				return
			}
		}
		out = append(out, s)
	}
	for _, sep := range []string{"-", " ", "_", "/"} {
		add(strings.Join(parts, sep))
	}
	add(strings.Join(parts, ""))
	return out
}

// wordReCache — кэш скомпилированных регулярок поиска токена: findText зовётся
// на каждый токен × каждую клаузу, и компиляция на каждый вызов съедала всё
// время на живых письмах.
var wordReCache sync.Map

// wordReCacheMax — потолок ёмкости кэша. Ключи приходят из
// reqTokens(текст вакансии), то есть из пользовательского ввода, и без
// ограничения процесс растёт всю жизнь.
//
// Замер (октябрь 2026): на обычной вакансии 13 токенов дают 16 записей, и
// повторные прогоны той же вакансии кэш не растят. На синтетической вакансии
// с 4 000 уникальных латинских слов накопилось 4 006 записей, удержание
// HeapAlloc после GC — 9,5 МБ, то есть ~2,4 КБ на запись (скомпилированная
// regexp много тяжелее строки-ключа). При потоке разных вакансий это
// утечка без предела.
//
// 4 096 записей — с запасом выше любой реальной вакансии (сотни токенов) и
// при этом ~10 МБ в худшем случае, что для десктопного сервиса приемлемо.
const wordReCacheMax = 4096

// wordReCacheSize — счётчик записей. sync.Map не отдаёт размер, а без него
// потолок не проверить. Только наши Store, под синхронизацией.
var wordReCacheSize atomic.Int64

func wordRe(v string) *regexp.Regexp {
	if r, ok := wordReCache.Load(v); ok {
		return r.(*regexp.Regexp)
	}
	r := regexp.MustCompile(`(?i)(^|[^a-zа-я0-9])` + regexp.QuoteMeta(v) + `([^a-zа-я0-9]|$)`)
	// Потолок проверяется ДО Store: при переполнении регулярка просто
	// компилируется заново на каждый вызов. Это медленно ровно настолько,
	// насколько вакансия патологична, и зато память ограничена. Для
	// нормальной вакансии кэш всегда попадает внутрь и остаётся быстрым.
	if wordReCacheSize.Load() >= wordReCacheMax {
		return r
	}
	if _, loaded := wordReCache.LoadOrStore(v, r); !loaded {
		wordReCacheSize.Add(1)
	}
	return r
}

// negRe — маркеры отрицания опыта в предложении: письмо, честно
// называющее пробел («С OpenTelemetry опыта нет, готов освоить»), не
// должно считаться закрытием требования — это живой кейс, когда честное
// письмо получало «закрыто в письме» по голой подстроке.
var negRe = regexp.MustCompile(`(?i)опыта нет|нет опыта|опыта\s+(?:\S+\s+){0,3}нет|опыты?[^.!?;]{0,60}нет|нет\s+(?:\S+\s+){0,3}опыта|не работал|не использ|отсутствует|не зафиксирован|не применял|готов освоить|освою|не приходилось|не эксплуатировал|не развёртывал|не развертывал|не внедрял|не интегрировал|не настраивал|не деплоил|не управлял|не администрировал`)

// profileGapRe — маркеры ограничителя-«честного пробела» в профиле.
// Это НЕ отрицание опыта в письме, а разные пометки одного смысла: владелец
// профиля прямо называет технологию пробелом, и строка не должна выдавать
// сигнал опыта ни в источнике фактов, ни в ограничителях.
//
// Живой замер (октябрь 2026): из профиля извлекались ложные факты `render`
// («НЕ писать про Canvas/WebGL-рендер»), `tracing`, `PostgreSQL` («НЕ писать
// про PostgreSQL/ORM»), `SRE` («Стаж по SRE/сетям = честный пробел») —
// ограничители, в которых эти слова названы, чтобы их НЕ заявлять.
//
// «не пробел» сюда НЕ входит намеренно: строка «A/B тестирование — НЕ
// пробел: факт Stable ID — event-driven A/B через Kafka» называет ФАКТ, и
// маркер «пробел» здесь отрицающий.
var profileGapRe = regexp.MustCompile(`(?i)честн\w*\s+пробел|в профиле\s+нет|не писать про|не упоминать`)

// backwardNegRe — маркеры, отрицающие клаузу целиком, включая стоящее до
// них: «Transactional outbox на PostgreSQL: НЕ использовал» — отклоняет
// outbox, хотя тот стоит впереди маркера. «не заявля/не говор» — маркеры
// аннотаций-ограничителей профиля («НЕ заявлять как outbox» context/01:256,
// «НЕ говорить «17+ лет PostgreSQL»» context/01:251). В negRe (маркеры
// отрицания в ПИСЬМЕ) они НЕ добавлены: кандидат так свой опыт не
// описывает, а клаузная логика письма уже отлажена.
var backwardNegRe = regexp.MustCompile(`(?i)опыта нет|нет опыта|опыта\s+(?:\S+\s+){0,3}нет|опыты?[^.!?;]{0,60}нет|нет\s+(?:\S+\s+){0,3}опыта|не работал|не использ|отсутствует|не зафиксирован|не применял|не приходилось|не заявля|не говор|не эксплуатировал|не развёртывал|не развертывал|не внедрял|не интегрировал|не настраивал|не деплоил|не управлял|не администрировал`)

// forwardNegRe — маркеры «готов освоить X»: отрицают только то, что стоит
// ПОСЛЕ них. В профиле мост «bash-автоматизация → готов освоить
// Python/Airflow» называет пробелом Python/Airflow, а Bash-автоматизация
// остаётся положительным якорем — old-логика роняла Bash как declined.
var forwardNegRe = regexp.MustCompile(`(?i)готов освоить|освою`)

// sentences — разбивка текста на клаузы (по .!?\n;,). Область отрицания
// клаузальная, а не «предложенческая»: буллет-пробел сплошь и рядом
// перечисляет через запятую и пробел, и позитив — «OpenTelemetry: опыта
// нет, observability — SQL-Top, Prometheus + Grafana». При области на всё
// предложение позитивный observability попадал под отрицание соседней
// клаузы и требование ронялось в unknown (живой регресс платёжной вакансии).
// При этом «не работал с Kubernetes» в другой клаузе валидный факт про
// Kafka не роняет.
func sentences(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '\n' || r == ';' || r == ','
	})
}

// bareNegRe — клауза, состоящая ТОЛЬКО из маркера отрицания: хвостовая
// форма «OpenTelemetry, опыта нет» — маркер относится к предыдущей клаузе.
// «готов освоить» сюда не входит: «Kafka, готов освоить» встречается и
// после позитивного факта, и отрицанием его считать нельзя.
var bareNegRe = regexp.MustCompile(`(?i)^\s*(опыта нет|нет опыта|опыта\s+(?:\S+\s+){0,3}нет|опыты?[^.!?;]{0,60}нет|нет\s+(?:\S+\s+){0,3}опыта|не работал[а-яё]*|не использ\w*|отсутствует|не зафиксирован|не применял|не приходилось)\s*[.!]?\s*$`)

// nextIsDeclaration — сразу за текущей клаузой, в том же буллете, идёт
// декларация готовности/понимания. Клаузы разбираются sentences(), поэтому
// граница буллета в них потеряна; восстанавливаем её по исходному тексту:
// текущая клауза заканчивается на «;»/«,» (продолжение), а не на «.»/«?»/
// «!» (новая мысль — следующий буллет или абзац).
func nextIsDeclaration(clause, text string) bool {
	idx := clauseIndex(text, clause)
	if idx < 0 || idx+1 >= len(sentences(text)) {
		return false
	}
	if !endsWithContinuation(clause, text) {
		return false
	}
	return understandingRe.MatchString(sentences(text)[idx+1])
}

// clauseIndex — номер клаузы в разбиении sentences по её содержимому.
func clauseIndex(text, clause string) int {
	for i, c := range sentences(text) {
		if c == clause {
			return i
		}
	}
	return -1
}

// endsWithContinuation — перед разделителем в исходном тексте стоял «;» или
// «,», а не «.»/«?»/«!».
func endsWithContinuation(clause, text string) bool {
	pos := strings.Index(text, clause)
	if pos < 0 {
		return false
	}
	tail := strings.TrimLeft(text[pos+len(clause):], " \t")
	if tail == "" {
		return false
	}
	switch tail[0] {
	case ';', ',':
		return true
	}
	return false
}

// closingBoldIndex — индекс конца ЖИРНОГО ЗАГОЛОВКА (открывающая рамка в
// позиции 0, закрывающая — конец фрагмента). Возвращает индекс символа после
// закрывающей рамки, либо -1.
//
// Раньше здесь был strings.Index(c[2:], "**"), который находил не закрывающую
// рамку, а ПАРУ — при «**Media / Video / Render pipelines:** опыт …» в
// «**Media / Video / Render pipelines:**» первая пара «**» стоит в начале, а
// Index искал уже со второго символа и попадал на пару «:** » внутри текста.
// Из-за этого заголовок не распознавался, а токены media/video из него
// засчитывались как факт.
func closingBoldIndex(c string) int {
	if !strings.HasPrefix(c, "**") {
		return -1
	}
	// Работаем в РУНАХ, а не в байтах: заголовок кириллический, и срез по
	// байтам попадал внутрь символа — «**Media / Video / Render pipelines:**»
	// давал 37 (байт) вместо 33 (рун), и head в конце содержал обрезок UTF-8.
	rest := []rune(c[2:])
	rBold := []rune("**")
	rColonBold := []rune(":**")
	if i := runeIndex(rest, rColonBold); i >= 0 {
		return 2 + i + len(rColonBold)
	}
	if i := runeIndex(rest, rBold); i >= 0 {
		return 2 + i + len(rBold)
	}
	return -1
}

// runeIndex — индекс подстроки-рун в срезе рун.
func runeIndex(hay []rune, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return -1
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// stripListMarker — убирает маркер списка в начале клаузы: «- **Media:**» →
// «**Media:**», «* пункт» → «пункт». Без этого заголовок буллета не распознаётся:
// живое письмо начинало буллет с «- **Media / Video / Render pipelines:**», и
// жирная рамка оказывалась не в начале клаузы.
func stripListMarker(clause string) string {
	c := strings.TrimSpace(clause)
	for _, m := range []string{"- ", "* ", "+ ", "• "} {
		if strings.HasPrefix(c, m) {
			return strings.TrimSpace(c[len(m):])
		}
	}
	return c
}

// inBulletHeader — токен встречается ТОЛЬКО в жирном заголовке буллета, а не в
// его содержательной части. Разрезаем клаузу по рамке и проверяем остаток:
//
//	«**Media / Video:** опыт оптимизации…» → media/video в заголовке,
//	                                  «опыт оптимизации…» — без них;
//	«**Kafka:** Stable ID, 10 000 RPS»  → kafka в заголовке, но «Stable ID,
//	                                  10 000 RPS» — факт про Kafka.
//
// Возвращает true, только если в остатке токена НЕТ. Заголовок повторяет тему
// требования и не доказывает опыт: его пишет и мост «**Media:** готовность
// осваивать». Живой кейс Fullstack Backend (сентябрь 2026) — требование
// «Экспертиза в Media/Video» закрывалось такими заголовками.
func inBulletHeader(token, clause, text string) bool {
	c := stripListMarker(clause)
	if !strings.HasPrefix(c, "**") {
		return false
	}
	end := closingBoldIndex(c)
	if end < 0 {
		return false
	}
	head := strings.TrimSuffix(strings.TrimSpace(string([]rune(c)[2:end])), "**")
	rest := strings.TrimSpace(string([]rune(c)[end:]))
	if !findText(token, head) {
		return false
	}
	// Различаем ТЕМУ буллета и СОДЕРЖАНИЕ.
	//
	//	«**Kafka:** Stable ID, 10 000 RPS»  → тема=Kafka, содержание=факт.
	//	    Kafka засчитывается: буллет О нём и содержит подтверждение.
	//	«**Media / Video / Render pipelines:** опыт оптимизации 10 000 RPS
	//	  (Stable ID); готовность осваивать медиа-конвейеры» → тема=Media/Video,
	//	  содержание=чужие факты + декларация готовности. Тема без
	//	  подтверждения в содержании НЕ доказывает опыт.
	//
	// Решает объём содержательной части: если в ней есть хоть один ФАКТОПОДОБНЫЙ
	// фрагмент (метрика, имя проекта, технология), заголовок считается
	// подтверждённым. Для Media/Video содержание — «готовность осваивать»,
	// декларация, а не факт.
	restStripped := strings.TrimLeft(rest, ",;:-")
	if restStripped == "" {
		return true // заголовок без содержания: тема заявлена, факта нет
	}
	// Декларация готовности/понимания в содержании — заголовок не
	// подтверждён: «**Media:** готовность осваивать медиа-конвейеры».
	if understandingRe.MatchString(rest) {
		return true
	}
	// Декларация может стоять в СЛЕДУЮЩЕЙ клаузе того же буллета:
	// «**Media / Video:** опыт оптимизации 10 000 RPS (Stable ID); готовность
	// осваивать медиа-конвейеры». Обе части — про один буллет, и вторая
	// прямо говорит, что опыта в медиа нет. Считать заголовок
	// подтверждённым содержимым первой части нельзя.
	if nextIsDeclaration(clause, text) {
		return true
	}
	// Иначе содержание подтверждает заголовок: «**Kafka:** Stable ID,
	// 10 000 RPS», «**PostgreSQL (глубокое знание)**, MySQL, ClickHouse».
	return false
}

// isHeaderFragment — клауза состоит ТОЛЬКО из жирного заголовка, без
// содержательной части. Именно такой фрагмент ничего не утверждает:
//
//	«**Media / Video / Render pipelines:**»            → заголовок, тема
//	«**Kafka:** Stable ID, 10 000 RPS, at-least-once»  → факт, закрывает
//
// Заголовок с текстом после него фактом считается: там и правда написано про
// Kafka. Регресс-тесты TestLongParenListIsNotAlternatives («**Kafka:** Stable
// ID, 10 000 RPS») и TestProfileLimiterDoesNotLeakToOtherTokens ловят ровно
// это, поэтому отбрасывать можно ТОЛЬКО заголовок целиком.
func isHeaderFragment(clause string) bool {
	c := stripListMarker(clause)
	if !strings.HasPrefix(c, "**") {
		return false
	}
	end := closingBoldIndex(c)
	if end < 0 {
		return false
	}
	// Хвост после закрывающей рамки — содержательная часть буллета.
	rest := strings.TrimLeft(strings.TrimSpace(c[end:]), ":*-— \u00a0")
	return rest == ""
}

// understandingRe — декларация понимания/готовности вместо факта опыта:
// «понимаю архитектуру оркестрации», «прочная база в Docker Compose», «готов
// перенести на K8s, Helm». Живой случай: такое упоминание закрывало требование
// «Kubernetes — эксплуатация» как «закрыто в письме», хотя опыта нет. Работает
// в паре с findText: самого слова «понимаю» мало — в клаузе должен быть ещё
// токен технологии.
// Формы «(быстро|легко) освоить/перенести» добавлены в сентябре 2026 после
// живого кейса SRE-вакансии: письмо писало «что позволяет быстро освоить
// отладку приложений в K8s», а требование «Kubernetes — опыт отладки» считалось
// закрытым, потому что «готов» в этой формулировке нет.
var understandingRe = regexp.MustCompile(`(?i)понима[юе]\w*|разбира[юесь]\w*|понятн\w*|понял\w*|готов.{0,30}перенести|перенесу|освою|готов.{0,20}освоить|` +
	// готовность/готовость/способность + любой инфинитив переноса опыта.
	// Живой кейс Fullstack Backend (сентябрь 2026): «готовность осваивать
	// медиа-конвейеры» закрывала требование «Экспертиза в Media/Video» как
	// «закрыто в письме» — прежнее правило знало только «готов … освоить», а
	// в письме было «готовн-ость осва-ивать»: и суффикс -ость, и инфинитив
	// осваивать вместо освоить.
	`готовност[ьи]|готовост[ьи]|способност[ьи]|готов.{0,15}(?:переносить|перенести|осваивать|освоить|изучать)|` +
	`(?:готов|буду|смогу|хочу)\s+(?:быстро\s+|легко\s+)?(?:осваивать|переносить|изучать)|` +
	`(?:осваивать|переносить)\s+(?:на новый|под|в этой|эту|на друг)|` +
	`(?:быстро|легко|позволяет|позволит|смогу|легко\s+и)\s+(?:быстро\s+|легко\s+)?(?:освоить|перенести)`)

// clauseNegated — клауза под отрицанием: маркер в ней самой или в
// следующей клаузе, если та состоит из одного маркера.
func clauseNegated(clauses []string, i int) bool {
	if negRe.MatchString(clauses[i]) || profileGapRe.MatchString(clauses[i]) {
		return true
	}
	return i+1 < len(clauses) && bareNegRe.MatchString(clauses[i+1])
}

// clauseNegatedFor — как clauseNegated, но с учётом «хвостового» отрицания
// САМОГО токена в соседней клаузе того же буллета.
//
// Живой кейс АФЛТ-Системс (октябрь 2026): буллет адаптации
// «- JSON-RPC: REST API — laravel-api, URL-Shortener; JSON-RPC — не применял,
// готов оперативно освоить». sentences() режет по «;», поэтому требование
// «Опыт работы с JSON-RPC» видело «JSON-RPC» в первой клаузе без всякого
// отрицания и закрывалось «закрыто в письме» — письмо честно говорило
// обратное. bareNegRe здесь не спасает: он требует, чтобы вся следующая
// клауза СОСТОЯЛА из маркера, а она начинается с «JSON-RPC —».
//
// Правило намеренно узкое: отрицание должно относиться к тому же токену.
// Соседняя клауза про другой предмет («опыта нет» про OpenTelemetry рядом с
// фактом про observability) требование не роняет — за этот случай отвечает
// scope-проверка ниже.
func clauseNegatedFor(clauses []string, i int, token string) bool {
	if clauseNegated(clauses, i) {
		return true
	}
	if token == "" {
		return false
	}
	for j := i + 1; j < len(clauses) && j <= i+2; j++ {
		if !negRe.MatchString(clauses[j]) && !profileGapRe.MatchString(clauses[j]) {
			continue
		}
		if findText(token, clauses[j]) {
			return true
		}
	}
	return false
}

// allowedUnderstandingRe — профиль ЯВНО разрешает декларацию понимания:
// «WAL-G/Patroni: НЕ работал; допустимо «понимаю принципы WAL»» (context/01).
// Такая строка — разрешение владельца профиля на конкретную формулировку, и она
// должна побеждать understandingRe: живой кейс SRE-вакансии — сильнейшее
// требование PostgreSQL падало в «нет данных» из-за «Понимаю принципы WAL»,
// хотя профиль разрешил ровно эту форму.
var allowedUnderstandingRe = regexp.MustCompile(`(?i)допустимо\s+[«"]?[^.\n]{0,40}(понима|разбира|знаком)|` +
	`разреш(ено|ается)\s+[«"]?[^.\n]{0,40}(понима|разбира|знаком)|` +
	`(можно|допустимо)\s+(писать|говорить|заявлять)\s+[«"]?[^.\n]{0,40}(понима|разбира)`)

// understandingAllowed — профиль разрешает декларацию понимания для токена.
func understandingAllowed(token, profile string) bool {
	if strings.TrimSpace(profile) == "" {
		return false
	}
	for _, c := range sentences(profile) {
		if findText(token, c) && allowedUnderstandingRe.MatchString(c) {
			return true
		}
	}
	return false
}

// findFact — токен назван в тексте как факт: существует клауза, где токен есть,
// а отрицания нет и это не декларация понимания («понимаю X», «готов перенести
// на X») — такая клауза описывает знакомство с темой, а не работу с ней.
// profile передаётся, чтобы разрешение профиля («допустимо понимать принципы
// WAL») могло перевесить декларацию понимания в письме. Пустая строка —
// профиля нет, правило разрешения молча выключено.
func findFact(token, text, profile string) bool {
	clauses := sentences(text)
	allowed := understandingAllowed(token, profile)
	for i, c := range clauses {
		if findText(token, c) && !clauseNegatedFor(clauses, i, token) &&
			(!understandingRe.MatchString(c) || allowed) &&
			// Список стека вакансии в буллете адаптации называет технологии
			// работодателя, а не опыт кандидата (живой кейс Fullstack/mistral).
			!adaptationClause(text, token, c) &&
			// Токен из заголовка буллета («**Media / Video:** …») тему
			// повторяет, но ничего не утверждает: как факт он не засчитывается.
			!isHeaderFragment(c) && !inBulletHeader(token, c, text) {
			return true
		}
	}
	return false
}

// tokenNegatedOnly — токен встречается в тексте, но только с отрицанием
// (ни одного «фактового» вхождения).
func tokenNegatedOnly(token, text, profile string) bool {
	return findText(token, text) && !findFact(token, text, profile)
}

// altListRe — OR-списки технологий в тексте требования. Вакансия
// перечисляет взаимозаменяемые варианты: «(Kafka, RabbitMQ)»,
// «RabbitMQ/Kafka», «Kafka или RabbitMQ». Факта по любой позиции
// достаточно для закрытия — то же правило «слэш = ИЛИ», что в промпте
// письма. Обычное перечисление через запятую без скобок/слэша/«или»
// OR-списком НЕ считается: «Kafka, PostgreSQL» — оба нужны.
var (
	parenAltRe = regexp.MustCompile(`\(([^()]*)\)`)

	slashAltRe = regexp.MustCompile(`(?i)[a-z][a-z0-9+#.-]{1,30}\s*/\s*[a-z][a-z0-9+#.-]{1,30}`)
	orAltRe    = regexp.MustCompile(`(?i)[a-z][a-z0-9+#.-]{1,30}(?:\s*,?\s+или\s+[a-z][a-z0-9+#.-]{1,30})+`)
)

// maxAltItems — сколько элементов в скобках ещё считаются перечнем
// взаимозаменяемых альтернатив. Длинный перечень — описание требования.
const maxAltItems = 4

// altGroups — группы альтернатив требования, каждая как список
// канонических токенов.
func altGroups(reqText string) [][]string {
	var groups [][]string
	add := func(s string) {
		if toks := reqTokens(s); len(toks) >= 2 {
			groups = append(groups, toks)
		}
	}
	for _, m := range parenAltRe.FindAllStringSubmatch(reqText, -1) {
		// Скобки — не всякий перечень альтернатив. Короткий список из
		// перечисляемых фич («(client credentials, device flow)») — это
		// описание требования, а не «подойдёт любой из». Живой кейс (вакансия
		// IAM): перечисление из 5 auth-потоков разбиралось как OR-список, и
		// требование «глубокое знание OAuth» закрывалось по словам token/code/
		// flow из письма, хотя письмо называло это расширением текущего опыта.
		// Порог — 4 элемента: настоящие альтернативы короткие.
		if strings.Count(m[1], ",")+1 > maxAltItems {
			continue
		}
		add(m[1])
	}
	for _, m := range slashAltRe.FindAllString(reqText, -1) {
		add(m)
	}
	for _, m := range orAltRe.FindAllString(reqText, -1) {
		add(m)
	}
	return groups
}

// alternativesOnly — каждый недостающий токен входит в OR-группу вместе
// с каким-нибудь найденным токеном. Тогда требование закрыто: найденный
// факт замещает альтернативу. Если хоть один missing вне OR-связи с
// found (или групп нет вовсе) — правило не применяется.
func alternativesOnly(missing []string, found map[string]bool, reqText string) bool {
	groups := altGroups(reqText)
	if len(groups) == 0 {
		return false
	}
	for _, mt := range missing {
		ok := false
		for _, g := range groups {
			if !slices.Contains(g, mt) {
				continue
			}
			if slices.ContainsFunc(g, func(t string) bool { return found[t] }) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// alternativeDirectionNegated — одно из альтернативных направлений целиком под
// отрицанием в письме. Возвращает (true, честная нота) только когда ВСЕ токены
// непокрытого направления найдены, но каждое найдено в клаузе с отрицанием —
// то есть письмо честно назвало пробел по этому направлению.
//
// Условие «все» принципиально: частично отрицанное направление может иметь
// и настоящие факты («Media/Video: стриминг делал, кодеки — нет»), и тогда
// закрытие по альтернативе правомерно.
func alternativeDirectionNegated(reqText, letter string, found map[string]bool) (bool, string) {
	groups := altGroups(reqText)
	if len(groups) < 2 {
		// Нужно минимум два направления: одиночный OR-список внутри
		// требования («Kafka или RabbitMQ») — обычное правило альтернатив.
		return false, ""
	}
	cls := sentences(letter)
	var negatedGroups [][]string
	for _, g := range groups {
		allNegated := len(g) > 0
		for _, t := range g {
			negOnly := false
			for i, cl := range cls {
				if findText(t, cl) && clauseNegated(cls, i) {
					negOnly = true
					break
				}
			}
			if !negOnly {
				allNegated = false
				break
			}
		}
		if allNegated {
			negatedGroups = append(negatedGroups, g)
		}
	}
	if len(negatedGroups) == 0 {
		return false, ""
	}
	// Ищем покрытое направление: факт по другой группе или по токену вне групп.
	covered := false
	for _, g := range groups {
		isNegated := false
		for _, ng := range negatedGroups {
			if slices.Equal(g, ng) {
				isNegated = true
				break
			}
		}
		if isNegated {
			continue
		}
		for _, t := range g {
			if found[t] {
				covered = true
				break
			}
		}
	}
	if !covered {
		return false, ""
	}
	labels := make([]string, 0, len(negatedGroups))
	for _, g := range negatedGroups {
		labels = append(labels, strings.Join(g, "/"))
	}
	note := "в письме честно назван пробел по направлению: " +
		strings.Join(labels, ", ") + " — закрыто по другому направлению"
	return true, note
}

// matchTokens — покрытие требования по токенам в одном источнике: сначала
// «все токены», затем OR-список альтернатив, затем правило большинства
// («Linux (systemd, cron)»: systemd есть, cron нет — ядро требования
// закрыто, недостающее названо в ноте; один неупомянутый термин не роняет
// требование в missing; один токен — либо есть, либо нет). Проверка идёт
// «источник за источником»: порядок важен, иначе «профиль закрывает все
// токены» бьёт «письмо закрывает большинство» и выдаётся совет «впиши в
// письмо» при уже закрытом письме (живой кейс DDD + Hexagonal: письмо даёт
// ddd/hexagonal/architecture, нет только api).
//
// Токен, честно отрицанный в письме («Transactional outbox не использовал»),
// не засчитывается нигде — включая профиль: честный пробел письма
// приоритетнее любого факта профиля, иначе matcher советует вписать
// неприменённый опыт.
func matchTokens(tokens []string, reqText, src, text, letter, profile string) (string, string, bool) {
	all := true
	for _, t := range tokens {
		if !countsAsFact(t, src, text, letter, profile) {
			all = false
			break
		}
	}
	if all {
		if src == SrcProfile {
			return src, "в профиле есть факт, но в письмо не попал — впиши в письмо, закроется полностью", true
		}
		return src, "закрыто в письме", true
	}
	var missing []string
	foundSet := map[string]bool{}
	for _, t := range tokens {
		if countsAsFact(t, src, text, letter, profile) {
			foundSet[t] = true
		} else {
			missing = append(missing, t)
		}
	}
	// OR-список: «(Kafka, RabbitMQ)» при факте Kafka закрыт, даже если
	// RabbitMQ честно назван пробелом — технологии в списке взаимозаменяемы.
	// Проверяем до честного пробела: иначе OR-требование уходило в unknown.
	if len(missing) > 0 && len(foundSet) > 0 && alternativesOnly(missing, foundSet, reqText) {
		note := "; не обязательны (альтернативы): " + strings.Join(missing, ", ")
		if src == SrcProfile {
			return src, "в профиле есть факт по альтернативному списку (не обязательны: " + strings.Join(missing, ", ") + ") — впиши в письмо, закроется полностью", true
		}
		return src, "закрыто в письме по альтернативному списку" + note, true
	}
	// Направление-«ИЛИ» целиком под отрицанием: «Media/Video: опыта работы с
	// видеопайплайнами и кодеками нет». Токены media/video есть в клаузе, но
	// принадлежат честно отрицанному направлению, поэтому фактом они не
	// являются — иначе «закрыто по альтернативному списку» называет ложное
	// покрытие Media/Video при честном пробеле, названном в письме.
	//
	// Живой кейс AI-продукта (сентябрь 2026): требование «Экспертиза в одном
	// из двух направлений: Media/Video … или AI Agents …» закрывалось целиком
	// («закрыто в письме», score 100), хотя Media/Video было названо пробелом
	// словами, а вердикт об этом не говорил нигде.
	if len(missing) > 0 && len(foundSet) > 0 && alternativesOnly(missing, foundSet, reqText) {
		if negated, label := alternativeDirectionNegated(reqText, letter, foundSet); negated {
			return SrcUnknown, label, false
		}
	}
	// Majority может закрыть, только если missing не содержит
	// честно отрицаемых токенов: «почти всё, но один честно назван
	// пробелом» — это unknown, а не letter/profile.
	for _, mt := range missing {
		if tokenNegatedOnly(mt, letter, profile) {
			if note, ok := honestGapNote(tokens, letter, profile); ok {
				return "", note, false
			}
		}
	}
	// majority-порог: found > missing ИЛИ found == missing (ровно 50%).
	// Второе введено для англоязычных требований с длинными формулировками:
	// «MySQL - query optimization, schema design, migrations» — 6 токенов,
	// found = 3 (mysql, schema, design), missing = 3 (query, optimization,
	// migrations) — 50/50; до фикса это было missing, хотя требование
	// фактически закрыто.
	//
	// Ограничения:
	// 1. found >= 2 — один токен («kafka») не считается закрытым по majority.
	// 2. found == missing (50/50) — только при total >= 6 (развёрнутая
	//    формулировка). Для total 4–5 порог 50% слишком слабый:
	//    «Transactional outbox на PostgreSQL» (total=3–4, found=2, missing=1)
	//    — паттерн назван соседним фактом, но сам не применён; это
	//    регресс TestEvaluateProfileLimiterBeatsBridgeLabel. Для total >= 6
	//    50% — разумный компромисс: половина фактов подтверждена.
	if len(foundSet) >= 2 &&
		(len(foundSet) > len(tokens)-len(foundSet) ||
			(len(foundSet) == len(tokens)-len(foundSet) && len(tokens) >= 6)) &&
		len(missing) > 0 {
		if src == SrcProfile {
			return src, "в профиле есть факт по большинству токенов (не упомянуты: " + strings.Join(missing, ", ") + ") — впиши в письмо, закроется полностью", true
		}
		return src, "закрыто в письме; не упомянуты: " + strings.Join(missing, ", ") + " — добавь", true
	}
	return "", "", false
}

// countsAsFact — токен считается фактом в источнике. Два ограничителя:
//   - токен, честно отрицанный в письме, не засчитывается нигде (включая
//     профиль): честный пробел письма приоритетнее любого факта профиля,
//     иначе matcher советует вписать неприменённый опыт;
//   - токен, отрицанный в клаузе профиля (строка-ограничитель), не
//     засчитывается как факт профиля, даже если в другой клаузе профиля он
//     упомянут как метка моста. Живой кейс платёжной вакансии: метка
//     «(мост к outbox)» в общем профиле давала «в профиле есть факт — впиши
//     в письмо, закроется полностью» при живом ограничителе
//     «Transactional outbox на PostgreSQL: НЕ использовал» — совет заявить
//     неприменённый паттерн.
func countsAsFact(t, src, text, letter, profile string) bool {
	if src == SrcProfile {
		// Запретный блок профиля не источник факта (живой кейс AI Agents:
		// токен нашёлся внутри строки-запрета), но ограничителем он остаётся —
		// declinedInProfile ниже работает по ПОЛНОМУ профилю.
		if !findFact(t, stripForbiddenProfile(text), profile) || tokenNegatedOnly(t, letter, profile) {
			return false
		}
		return !declinedInProfile(t, text)
	}
	if !findFact(t, text, profile) || tokenNegatedOnly(t, letter, profile) {
		return false
	}
	return true
}

// bridgeLabelRe — метка моста в профиле («(мост к outbox)», «мост: polling-журнал»).
// Метка называет смежный опыт, а не владение технологией требования, поэтому
// чистым фактом такая клауза не считается. Живой регресс платёжной вакансии:
// метка «(мост к outbox)» в блоке фактов перебивала ограничитель
// «Transactional outbox: не использовал» (TestEvaluateProfileLimiterBeatsBridgeLabel).
var bridgeLabelRe = regexp.MustCompile(`(?i)(^|[^а-яё])мост`)

// notPartRe — заглавная частица «НЕ» как ограничитель профиля: «…bash
// (LLM-конвейеры), НЕ Python» (context/01:261), «НЕ Kafka» (context/02:77).
// Только заглавная форма: строчное «не» в прозе («не пробел», «не только»,
// «не значит») ограничителем не является.
var notPartRe = regexp.MustCompile(`(^|[^а-яёА-ЯЁ])НЕ([^а-яёА-ЯЁ]|$)`)

// declinedInProfile — токен назван ограничителем профиля: профиль-фактом он
// не считается. Два прохода, потому что у ограничений разная область действия:
//
//  1. Глобальные вето — прямое признание пробела профилем («готов освоить X»)
//     и хвостовое «X, опыта нет». Это заявления о себе, они авторитетны для
//     всего профиля: «Airflow» не становится фактом оттого, что рядом есть
//     клауза «принципы ETL переносятся на Airflow».
//  2. Клаузные ограничители — backward-маркеры («НЕ использовал») и частица
//     «НЕ» гасят токен в СВОЕЙ клаузе, но не отменяют факты профиля в других
//     клаузах. Регресс 2026-09-19: ограничитель «Transactional outbox на
//     PostgreSQL: НЕ использовал» ронял требование «Опыт работы с SQL БД
//     (Postgres)» при живом факте «PostgreSQL (глубокое знание)» (context/01:7,439).
//
// Токен declined ⇔ он упомянут в профиле и ни одной чистой клаузы у него нет
// (метки моста чистыми не считаются). Направленность сохранена: backward гасит
// всю клаузу, forward («готов освоить Python/Airflow») — только то, что стоит
// после него, поэтому положительный якорь моста до маркера («bash-автоматизация»)
// фактом остаётся (TestDeclinedInProfileForwardMarker).
func declinedInProfile(token, profile string) bool {
	clauses := sentences(profile)
	for i, c := range clauses {
		if !findText(token, c) {
			continue
		}
		if loc := forwardNegRe.FindStringIndex(c); loc != nil && !findText(token, c[:loc[0]]) {
			return true
		}
		if i+1 < len(clauses) && bareNegRe.MatchString(clauses[i+1]) {
			return true
		}
	}
	declined := false
	for _, c := range clauses {
		if !findText(token, c) {
			continue
		}
		if backwardNegRe.MatchString(c) || notPartRe.MatchString(c) || profileGapRe.MatchString(c) {
			declined = true
			continue
		}
		if bridgeLabelRe.MatchString(c) {
			continue
		}
		return false
	}
	return declined
}

// coverage — где требование закрыто. Порядок проверки: (1) технологии
// требования есть в тексте целиком; (2) требование без технологий — по
// концепт-признакам (≥2 сигнала). Источники по приоритету: письмо,
// профиль, мост, неизвестно.
func coverage(concepts []Concept, req Requirement, letter, profile string) (source, note string) {
	tokens := reqTokens(req.Text)
	if len(tokens) == 0 {
		// Концептное требование: проверяем честный пробел до проверки признаков,
		// чтобы «С платёжными процессингами не работал» не засчитался как покрытие.
		if note, ok := conceptHonestGap(concepts, req.Text, letter); ok {
			return SrcUnknown, note
		}
		// Первое сработавшее понятие обязательно: «медиа/видео» и «агентские
		// системы» стоят раньше общих концептов и не могут быть закрыты ими.
		if name, gap := conceptPrimaryGap(concepts, req.Text, letter, profile); gap {
			return SrcUnknown, "в письме и профиле нет опыта по «" + name + "» — проверь вручную, это не значит «опыта нет»"
		}

		// Ищем признаки в письме, потом в профиле.
		for _, src := range []struct {
			label, text string
		}{
			{SrcLetter, letter},
			{SrcProfile, profile},
		} {
			if name, hits := conceptHit(concepts, req.Text, src.text, 2); name != "" {
				if src.label == SrcProfile {
					return SrcProfile, "закрыто по признакам («" + name + "»: " + strings.Join(hits, ", ") + "), но в письмо не попало — впиши в письмо, закроется полностью"
				}
				return SrcLetter, "закрыто по признакам («" + name + "»: " + strings.Join(hits, ", ") + ")"
			}
		}
		return SrcUnknown, "в письме и профиле нет достаточных признаков по этому требованию — проверь вручную"
	}
	for _, src := range []struct {
		label, text string
	}{
		{SrcLetter, letter},
		{SrcProfile, profile},
	} {
		if s, note, ok := matchTokens(tokens, req.Text, src.label, src.text, letter, profile); ok {
			// Concept-уровневый честный пробел понижает буквальное закрытие:
			// «observability» в письме есть (Prometheus), но трейсинг назван
			// пробелом словами — «закрыто в письме» здесь нечестно.
			if note2, gap := conceptGapNote(concepts, req.Text, letter); gap {
				if s == SrcLetter {
					return SrcUnknown, note2
				}
			}
			return s, note
		}
	}
	// Честный пробел: токены требования названы в письме, но только
	// с отрицанием («С OpenTelemetry опыта нет, готов освоить»). Это не
	// закрытие — но и не «в письме нет вообще»: модель отработала чек-лист,
	// пробел назван словами. unknown с человеческой нотой, не missing.
	if note, ok := honestGapNote(tokens, letter, profile); ok {
		return SrcUnknown, note
	}
	// Мост: по токенам требования (Kubernetes в bridges НЕ входит —
	// must-have «K8s в проде» без опыта не закрывается соседним опытом).
	if note, ok := bridgeHit(tokens, letter, profile); ok {
		return SrcBridge, note
	}
	// Концепт-путь для требований с МАЛЫМ числом токенов. Живой кейс PHP-
	// архитектора (сентябрь 2026): «Работа с реляционными и NoSQL базами
	// данных, понимание консистентности и производительности» почти целиком
	// кириллическое, tokenRe извлекает из него ОДИН токен «nosql», которого в
	// письме нет дословно. Ветка len(tokens)==0 не срабатывала, концепт «реля-
	// ционные БД и SQL» с обоими сигналами в письме не спрашивался, и требова-
	// ние уходило в missing при «Экспертное владение PostgreSQL, MySQL,
	// ClickHouse, Redis, Oracle» прямо в письме.
	//
	// Порог 1 токен: если лексических зацепок почти нет, признаки концепта —
	// единственный способ оценить требование. При 2+ токенах концепт-путь не
	// подстраховывает: там token-матчинг отвечает за точность, а слабые
	// совпадения по двум разным концептам дали бы ложные закрытия.
	//
	// ТОЛЬКО по письму. Первая версия проверяла и профиль, и это дало три
	// регрессии: «Опыт эксплуатации Kubernetes в проде» (tokens=[k8s]) и
	// «Опыт с Docker Swarm» закрывались концептом из ПРОФИЛЯ, хотя в письме
	// («Стек: Go, Kafka») нет ничего — а это ровно тот ложный skip, который
	// эти тесты и охраняют. Признаки из профиля не доказывают, что кандидат
	// написал это в письмо.
	if len(tokens) <= 1 {
		// conceptPrimaryGap здесь НЕ применяется: у требования есть токен
		// (k8s, observability), и «нет нигде» — это missing, а не unknown.
		// Правило нужно только требованиям без токенов вообще (видео-рендеринг,
		// AI-агенты), где концепт — единственная опора.
		if name, hits := conceptHit(concepts, req.Text, letter, 2); name != "" {
			return SrcLetter, "закрыто по признакам («" + name + "»: " +
				strings.Join(hits, ", ") + ")"
		}
	}
	return "", ""
}

// conceptGapNote — concept-уровневый честный пробел: часть сигналов концепта
// в письме подтверждена, а часть названа пробелом словами. Живой кейс SRE
// (сентябрь 2026): требование «observability (мониторинг, логи, трейсинг)»
// закрывалось фактом Prometheus/Grafana, хотя письмо честно писало «Опыт
// интеграции OpenTelemetry/Tempo отсутствует» — по буквальным токенам
// требования («observability») пробел невидим, «трейсинг» в письме не
// встречается вовсе.
//
// Срабатывает ТОЛЬКО при отрицании: молчание пробелом не считается, иначе
// требование «PostgreSQL» с соседним «outbox» снова ушло бы в unknown там,
// где письмо честно пишет «Transactional outbox не использовал» — там
// отрицание буквальное, оно ловится token-путём раньше.
func conceptGapNote(concepts []Concept, reqText, letter string) (string, bool) {
	if strings.TrimSpace(letter) == "" {
		return "", false
	}
	for _, c := range concepts {
		if !c.Trigger.MatchString(reqText) {
			continue
		}
		var positive, declined []string
		for _, sig := range c.Signals {
			// Отрицание понижает требование ТОЛЬКО если само требование
			// называет эту способность. Принцип scope-отрицания
			// (TestEvaluateNegationClauseScoped): требование «Выстраивание
			// observability» закрыто Prometheus, и честно отрицанный
			// соседний OpenTelemetry не при чём — он не назван в требовании.
			// А требование «observability (мониторинг, логи, трейсинг)»
			// трейсинг называет прямо, и вот тут пробел честный.
			if !sig.Re.MatchString(reqText) {
				continue
			}
			found, negated := false, false
			for i, cl := range sentences(letter) {
				if !sig.Re.MatchString(cl) {
					continue
				}
				found = true
				if clauseNegated(sentences(letter), i) || understandingRe.MatchString(cl) {
					negated = true
				}
			}
			switch {
			case found && negated:
				declined = append(declined, sig.Label)
			case found:
				positive = append(positive, sig.Label)
			}
		}
		if len(positive) > 0 && len(declined) > 0 {
			note := "в письме есть " + strings.Join(positive, ", ") +
				", но честно назван пробел: " + strings.Join(declined, ", ")
			return note, true
		}
	}
	return "", false
}

// honestGapNote — нота честного пробела: токены требования названы в письме
// только с отрицанием. Если часть токенов в письме есть позитивно, нота это
// называет («в письме есть postgresql, но outbox честно назван пробелом») —
// иначе кажется, что не упомянуто вообще ничего. ok=false, когда честно
// отрицанных токенов нет.
func honestGapNote(tokens []string, letter, profile string) (string, bool) {
	var negTokens, posTokens []string
	for _, t := range tokens {
		switch {
		case tokenNegatedOnly(t, letter, profile):
			negTokens = append(negTokens, t)
		case findFact(t, letter, profile):
			posTokens = append(posTokens, t)
		}
	}
	if len(negTokens) == 0 {
		return "", false
	}
	note := "в письме честно назван пробел («" + strings.Join(negTokens, ", ") + "»)"
	if len(posTokens) > 0 {
		note = "в письме есть " + strings.Join(posTokens, ", ") + ", но " + strings.Join(negTokens, ", ") + " честно назван пробелом"
	}
	return note + " — для вердикта это «нет данных»", true
}

// bridgeHit — мост по токенам требования подтверждён якорями в письме или
// профиле. Kubernetes в bridges НЕ входит: must-have «K8s в проде» без опыта
// не закрывается соседним опытом.
func bridgeHit(tokens []string, letter, profile string) (string, bool) {
	for _, t := range tokens {
		if b, ok := bridges[t]; ok && b.re.MatchString(letter+profile) {
			return b.note, true
		}
	}
	return "", false
}

// LoadProfile читает context/*.md для матчинга фита — в том виде, в каком их
// увидела модель: drops применяются к содержимому каждого файла, поэтому
// вырезанный композером раздел не может закрыть требование как «факт есть в
// профиле». Без этого вердикт противоречил письму: модель раздела не видела,
// а fit звал fit-fix за фактом, которого в промпте нет.
// Ошибка/отсутствие папки — не фатально: матчинг идёт по пустой строке.
func LoadProfile(contextDir string, drops []prompt.Drop) string {
	var b strings.Builder
	entries, err := os.ReadDir(contextDir)
	if err != nil {
		slog.Warn("context profile unreachable — fit matching will use the letter only", "context_dir", contextDir, "err", err)
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		slog.Warn("no context/*.md found — profile empty, fit matching will use the letter only", "context_dir", contextDir)
		return ""
	}
	sort.Strings(names)
	for i, name := range names {
		raw, err := os.ReadFile(filepath.Join(contextDir, name))
		if err != nil {
			continue
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(cover.DropSections(string(raw), dropsFor(name, drops)))
	}
	return b.String()
}

// dropsFor — только дропы, относящиеся к данному файлу (регистр имён не
// важен: модель могла вернуть имя в другом регистре расширения).
func dropsFor(name string, drops []prompt.Drop) []prompt.Drop {
	var out []prompt.Drop
	for _, d := range drops {
		if strings.EqualFold(d.File, name) {
			out = append(out, d)
		}
	}
	return out
}

// fitBuilder собирает Fit по мере обхода must-have: держит сумму весов,
// базу скора и отложенные советы, чтобы детерминированный матчер (Evaluate)
// и гибридный LLM-путь (MapCoverage) делили один скоринг, вердикт и советы.
// JSON-представление Fit не меняется: служебные поля живут в билдере.
type fitBuilder struct {
	f             Fit
	concepts      []Concept
	sum           float64
	count         int
	missingAdvice []string
	// profile/letter — тексты для диагностики missing: какая часть токенов
	// требования в них всё-таки названа (заполняются Evaluate/MapCoverage).
	profile string
	letter  string
}

// addMust — вклад одного must-have: веса 1.0 (письмо), 0.7 (профиль),
// 0.5 (мост), 0.2 (unknown), 0 (не закрыто) плюс советы.
func (b *fitBuilder) addMust(r Requirement, src, note string) {
	b.count++
	switch src {
	case SrcLetter:
		b.sum += 1.0
		b.f.Covered = append(b.f.Covered, Req{r.Text, src, note, r.Kind})
	case SrcProfile:
		b.sum += 0.7
		b.f.Covered = append(b.f.Covered, Req{r.Text, src, note, r.Kind})
		b.f.Advice = append(b.f.Advice, "в профиле есть факт по «"+r.Text+"», но в письмо он не попал — впиши в письмо, закроется полностью")
	case SrcBridge:
		b.sum += 0.5
		b.f.Caveats = append(b.f.Caveats, Req{r.Text, src, note, r.Kind})
		b.f.Advice = append(b.f.Advice, note+" — в письме и на собеседовании это будет слабое место")
	case SrcUnknown:
		b.sum += 0.2
		// Построчного совета нет: все unknown сведены в одну строку
		// после обхода (шесть одинаковых «проверь вручную» — шум).
		b.f.Caveats = append(b.f.Caveats, Req{r.Text, src, note, r.Kind})
	default:
		b.f.Missing = append(b.f.Missing, Req{r.Text, SrcMissing, b.missingNote(r.Text), r.Kind})
		b.missingAdvice = append(b.missingAdvice, "обязательное требование «"+r.Text+"» не закрыто ничем — письмом это не лечится")
	}
}

// missingNote — человеческая диагностика незакрытого must-have: «в профиле и
// письме нет, моста нет» молчали о том, что часть требования всё-таки названа
// (живой кейс Kairon.Finance: «Frontend stack knowledge: JavaScript,
// TypeScript, Vue, React» улетал в «нет нигде», хотя React/TypeScript были
// в профиле — не хватало только Vue). Статус и вес missing от этого не
// меняются: названное вскользь — не закрытое требование, но пользователю
// важно видеть, насколько требование реально чужое.
func (b *fitBuilder) missingNote(reqText string) string {
	note := "в профиле и письме нет, моста нет"
	var partial []string
	for _, t := range reqTokens(reqText) {
		if findFact(t, b.profile, b.profile) || findFact(t, b.letter, b.profile) {
			partial = append(partial, t)
		}
	}
	if len(partial) > 0 {
		note += " (названо вскользь: " + strings.Join(partial, ", ") + ")"
	}
	return note
}

// finish — nice-to-have бонус, скор, вердикт и советы: точка сходимости
// обоих движков. Вердикт и проценты всегда считает код по таблице весов,
// а не модель — иначе они невоспроизводимы и не тестируются.
func (b *fitBuilder) finish(reqs Requirements, profile, letter string) Fit {
	f := b.f
	// Nice-to-have: закрытый в письме — небольшой бонус, незакрытый — без штрафа.
	for _, r := range reqs.NiceToHave {
		if softTerms.MatchString(r.Text) {
			continue
		}
		if src, _ := coverage(b.concepts, r, letter, profile); src == SrcLetter {
			b.sum += 0.05 * float64(b.count) // бонус +5% от базы must-have за каждый
		}
	}

	if b.count > 0 {
		f.Score = int(b.sum/float64(b.count)*100 + 0.5)
	}
	if f.Score > 100 {
		f.Score = 100
	}

	if b.count == 0 {
		// Ни одного must-have не оценено (все отфильтрованы softTerms/plusRe
		// или MustHave изначально пуст): вердикта быть не должно — нет данных
		// для оценки. Панель не рендерится (verdict == ""), чтобы не показывать
		// противоречие «apply + 0% + все закрыты».
		f.Score = 0
		f.Verdict = ""
		return f
	}

	// Вердикт из той же таблицы покрытия, что и скор: противоречить
	// друг другу они не могут. unknown не роняет вердикт сам по себе:
	// это «нет данных», а не «нет опыта» — из шести unknown при нулевом
	// missing нельзя делать вывод «не откликаться» (реальный кейс
	// архитекторской вакансии). Один missing при закрытом остальном —
	// не «не откликаться», а серая зона: скор ~90%+ при skip —
	// противоречие в плашке (реальный кейс анти-DDoS: 8 из 9 закрыто,
	// один пробел UDP/TCP). skip — два и более пробела, пробел плюс
	// массовое unknown, роль другого профиля. Массовое unknown БЕЗ
	// missing — НЕ skip: клауза unkN >= 3 противоречила спецификации
	// выше и давала «не откликаться» при полностью закрытых must-have
	// (живой регресс Evolution CMS: 5 закрыто цитатами, 0 missing,
	// 3 unknown → skip).
	missN, unkN, brN := len(f.Missing), 0, 0
	// hardMiss — незакрытые ТРЕБОВАНИЯ (без обязанностей): обязанность
	// видна в списке, но вердикт «не откликаться» дают только требования.
	hardMiss := 0
	for _, c := range f.Missing {
		if c.Kind != "duty" {
			hardMiss++
		}
	}
	for _, c := range f.Caveats {
		switch c.Source {
		case SrcBridge:
			brN++
		case SrcUnknown:
			if isHonestGap(c.Note) {
				// Честный пробел, названный в письме словами, — не «нет
				// данных» в смысле риска: кандидат сам раскрыл пробел,
				// проверять вручную нечего, а наказывать честное письмо
				// skip'ом — перверсия стимулов (скрытие пробелов давало
				// бы лучший вердикт). В skip-пороге unknown не участвует.
				continue
			}
			unkN++
		}
	}
	switch {
	case hardMiss >= 2 || (hardMiss == 1 && unkN >= 2) || roleMismatch(reqs, profile):
		f.Verdict = Skip
	case hardMiss == 1 || brN >= 1 || unkN >= 1 || len(f.Caveats) > 0:
		f.Verdict = Caveats // оговорка обязана назвать слабое место — Advice уже заполнен
	default:
		f.Verdict = Apply
	}
	switch f.Verdict {
	case Skip:
		if roleMismatch(reqs, profile) {
			f.Advice = append([]string{"роль вакансии другого профиля, чем у кандидата"}, f.Advice...)
		} else {
			f.Advice = append([]string{"не тратить время на отклик: есть незакрытые must-have"}, f.Advice...)
		}
	case Caveats:
		if hardMiss == 1 {
			f.Advice = append([]string{"откликаться с оговоркой: один must-have не закрыт («" + firstHardMiss(f.Missing) + "») — оцени, критичен ли он для этой вакансии"}, f.Advice...)
		} else {
			f.Advice = append([]string{"откликаться с оговоркой — слабое место названо ниже"}, f.Advice...)
		}
	case Apply:
		f.Advice = append([]string{"все обязательные требования закрыты — откликаться"}, f.Advice...)
	}
	// Мерж построчных missing-советов: при Caveats с ровно одним missing
	// заголовок уже называет требование и даёт действие («оцени, критичен
	// ли он») — построчный совет дублировал бы его. При skip заголовок
	// без имён («не тратить время») — построчные советы обязательны,
	// иначе непонятно, какое именно требование не закрыто.
	// Обязанности называем отдельной строкой: пробел виден, но skip не дают.
	if missN > hardMiss {
		f.Advice = append(f.Advice, "не закрыты обязанности (не требования): "+
			strings.Join(dutyGaps(f.Missing), ", ")+" — оцени, критичны ли они")
	}
	if !(f.Verdict == Caveats && hardMiss == 1) {
		f.Advice = append(f.Advice, b.missingAdvice...)
	}
	// unknown — одним сводным советом, а не построчно: шесть одинаковых
	// строк «проверь вручную» — шум, а не помощь. Честные пробелы письма
	// в свод не входят: кандидат их сам раскрыл, «проверь вручную» не нужно.
	if unkN > 0 {
		var texts []string
		for _, c := range f.Caveats {
			if c.Source == SrcUnknown && !isHonestGap(c.Note) {
				texts = append(texts, "«"+c.Text+"»")
			}
		}
		if len(texts) > 0 {
			f.Advice = append(f.Advice, "по "+strconv.Itoa(unkN)+" требовани"+unknownPlural(unkN)+" в профиле нет данных — проверь вручную, это не значит «опыта нет»: "+strings.Join(texts, ", "))
		}
	}
	sort.SliceStable(f.Covered, func(i, j int) bool { return f.Covered[i].Source < f.Covered[j].Source })
	return f
}

// Evaluate — детерминированный вердикт: покрытие must-have против письма
// и профиля, взвешенный скор и три состояния. Чистая функция: без LLM,
// без IO — тестируется на фикстурах.
//
// Веса покрытия (must-have): письмо 1.0, профиль 0.7 (совет «добавь в
// письмо»), мост 0.5 (слабое место), unknown 0.2 («проверь вручную»),
// не закрыто 0. Nice-to-have: закрыт — небольшой бонус, незакрыт — без
// штрафа. «Будет плюсом» и мягкие требования покрытием не считаются.
func Evaluate(concepts []Concept, reqs Requirements, profile, letter, vacancy string) Fit {
	b := &fitBuilder{f: Fit{Role: reqs.Role}, concepts: concepts, profile: profile, letter: letter}
	if reqs.MustHave == nil && reqs.NiceToHave == nil {
		// Разбор не удался — вердикта быть не должно, панель не рендерится.
		b.f.Verdict = ""
		return b.f
	}
	for _, r := range reqs.MustHave {
		if plusRe.MatchString(r.Text) || softTerms.MatchString(r.Text) {
			continue // «будет плюсом» и мягкие не требуют покрытия
		}
		src, note := coverage(concepts, r, letter, profile)
		b.addMust(r, src, note)
	}
	return b.finish(reqs, profile, letter)
}

// isHonestGap — кавеат «честный пробел»: токен требования назван в письме
// словами, но с отрицанием. Проверять вручную нечего, и наказывать такое
// письмо skip'ом нельзя (иначе скрытие пробелов даёт лучший вердикт).
func isHonestGap(note string) bool {
	return strings.Contains(note, "честно назван пробел")
}

// unknownPlural — падеж слова «требование» после числительного.
// База строки — «требовани»: 1 → «по 1 требованию», 6 → «по 6 требованиям».
func unknownPlural(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "ю"
	}
	return "ям"
}

// roleMismatch — роль вакансии другого профиля, чем кандидат. Пока
// единственный надёжный маркер: PHP-primary у Go-кандидата.
func roleMismatch(reqs Requirements, profile string) bool {
	role := strings.ToLower(reqs.Role)
	if role == "" {
		return false
	}
	phpRole := strings.Contains(role, "php")
	// goCand: Go-специалист без PHP-маркеров — откликаться на PHP-вакансию рискованно.
	goCand := regexp.MustCompile(`(?i)go-разработчик|golang|основн.{0,15}\bgo\b`).MatchString(profile)
	// phpCand: кандидат с PHP-маркером. Раньше регулярка требовала
	// «php-разработчик» или «основн...php» — узко, не ловило билингвальный
	// профиль с «PHP — PRODUCTION (17 ЛЕТ ОПЫТА)». Расширяем до факта
	// продакшн-опыта на PHP, не только «названия профессии».
	// Регресс: обрезанный профиль «Основные языки: Go (3 года), PHP (2005+), Bash, SQL»
	// не содержит слова «PHP» в пределах 40 символов от маркера опыта → phpCand=false →
	// ложный roleMismatch → мгновенный skip со счётом 0. Добавляем маркер года «PHP (20\d\d+)»
	// и расширяем окно до 60 символов, чтобы ловить «PHP (2005+)» и «PHP, 17 лет опыта».
	phpCand := regexp.MustCompile(`(?i)php-разработчик|основн.{0,15}php|\bphp\b.{0,60}(production|prod|лет|опыт|20\d\d)|((production|prod|лет|опыт|20\d\d).{0,60}\bphp\b|\bphp\b\s*\(\s*20\d\d)`).MatchString(profile)
	return phpRole && goCand && !phpCand
}

// profileForbiddenRe — маркеры запретного буллета профиля. Такой блок
// ЗАПРЕЩАЕТ заявку, а не подтверждает факт: токен внутри него — не факт.
//
// Живой кейс Fullstack Backend (октябрь 2026): fit сообщил «в профиле есть
// факт по большинству токенов (не упомянуты: tool) — впиши в письмо» и повесил
// это в fitFixable. Единственное упоминание «AI Agents» во всём контексте —
// context/01:340, ВНУТРИ запрета «ИИ-инструменты — НЕ заявлять без факта …
// модель дописывает … ловится на интервью». Факта нет: закрыть требование
// нельзя (писать запрещено), и автоправка зацикливалась вхолостую.
//
// Маркеры проверены по контексту: все 16 вхождений — только в запретных
// буллетах (production-grade IAM, Consensus/Paxos/Raft, Service Mesh,
// ИИ-инструменты, гарантия порядка Kafka, go-queue-broker).
var profileForbiddenRe = regexp.MustCompile(`НЕ заявлять|0 вхождений|дописывает|ловится на интервью`)

// stripForbiddenProfile — профиль без запретных блоков, ТОЛЬКО для поиска
// фактов. Блок = непрерывная группа строк буллета (начало: «- »/«* »/«#»
// или пустая; продолжение — с ведущими пробелами): маркеры запретов
// разбросаны по 5 строкам, и построчный сплит их не вырезал бы.
//
// ВАЖНО: ограничители (declinedInProfile/tokenNegatedOnly) считаются по
// ПОЛНОМУ профилю — запрет обязан продолжать работать вето. Вырезается
// только путь «найдено → факт».
func stripForbiddenProfile(profile string) string {
	var kept []string
	var block []string
	flush := func() {
		if len(block) == 0 {
			return
		}
		if !profileForbiddenRe.MatchString(strings.Join(block, "\n")) {
			kept = append(kept, block...)
		}
		block = nil
	}
	for _, line := range strings.Split(profile, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "- ") ||
			strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "#") {
			flush()
		}
		block = append(block, line)
	}
	flush()
	return strings.Join(kept, "\n")
}

// genericTokens — токены-метки области, а не технологии. «AI-агентов» после
// normToken даёт единственный токен «ai» (дефис срезан), и он находился в
// «AI-системы» письма — требование про агентские системы закрывалось без
// агентского опыта (живой кейс Fullstack/mistral, октябрь 2026). Такие токены
// уводят требование в концепт-путь, где судится понятие, а не метка.
var genericTokens = map[string]bool{
	"ai": true, "ml": true, "ii": true, "ui": true,
}

// conceptSignalCount — сколько сигналов концепта подтверждено в тексте
// (клаузы под отрицанием не считаются: честный пробел — не сигнал).
func conceptSignalCount(c Concept, text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	clauses := sentences(text)
	n := 0
	for _, s := range c.Signals {
		for i, cl := range clauses {
			if s.Re.MatchString(strings.ToLower(cl)) && !clauseNegated(clauses, i) {
				n++
				break
			}
		}
	}
	return n
}

// conceptPrimaryGap — требование нацелено на ПЕРВОЕ сработавшее понятие, и
// фактов этого понятия нет ни в письме, ни в профиле. Тогда закрытие соседним,
// более общим концептом нечестно.
//
// Живые кейсы (октябрь 2026): «Обеспечение быстрого и отказоустойчивого
// рендеринга видео» закрывалось концептом «эксплуатация и observability» (его
// триггер содержит «отказоустойчив», сигналами стали Prometheus/Grafana —
// дашборды, не видеорендеринг); «Улучшение логики AI-агентов» — концептом
// GenAI по «AI-системам». Понятия «медиа/видео» и «агентские системы» стоят
// в списке выше и обязаны быть проверены первыми.
func conceptPrimaryGap(concepts []Concept, reqText, letter, profile string) (string, bool) {
	low := strings.ToLower(reqText)
	for _, c := range concepts {
		if !c.Trigger.MatchString(low) {
			continue
		}
		if conceptSignalCount(c, letter) == 0 && conceptSignalCount(c, profile) == 0 {
			return c.Name, true
		}
		return "", false
	}
	return "", false
}

// adaptationListRe — буллет «перечисление стека вакансии», за которым идёт
// признание пробела: «TypeScript/React Native/Expo/Convex/Remotion/E2B: имею
// опыт fullstack…; готова оперативно освоить ваш стек». Такое перечисление
// называет стек РАБОТОДАТЕЛЯ, а не опыт кандидата.
var adaptationListRe = regexp.MustCompile(`(?i)^\s*[-*•]?\s*(?:[A-Za-z][A-Za-z0-9+#.-]*(?:\s+[A-Za-z][A-Za-z0-9+#.-]*)*\s*[/,]\s*){2,}[A-Za-z][A-Za-z0-9+#.-]*(?:\s+[A-Za-z][A-Za-z0-9+#.-]*)*\s*:`)

// adaptationHeadRe — та же форма, но от ДВУХ позиций: «Protobuf/JSON-RPC: …»
// и «JSON-RPC: …» тоже называют стек работодателя, а adaptationListRe их
// пропускал. Причина в живом кейсе АФЛТ-Системс: перечисление из двух
// технологий — самый частый вид буллета адаптации («Protobuf/JSON-RPC»,
// «TypeScript/React»), и именно он не распознавался.
var adaptationHeadRe = regexp.MustCompile(`(?i)^\s*[-*•]?\s*[A-Za-z][A-Za-z0-9+#.-]*(?:\s+[A-Za-z][A-Za-z0-9+#.-]*)*\s*(?:[/,]\s*[A-Za-z][A-Za-z0-9+#.-]*(?:\s+[A-Za-z][A-Za-z0-9+#.-]*)*)*\s*:`)

// adaptationListing — токен назван внутри буллета-перечисления стека вакансии,
// который в ТОЙ ЖЕ строке признаёт отсутствие опыта («готов/готова … освоить»).
//
// Живой кейс Fullstack/mistral (октябрь 2026): пять требований (TypeScript,
// React Native/Expo, Convex, Remotion, E2B) отмечались «закрыто в письме»,
// хотя письмо их только перечисляло. sentences() режет по «;», поэтому
// «готова оперативно освоить» оставалось в соседней клаузе и understandingRe
// в findFact не срабатывал. Проверка идёт по СТРОКЕ (буллету), а не по клаузе:
// так «Kafka: consumer groups» из соседнего буллета не задевается.
func adaptationClause(text, token, clause string) bool {
	cl := strings.TrimSpace(clause)
	if strings.TrimSpace(token) == "" || cl == "" {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		if !understandingRe.MatchString(line) {
			continue
		}
		// Правило 1 (голова буллета): технология названа ДО двоеточия, которое
		// открывает перечисление стека работодателя.
		if adaptationHeadRe.MatchString(line) {
			if idx := strings.Index(line, ":"); idx >= 0 && findText(token, line[:idx]) {
				return true
			}
		}
		// Правило 2 (прежнее): длинное перечисление (3+ позиции) — весь буллет
		// перечисляет стек вакансии, любая его клауза под адаптацию не факт.
		if !adaptationListRe.MatchString(line) {
			continue
		}
		if strings.Contains(line, cl) {
			return true
		}
	}
	return false
}

// firstHardMiss — текст первого незакрытого ТРЕБОВАНИЯ (не обязанности):
// совет «один must-have не закрыт» обязан называть именно требование.
func firstHardMiss(missing []Req) string {
	for _, c := range missing {
		if c.Kind != "duty" {
			return c.Text
		}
	}
	return ""
}

// dutyGaps — тексты незакрытых обязанностей (kind=duty).
func dutyGaps(missing []Req) []string {
	var out []string
	for _, c := range missing {
		if c.Kind == "duty" {
			out = append(out, "«"+c.Text+"»")
		}
	}
	return out
}
