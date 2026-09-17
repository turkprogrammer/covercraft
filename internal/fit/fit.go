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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	"observability":   {regexp.MustCompile(`(?i)prometheus|grafana|мониторинг|трейсин|трейс`), "мост: опыт мониторинга метрик и дашбордов → observability"},
}

// softTerms — мягкие требования: не факты и не пробелы. Фит их не
// считает незакрытыми.
var softTerms = regexp.MustCompile(`(?i)самоорганиз|темп|стрессоустойч|командн|коммуника|внимательн|ответственн|инициативн`)

// signal — один признак концепта с человекочитаемой меткой для ноты.
type signal struct {
	label string
	re    *regexp.Regexp
}

// concept — концептное требование: навык, названный словами, а не
// технологией («опыт проектирования распределённых систем»). Токенов у
// такого требования нет, но его можно закрыть ПО ПРИЗНАКАМ в письме/
// профиле: сигналы — конкретные технологии и факты, из которых концепт
// следует. Зачёт только при ≥2 разных сигналах: одно слово — ещё не
// концепт.
type concept struct {
	re      *regexp.Regexp // где требование говорит об этом концепте
	signals []signal       // чем концепт проявляется в тексте
	note    string         // человеческое имя концепта
}

// concepts — таблица концептов. Применяется ТОЛЬКО к требованиям без
// распознаваемых технологий: у требования с токенами (Kubernetes, Kafka)
// своя честная проверка, и признаки не должны маскировать реальный пробел
// (письмо с Prometheus+Grafana не закрывает must-have «K8s в проде»).
var concepts = []concept{
	{
		regexp.MustCompile(`(?i)распредел[её]нн|event-driven|микросервисн|микросервис`),
		[]signal{
			{"Kafka/брокеры сообщений", regexp.MustCompile(`(?i)\bkafka\b|очеред|брокер`)},
			{"event-driven паттерны", regexp.MustCompile(`(?i)event-driven|consumer group|at-least-once|партици|offset|dead letter`)},
			{"микросервисы/RPC", regexp.MustCompile(`(?i)микросервис|grpc|rpc`)},
		},
		"распределённые системы",
	},
	{
		regexp.MustCompile(`(?i)высоконагруж|нагруженн|критичн|highload|производительн`),
		[]signal{
			{"нагрузочные метрики RPS/QPS", regexp.MustCompile(`(?i)\brps\b|\bqps\b|highload|запросов в секунду`)},
			{"латентность P99/P95", regexp.MustCompile(`(?i)\bp99\b|\bp95\b|latency|эл/с`)},
			{"идемпотентность/лимиты", regexp.MustCompile(`(?i)идемпотент|rate limit|нагрузочн`)},
		},
		"высоконагруженные системы",
	},
	{
		regexp.MustCompile(`(?i)эксплуатац|мониторинг|отказоустойч|деградац|observability|наблюдае`),
		[]signal{
			{"Prometheus/Grafana", regexp.MustCompile(`(?i)prometheus|grafana|метрик|монитор`)},
			{"алертинг по SLO", regexp.MustCompile(`(?i)алерт|p99|p95|error rate|дашборд`)},
			{"SQL-Top/профайлинг", regexp.MustCompile(`(?i)sql.?top|профайлер|pg_stat|explain`)},
		},
		"эксплуатация и observability",
	},
	{
		regexp.MustCompile(`(?i)архитектурн|архитектор|техническое ревью|code review|прототип`),
		[]signal{
			{"ADR", regexp.MustCompile(`(?i)\badr\b|архитектурн`)},
			{"архитектурные стили/ревью", regexp.MustCompile(`(?i)hexagonal|ddd|ревью|review`)},
			{"прототипы/стандарты", regexp.MustCompile(`(?i)прототип|стандарт|рефакторин`)},
		},
		"архитектурное управление",
	},
	{
		regexp.MustCompile(`(?i)модернизац|legacy|наследи`),
		[]signal{
			{"миграции", regexp.MustCompile(`(?i)миграц|legacy`)},
			{"модернизация без даунтайма", regexp.MustCompile(`(?i)модернизац|рефакторин|даунтайм`)},
		},
		"модернизация legacy",
	},
	{
		regexp.MustCompile(`(?i)многопоточн|мультипоточн|межпроцесс|межпоточн|диспетчеризац|синхронизац|параллельн|конкурентн|жизненн.{0,12}цикл|goroutine`),
		[]signal{
			{"горутины/каналы", regexp.MustCompile(`(?i)горутин|канал|goroutine|channel|воркер|worker`)},
			{"lock-free/синхронизация", regexp.MustCompile(`(?i)lock-free|lockfree|atomic|mutex|мьютекс|блокировк|синхронизац`)},
			{"диспетчеризация/жизненный цикл", regexp.MustCompile(`(?i)processmanager|process manager|диспетчериз|graceful|пул|pool|shutdown`)},
		},
		"многопоточность и жизненный цикл",
	},
	{
		regexp.MustCompile(`(?i)ооп|solid|паттерн|проектирова.{0,15}шаблон|принципы`),
		[]signal{
			{"SOLID/GRASP", regexp.MustCompile(`(?i)solid|grasp|ооп|объектно-ориент`)},
			{"паттерны/архитектурные стили", regexp.MustCompile(`(?i)паттерн|шаблон|hexagonal|ddd|strategy|слой|layer`)},
		},
		"ООП/SOLID/паттерны",
	},
	{
		regexp.MustCompile(`(?i)алгоритм|структур.{0,15}данн`),
		[]signal{
			{"алгоритмы/данные в проектах", regexp.MustCompile(`(?i)очеред|приоритет|индекс|классификац|алгоритм|дерев|кэш|хеш`)},
			{"нагрузочная практика", regexp.MustCompile(`(?i)rps|p99|p95|throughput|эл/с`)},
		},
		"алгоритмы и структуры данных",
	},
	{
		regexp.MustCompile(`(?i)backend|бэкенд|бекенд|серверн`),
		[]signal{
			{"серверные языки", regexp.MustCompile(`(?i)\bgo\b|\bgolang\b|php|python|java`)},
			{"API/сервисы", regexp.MustCompile(`(?i)api|grpc|http|сервис`)},
		},
		"backend-разработка",
	},
	{
		// Живое требование платёжной вакансии: «Гарантии консистентности и
		// идемпотентности» — латиницы в нём нет, поэтому раньше уходило в
		// unknown, хотя письмо закрывает его словами (at-least-once,
		// идемпотентный Upsert, idempotency keys).
		regexp.MustCompile(`(?i)консистентн|идемпотентн|целостн|гарантии доставки|exactly-once`),
		[]signal{
			{"at-least-once/досылка", regexp.MustCompile(`(?i)at-least-once|at least once|досылк|буфериз|retry|повторн`)},
			{"идемпотентность/Upsert", regexp.MustCompile(`(?i)идемпотент|idempotency|upsert`)},
			{"транзакции/fail-closed", regexp.MustCompile(`(?i)транзакц|fail-closed|exactly-once`)},
		},
		"гарантии консистентности и идемпотентности",
	},
	{
		// Живой кейс платёжной вакансии: «Интеграция с платёжными
		// процессингами» — латиницы нет, токенов нет, но по сигналам
		// (банк/финтех, транзакции, платежи) письмо его закрывает.
		regexp.MustCompile(`(?i)платёжн|процессинг|эквайринг|payment|acquiring`),
		[]signal{
			{"финтех/банкинг", regexp.MustCompile(`(?i)финтех|fintech|банкинг|банковск|эквайринг`)},
			{"платежи/payment", regexp.MustCompile(`(?i)платёж|платеж|payment|биллинг|billing`)},
			{"процессинг/шлюзы", regexp.MustCompile(`(?i)процессинг|шлюз|gateway|webhook|acquiring|эквайринг`)},
		},
		"интеграция с платёжными процессингами",
	},
}

// conceptHonestGap — концептное требование честно названо пробелом: тема
// требования (её regex) упомянута в письме, но только в клаузах под
// отрицанием («С платёжными процессингами не работал»). Это не «нет данных»,
// а раскрытый кандидатом пробел — вердикт не должен наказывать честность
// сильнее, чем молчание.
func conceptHonestGap(reqText, letter string) (string, bool) {
	clauses := sentences(letter)
	for _, c := range concepts {
		if !c.re.MatchString(strings.ToLower(reqText)) {
			continue
		}
		negated := false
		for i, cl := range clauses {
			if !c.re.MatchString(strings.ToLower(cl)) {
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
			return "в письме честно назван пробел («" + c.note + "») — для вердикта это «нет данных»", true
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
func conceptHit(reqText, text string, minSignals int) (name string, hits []string) {
	clauses := sentences(text)
	for _, c := range concepts {
		if !c.re.MatchString(strings.ToLower(reqText)) {
			continue
		}
		hits = nil
		for _, s := range c.signals {
			// Ищем сигнал в клаузах: засчитываем, только если он не под отрицанием.
			for i, cl := range clauses {
				if s.re.MatchString(strings.ToLower(cl)) && !clauseNegated(clauses, i) {
					hits = append(hits, s.label)
					break // один хит на сигнал достаточно
				}
			}
		}
		if len(hits) >= minSignals {
			return c.note, hits
		}
	}
	return "", nil
}

// plusRe — «будет плюсом»: такое требование не требует покрытия.
var plusRe = regexp.MustCompile(`(?i)будет плюсом|желательно|nice to have`)

// tokenRe — латинские токены (названия технологий) в тексте требования.
var tokenRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+#.-]{1,30}`)

// stopwords — общеупотребительные слова, не технологии.
var stopwords = map[string]bool{
	"the": true, "and": true, "or": true, "of": true, "in": true,
	"at": true, "to": true, "with": true, "for": true, "a": true,
	"senior": true, "middle": true, "junior": true, "lead": true,
	"years": true, "year": true, "experience": true, "work": true,
	"com": true, "http": true, "https": true, "www": true,
	"api": true, "sql": true, "rest": true, "rpc": true,
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
		if t == "" || stopwords[t] || len(t) < 2 {
			continue
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// findText ищет токен в тексте. Кириллические альты ищем просто
// подстрокой (без границ слова — там падежи: «Kubernetes», «Kubernetesа»),
// латиницу — с границами, чтобы «go» не ловился внутри «golang».
func findText(token, text string) bool {
	lt := strings.ToLower(text)
	alts := append([]string{token}, synonyms[token]...)
	for _, a := range alts {
		if hasCyrillic(a) {
			if strings.Contains(lt, strings.ToLower(a)) {
				return true
			}
			continue
		}
		re := regexp.MustCompile(`(?i)(^|[^a-zа-я0-9])` + regexp.QuoteMeta(a) + `([^a-zа-я0-9]|$)`)
		if re.MatchString(lt) {
			return true
		}
	}
	return false
}

// negRe — маркеры отрицания опыта в предложении: письмо, честно
// называющее пробел («С OpenTelemetry опыта нет, готов освоить»), не
// должно считаться закрытием требования — это живой кейс, когда честное
// письмо получало «закрыто в письме» по голой подстроке.
var negRe = regexp.MustCompile(`(?i)опыта нет|нет опыта|опыта\s+(?:\S+\s+)?нет|не работал|не использ|отсутствует|не зафиксирован|не применял|готов освоить|освою|не приходилось`)

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
var bareNegRe = regexp.MustCompile(`(?i)^\s*(опыта нет|нет опыта|опыта\s+(?:\S+\s+)?нет|не работал[а-яё]*|не использ\w*|отсутствует|не зафиксирован|не применял|не приходилось)\s*[.!]?\s*$`)

// clauseNegated — клауза под отрицанием: маркер в ней самой или в
// следующей клаузе, если та состоит из одного маркера.
func clauseNegated(clauses []string, i int) bool {
	if negRe.MatchString(clauses[i]) {
		return true
	}
	return i+1 < len(clauses) && bareNegRe.MatchString(clauses[i+1])
}

// findFact — токен назван в тексте как факт: существует клауза, где токен
// есть, а отрицания нет.
func findFact(token, text string) bool {
	clauses := sentences(text)
	for i, c := range clauses {
		if findText(token, c) && !clauseNegated(clauses, i) {
			return true
		}
	}
	return false
}

// tokenNegatedOnly — токен встречается в тексте, но только с отрицанием
// (ни одного «фактового» вхождения).
func tokenNegatedOnly(token, text string) bool {
	return findText(token, text) && !findFact(token, text)
}

// matchTokens — покрытие требования по токенам в одном источнике: сначала
// «все токены», затем правило большинства («Linux (systemd, cron)»: systemd
// есть, cron нет — ядро требования закрыто, недостающее названо в ноте; один
// неупомянутый термин не роняет требование в missing; один токен — либо есть,
// либо нет). Проверка идёт «источник за источником»: порядок важен, иначе
// «профиль закрывает все токены» бьёт «письмо закрывает большинство» и
// выдаётся совет «впиши в письмо» при уже закрытом письме (живой кейс
// DDD + Hexagonal: письмо даёт ddd/hexagonal/architecture, нет только api).
//
// Токен, честно отрицанный в письме («Transactional outbox не использовал»),
// не засчитывается нигде — включая профиль: честный пробел письма
// приоритетнее любого факта профиля, иначе matcher советует вписать
// неприменённый опыт.
func matchTokens(tokens []string, src, text, letter string) (string, string, bool) {
	all := true
	for _, t := range tokens {
		if !countsAsFact(t, src, text, letter) {
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
	found := 0
	for _, t := range tokens {
		if countsAsFact(t, src, text, letter) {
			found++
		} else {
			missing = append(missing, t)
		}
	}
	// Majority может закрыть, только если missing не содержит
	// честно отрицаемых токенов: «почти всё, но один честно назван
	// пробелом» — это unknown, а не letter/profile.
	for _, mt := range missing {
		if tokenNegatedOnly(mt, letter) {
			if note, ok := honestGapNote(tokens, letter); ok {
				return "", note, false
			}
		}
	}
	if found >= 2 && found > len(tokens)-found && len(missing) > 0 {
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
func countsAsFact(t, src, text, letter string) bool {
	if !findFact(t, text) || tokenNegatedOnly(t, letter) {
		return false
	}
	return src != SrcProfile || !declinedInProfile(t, text)
}

// declinedInProfile — токен назван в отрицающей клаузе профиля: это
// ограничитель («НЕ использовал», «опыта нет»), а не факт для письма.
func declinedInProfile(token, profile string) bool {
	clauses := sentences(profile)
	for i, c := range clauses {
		if findText(token, c) && clauseNegated(clauses, i) {
			return true
		}
	}
	return false
}

// coverage — где требование закрыто. Порядок проверки: (1) технологии
// требования есть в тексте целиком; (2) требование без технологий — по
// концепт-признакам (≥2 сигнала). Источники по приоритету: письмо,
// профиль, мост, неизвестно.
func coverage(req Requirement, letter, profile string) (source, note string) {
	tokens := reqTokens(req.Text)
	if len(tokens) == 0 {
		// Концептное требование: проверяем честный пробел до проверки признаков,
		// чтобы «С платёжными процессингами не работал» не засчитался как покрытие.
		if note, ok := conceptHonestGap(req.Text, letter); ok {
			return SrcUnknown, note
		}
		// Ищем признаки в письме, потом в профиле.
		for _, src := range []struct {
			label, text string
		}{
			{SrcLetter, letter},
			{SrcProfile, profile},
		} {
			if name, hits := conceptHit(req.Text, src.text, 2); name != "" {
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
		if s, note, ok := matchTokens(tokens, src.label, src.text, letter); ok {
			return s, note
		}
	}
	// Честный пробел: токены требования названы в письме, но только
	// с отрицанием («С OpenTelemetry опыта нет, готов освоить»). Это не
	// закрытие — но и не «в письме нет вообще»: модель отработала чек-лист,
	// пробел назван словами. unknown с человеческой нотой, не missing.
	if note, ok := honestGapNote(tokens, letter); ok {
		return SrcUnknown, note
	}
	// Мост: по токенам требования (Kubernetes в bridges НЕ входит —
	// must-have «K8s в проде» без опыта не закрывается соседним опытом).
	if note, ok := bridgeHit(tokens, letter, profile); ok {
		return SrcBridge, note
	}
	return "", ""
}

// honestGapNote — нота честного пробела: токены требования названы в письме
// только с отрицанием. Если часть токенов в письме есть позитивно, нота это
// называет («в письме есть postgresql, но outbox честно назван пробелом») —
// иначе кажется, что не упомянуто вообще ничего. ok=false, когда честно
// отрицанных токенов нет.
func honestGapNote(tokens []string, letter string) (string, bool) {
	var negTokens, posTokens []string
	for _, t := range tokens {
		switch {
		case tokenNegatedOnly(t, letter):
			negTokens = append(negTokens, t)
		case findFact(t, letter):
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

// LoadProfile читает context/*.md для матчинга фита (тот же набор
// файлов, что cover.BuildUserPrompt кладёт в промпт, — в сыром виде).
// Ошибка/отсутствие папки — не фатально: матчинг идёт по пустой строке.
func LoadProfile(contextDir string) string {
	var b strings.Builder
	entries, err := os.ReadDir(contextDir)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
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
		b.Write(raw)
	}
	return b.String()
}

// fitBuilder собирает Fit по мере обхода must-have: держит сумму весов,
// базу скора и отложенные советы, чтобы детерминированный матчер (Evaluate)
// и гибридный LLM-путь (MapCoverage) делили один скоринг, вердикт и советы.
// JSON-представление Fit не меняется: служебные поля живут в билдере.
type fitBuilder struct {
	f             Fit
	sum           float64
	count         int
	missingAdvice []string
}

// addMust — вклад одного must-have: веса 1.0 (письмо), 0.7 (профиль),
// 0.5 (мост), 0.2 (unknown), 0 (не закрыто) плюс советы.
func (b *fitBuilder) addMust(r Requirement, src, note string) {
	b.count++
	switch src {
	case SrcLetter:
		b.sum += 1.0
		b.f.Covered = append(b.f.Covered, Req{r.Text, src, note})
	case SrcProfile:
		b.sum += 0.7
		b.f.Covered = append(b.f.Covered, Req{r.Text, src, note})
		b.f.Advice = append(b.f.Advice, "в профиле есть факт по «"+r.Text+"», но в письмо он не попал — впиши в письмо, закроется полностью")
	case SrcBridge:
		b.sum += 0.5
		b.f.Caveats = append(b.f.Caveats, Req{r.Text, src, note})
		b.f.Advice = append(b.f.Advice, note+" — в письме и на собеседовании это будет слабое место")
	case SrcUnknown:
		b.sum += 0.2
		// Построчного совета нет: все unknown сведены в одну строку
		// после обхода (шесть одинаковых «проверь вручную» — шум).
		b.f.Caveats = append(b.f.Caveats, Req{r.Text, src, note})
	default:
		b.f.Missing = append(b.f.Missing, Req{r.Text, SrcMissing, "в профиле и письме нет, моста нет"})
		b.missingAdvice = append(b.missingAdvice, "обязательное требование «"+r.Text+"» не закрыто ничем — письмом это не лечится")
	}
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
		if src, _ := coverage(r, letter, profile); src == SrcLetter {
			b.sum += 0.05 * float64(b.count) // бонус +5% от базы must-have за каждый
		}
	}

	if b.count > 0 {
		f.Score = int(b.sum/float64(b.count)*100 + 0.5)
	}
	if f.Score > 100 {
		f.Score = 100
	}

	// Вердикт из той же таблицы покрытия, что и скор: противоречить
	// друг другу они не могут. unknown не роняет вердикт сам по себе:
	// это «нет данных», а не «нет опыта» — из шести unknown при нулевом
	// missing нельзя делать вывод «не откликаться» (реальный кейс
	// архитекторской вакансии). Один missing при закрытом остальном —
	// не «не откликаться», а серая зона: скор ~90%+ при skip —
	// противоречие в плашке (реальный кейс анти-DDoS: 8 из 9 закрыто,
	// один пробел UDP/TCP). skip — два и более пробела, пробел плюс
	// массовое unknown, роль другого профиля.
	missN, unkN, brN := len(f.Missing), 0, 0
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
	case missN >= 2 || (missN == 1 && unkN >= 2) || unkN >= 3 || roleMismatch(reqs, profile):
		f.Verdict = Skip
	case missN == 1 || brN >= 1 || unkN >= 1 || len(f.Caveats) > 0:
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
		if missN == 1 {
			f.Advice = append([]string{"откликаться с оговоркой: один must-have не закрыт («" + f.Missing[0].Text + "») — оцени, критичен ли он для этой вакансии"}, f.Advice...)
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
	if !(f.Verdict == Caveats && missN == 1) {
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
// не закрыто 0. Nice-to-have: закрыт — небольшой бонус, не закрыт — без
// штрафа. «Будет плюсом» и мягкие требования покрытием не считаются.
func Evaluate(reqs Requirements, profile, letter, vacancy string) Fit {
	b := &fitBuilder{f: Fit{Role: reqs.Role}}
	if reqs.MustHave == nil && reqs.NiceToHave == nil {
		// Разбор не удался — вердикта быть не должно, панель не рендерится.
		b.f.Verdict = ""
		return b.f
	}
	for _, r := range reqs.MustHave {
		if plusRe.MatchString(r.Text) || softTerms.MatchString(r.Text) {
			continue // «будет плюсом» и мягкие не требуют покрытия
		}
		src, note := coverage(r, letter, profile)
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
	goCand := regexp.MustCompile(`(?i)go-разработчик|golang|основн.{0,15}\bgo\b`).MatchString(profile)
	phpCand := regexp.MustCompile(`(?i)php-разработчик|основн.{0,15}php`).MatchString(profile)
	return phpRole && goCand && !phpCand
}
