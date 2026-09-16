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
			{"алертинг по SLO", regexp.MustCompile(`(?i)алерт|p99|p95|error rate`)},
			{"rollback/отказоустойчивые релизы", regexp.MustCompile(`(?i)rollback|graceful|blue-green|откат|shutdown`)},
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
// сработавших сигналов.
func conceptHit(reqText, text string, minSignals int) (name string, hits []string) {
	lt := strings.ToLower(text)
	for _, c := range concepts {
		if !c.re.MatchString(strings.ToLower(reqText)) {
			continue
		}
		hits = nil
		for _, s := range c.signals {
			if s.re.MatchString(lt) {
				hits = append(hits, s.label)
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
}

// normToken приводит токен к каноническому имени по таблице синонимов.
func normToken(tok string) string {
	lt := strings.ToLower(strings.Trim(tok, ".-,#"))
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

// coverage — где требование закрыто. Порядок проверки: (1) технологии
// требования есть в тексте целиком; (2) требование без технологий — по
// концепт-признакам (≥2 сигнала). Источники по приоритету: письмо,
// профиль, мост, неизвестно.
func coverage(req Requirement, letter, profile string) (source, note string) {
	tokens := reqTokens(req.Text)
	if len(tokens) == 0 {
		// Концептное требование: ищем признаки в письме, потом в профиле.
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
		return SrcUnknown, "в требовании нет распознаваемых технологий и признаков — проверь вручную"
	}
	for _, src := range []struct {
		label, text string
	}{
		{SrcLetter, letter},
		{SrcProfile, profile},
	} {
		all := true
		for _, t := range tokens {
			if !findText(t, src.text) {
				all = false
				break
			}
		}
		if all {
			if src.label == SrcProfile {
				return SrcProfile, "в профиле есть факт, но в письмо не попал — впиши в письмо, закроется полностью"
			}
			return SrcLetter, "закрыто в письме"
		}
	}
	// Частичное покрытие: большинство токенов найдено, но не все
	// («Linux (systemd, cron)»: systemd в письме есть, cron нет). Требовать
	// ВСЕ токены — слишком строго: один неупомянутый термин роняет
	// требование в «не закрыто ничем», хотя ядро требования закрыто.
	// Правило большинства: ≥ половины токенов (и ≥2) — закрыто, недостающее
	// честно названо в ноте. Один токен — без изменений: либо есть, либо нет.
	for _, src := range []struct {
		label, text string
	}{
		{SrcLetter, letter},
		{SrcProfile, profile},
	} {
		var missing []string
		found := 0
		for _, t := range tokens {
			if findText(t, src.text) {
				found++
			} else {
				missing = append(missing, t)
			}
		}
		if found >= 2 && found > len(tokens)-found && len(missing) > 0 {
			if src.label == SrcProfile {
				return SrcProfile, "в профиле есть факт по большинству токенов (не упомянуты: " + strings.Join(missing, ", ") + ") — впиши в письмо, закроется полностью"
			}
			return SrcLetter, "закрыто в письме; не упомянуты: " + strings.Join(missing, ", ") + " — добавь"
		}
	}
	// Мост: по токенам требования (Kubernetes в bridges НЕ входит —
	// must-have «K8s в проде» без опыта не закрывается соседним опытом).
	for _, t := range tokens {
		if b, ok := bridges[t]; ok && b.re.MatchString(letter+profile) {
			return SrcBridge, b.note
		}
	}
	return "", ""
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

// Evaluate — детерминированный вердикт: покрытие must-have против письма
// и профиля, взвешенный скор и три состояния. Чистая функция: без LLM,
// без IO — тестируется на фикстурах.
//
// Веса покрытия (must-have): письмо 1.0, профиль 0.7 (совет «добавь в
// письмо»), мост 0.5 (слабое место), unknown 0.2 («проверь вручную»),
// не закрыто 0. Nice-to-have: закрыт — небольшой бонус, не закрыт — без
// штрафа. «Будет плюсом» и мягкие требования покрытием не считаются.
func Evaluate(reqs Requirements, profile, letter, vacancy string) Fit {
	f := Fit{Role: reqs.Role}
	if reqs.MustHave == nil && reqs.NiceToHave == nil {
		// Разбор не удался — вердикта быть не должно, панель не рендерится.
		f.Verdict = ""
		return f
	}

	sum, count := 0.0, 0.0
	for _, r := range reqs.MustHave {
		if plusRe.MatchString(r.Text) || softTerms.MatchString(r.Text) {
			continue // «будет плюсом» и мягкие не требуют покрытия
		}
		count++
		src, note := coverage(r, letter, profile)
		switch src {
		case SrcLetter:
			sum += 1.0
			f.Covered = append(f.Covered, Req{r.Text, src, note})
		case SrcProfile:
			sum += 0.7
			f.Covered = append(f.Covered, Req{r.Text, src, note})
			f.Advice = append(f.Advice, "в профиле есть факт по «"+r.Text+"», но в письмо он не попал — впиши в письмо, закроется полностью")
		case SrcBridge:
			sum += 0.5
			f.Caveats = append(f.Caveats, Req{r.Text, src, note})
			f.Advice = append(f.Advice, note+" — в письме и на собеседовании это будет слабое место")
		case SrcUnknown:
			sum += 0.2
			// Построчного совета нет: все unknown сведены в одну строку
			// после цикла (шесть одинаковых «проверь вручную» — шум).
			f.Caveats = append(f.Caveats, Req{r.Text, src, note})
		default:
			f.Missing = append(f.Missing, Req{r.Text, SrcMissing, "в профиле и письме нет, моста нет"})
			f.Advice = append(f.Advice, "обязательное требование «"+r.Text+"» не закрыто ничем — письмом это не лечится")
		}
	}

	// Nice-to-have: закрытый в письме — небольшой бонус, незакрытый — без штрафа.
	for _, r := range reqs.NiceToHave {
		if softTerms.MatchString(r.Text) {
			continue
		}
		if src, _ := coverage(r, letter, profile); src == SrcLetter {
			sum += 0.05 * count // бонус +5% от базы must-have за каждый
		}
	}

	if count > 0 {
		f.Score = int(sum/count*100 + 0.5)
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
			unkN++
		}
	}
	switch {
	case missN >= 2 || (missN == 1 && unkN >= 2) || unkN >= 3 || brN >= 3 || roleMismatch(reqs, profile):
		f.Verdict = Skip
	case missN == 1 || brN >= 1 || unkN >= 1:
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
	// unknown — одним сводным советом, а не построчно: шесть одинаковых
	// строк «проверь вручную» — шум, а не помощь.
	if unkN > 0 {
		var texts []string
		for _, c := range f.Caveats {
			if c.Source == SrcUnknown {
				texts = append(texts, "«"+c.Text+"»")
			}
		}
		f.Advice = append(f.Advice, "по "+strconv.Itoa(unkN)+" требовани"+unknownPlural(unkN)+" в профиле нет данных — проверь вручную, это не значит «опыта нет»: "+strings.Join(texts, ", "))
	}
	sort.SliceStable(f.Covered, func(i, j int) bool { return f.Covered[i].Source < f.Covered[j].Source })
	return f
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
