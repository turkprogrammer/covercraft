package fit

import (
	"regexp"
	"testing"
)

// ---------------------------------------------------------------------------
// No-leaks: корпус не содержит email/телефон/URL
// ---------------------------------------------------------------------------
var leakRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|[+]7[\s\-\d]{10}|https?://`)

func TestGolden_NoLeaks(t *testing.T) {
	for _, c := range cases {
		for _, s := range []string{c.Vacancy, c.Profile, c.Letter} {
			if m := leakRe.FindString(s); m != "" {
				t.Errorf("case %q: утечка — %q", c.Name, m)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Golden runner
// ---------------------------------------------------------------------------
func TestGolden(t *testing.T) {
	var pass, knownGap int
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			reqs := mustReqs(c.MustHave, c.NiceToHave, c.Role)
			res := Evaluate(DefaultConcepts(), reqs, c.Profile, c.Letter, c.Vacancy)
			got := res.Verdict
			// Нормализуем: "apply_with_caveats" → "caveats" для сравнения
			if got == "apply_with_caveats" {
				got = "caveats"
			}
			if got != c.Want {
				if c.Ideal != "" && got == c.Ideal {
					// случай починен — не регресс
				} else {
					t.Fatalf("вердикт: got %q, want %q (score=%d, role=%s)\n"+
						"missing=%v, caveats=%v",
						got, c.Want, res.Score, res.Role,
						res.Missing, res.Caveats)
				}
			}
			if c.Ideal != "" && got == c.Ideal {
				t.Logf("ПОЧИНЕНО: кейс %q — ожидалось %q, сейчас %q (score=%d)",
					c.Name, c.Ideal, got, res.Score)
			}
			if c.Ideal != "" && got != c.Ideal {
				knownGap++
				t.Logf("KNOWN_GAP: кейс %q — ожидалось %q, получено %q (score=%d)",
					c.Name, c.Ideal, got, res.Score)
			}
			pass++
		})
	}
	t.Logf("ИТОГО: %d кейсов пройдено, %d known gap", pass, knownGap)
}

// ---------------------------------------------------------------------------
// Профиль — санитизированная реконструкция реального context/
// Без email, телефона, URL — только факты.
// ---------------------------------------------------------------------------
const profileSanitized = "PHP с 2005 (17 лет), Go — 3 года. " +
	"Laravel (laravel-api, LaravelShop, LaravelBlog), Lumen — production. " +
	"Symfony 7.2/8.0 (E-commerce-Lite, URL-Shortener), Yii2 — коммерческий production. " +
	"PHPUnit + TDD, E2E API тесты, Red-Green-Refactor. " +
	"PHPStan Level 6, Rector. " +
	"Go: Fraud Engine (multi-tenancy, Random Forest), Stable ID (Kafka, 10 000 RPS). " +
	"PostgreSQL: SQL-Top (pg_stat_statements, Safe EXPLAIN). " +
	"Redis — кэш. " +
	"MySQL 8.0+, тюнинг индексов B-tree/GIN/partial/covering. " +
	"WebSocket (Laravel + Ratchet), HTTP. " +
	"Git (17 лет), Git Flow. " +
	"Code review, 10 ADR. " +
	"End-to-end ответственность за сервисы. " +
	"LLM-интеграции: RAG CLI System, DeepSeek R1, Llama-3.3-70B. " +
	"Docker Compose, Prometheus, Grafana. " +
	"С Asterisk, WebRTC, sip.js, Nest.JS, Vue.js, Evolution CMS, MariaDB, SCSS/SASS, Vite, Kotlin не работал."

// ---------------------------------------------------------------------------
// Корпус кейсов (~10 штук, все из реальной истории 0.2.0–0.2.3)
// MustHave / NiceToHave / Role — ручная разметка (как в fit_test.go)
// Want  — эталонный вердикт (подтверждённое поведение)
// Ideal — если текущее поведение неверно: что должно быть (пусто = текущее ок)
// ---------------------------------------------------------------------------

type goldenCase struct {
	Name       string
	Vacancy    string
	MustHave   []string
	NiceToHave []string
	Role       string
	Profile    string
	Letter     string
	Want       string // "apply" | "caveats" | "skip"
	Ideal      string // пусто = want верен; иначе = что должно быть после фикса
}

var cases = []goldenCase{

	// 1. Все must-have закрыты — apply
	{
		Name:     "all-covered-apply",
		Vacancy:  "Ищем PHP-разработчика. Требуется: опыт Laravel от 3 лет, знание PostgreSQL, работа с REST API, Git.",
		MustHave: []string{"Опыт Laravel от 3 лет", "Знание PostgreSQL", "Работа с REST API", "Git"},
		Role:     "php-primary",
		Profile:  profileSanitized,
		Letter:   "Laravel (laravel-api, LaravelShop, LaravelBlog), PHP 8.x, production; REST API, PostgreSQL, Git (17 лет).",
		Want:     "apply",
	},

	// 2. Один missing + Docker распознаётся как bridge через Kafka — apply
	// (Docker не упомянут в письме, но есть косвенная связь через infra-опыт)
	// Это_known_gap: должен быть caveats, но текущий код даёт apply(93).
	{
		Name:     "one-missing-docker",
		Vacancy:  "Требуется: Go опыт от 2 лет, Kafka, PostgreSQL, Docker.",
		MustHave: []string{"Go опыт от 2 лет", "Kafka", "PostgreSQL", "Docker"},
		Role:     "go-primary",
		Profile:  profileSanitized,
		Letter:   "Go (Fraud Engine, Stable ID), Kafka (10 000 RPS), PostgreSQL, Redis, MySQL.",
		Want:     "apply",
		Ideal:    "caveats", // Docker — missing, должен быть caveats
	},

	// 3. Массовый unknown без missing — caveats (регресс 0.2.3, ПОЧИНЕНО)
	{
		Name:     "mass-unknown-no-missing-caveats",
		Vacancy:  "PHP 8.3, Laravel, PostgreSQL, Redis, Git Flow, REST API, Asterisk, WebRTC, Go.",
		MustHave: []string{"PHP 8.3", "Laravel", "PostgreSQL", "Redis", "Git Flow", "REST API", "Asterisk", "WebRTC", "Go"},
		Role:     "php-primary",
		Profile:  profileSanitized,
		Letter:   "PHP 8.3, Laravel, PostgreSQL, Redis, Git Flow, REST API. С Asterisk и WebRTC не работал.",
		Want:     "caveats",
	},

	// 4. Честный пробел WebRTC — caveats (не skip)
	{
		Name:     "honest-gap-not-skip",
		Vacancy:  "Требуется: Laravel, PHP 8.3, PostgreSQL, Redis, Git, понимание WebRTC.",
		MustHave: []string{"Laravel", "PHP 8.3", "PostgreSQL", "Redis", "Git", "Понимание WebRTC"},
		Role:     "php-primary",
		Profile:  profileSanitized,
		Letter:   "Laravel, PHP 8.3, PostgreSQL, Redis, Git. С WebRTC не работал, готов освоить.",
		Want:     "caveats",
	},

	// 5. Мост (bridge) — caveat
	{
		Name:     "bridge-not-fact",
		Vacancy:  "Требуется: Kafka, ClickHouse, Go опыт.",
		MustHave: []string{"Kafka", "ClickHouse", "Go опыт"},
		Role:     "go-primary",
		Profile:  profileSanitized,
		Letter:   "Kafka (Stable ID, 10 000 RPS), Go (Fraud Engine, Stable ID). С ClickHouse не работал.",
		Want:     "caveats",
	},

	// 6. 3 missing (Kubernetes, gRPC, ClickHouse) — skip
	{
		Name:     "role-mismatch-go-vacancy",
		Vacancy:  "Ищем Go-разработчика для highload-сервиса. Kubernetes, gRPC, ClickHouse.",
		MustHave: []string{"Go опыт", "Kubernetes", "gRPC", "ClickHouse"},
		Role:     "go-primary",
		Profile:  profileSanitized,
		Letter:   "Go (Fraud Engine, Stable ID), Kafka, PostgreSQL, Redis.",
		Want:     "skip",
	},

	// 7. Negation клаузная (outbox-регресс 0.2.2, ПОЧИНЕНО)
	{
		Name:     "negation-clausal-outbox",
		Vacancy:  "Требуется: опыт с Transactional outbox на PostgreSQL, Go, Kafka.",
		MustHave: []string{"Опыт с Transactional outbox на PostgreSQL", "Go", "Kafka"},
		Role:     "go-primary",
		Profile:  "Go, Kafka, PostgreSQL (глубокое знание). Transactional outbox на PostgreSQL: НЕ использовал.",
		Letter:   "Go, Kafka, PostgreSQL (глубокое знание). С Transactional outbox не работал.",
		Want:     "caveats",
	},

	// 8. Факт в профиле, не в письме — apply (must-have закрыт профилем),
	// но fit-fix должен предложить добавить в письмо. Это задача audit, не fit.
	{
		Name:     "fact-in-profile-not-in-letter",
		Vacancy:  "Ищем PHP-разработчика. Требуется: Laravel, PHPUnit, PostgreSQL, Redis.",
		MustHave: []string{"Laravel", "PHPUnit", "PostgreSQL", "Redis"},
		Role:     "php-primary",
		Profile:  profileSanitized,
		Letter:   "Laravel (laravel-api, LaravelShop, LaravelBlog), PostgreSQL, Redis.",
		Want:     "apply",
		Ideal:    "caveats", // PHPUnit в профиле есть, но не в письме → должен быть совет fit-fix
	},

	// 9. Клаузная негация: PostgreSQL выживает (ПОЧИНЕНО)
	{
		Name:     "clausal-negation-postgres-survives",
		Vacancy:  "Требуется: опыт работы с PostgreSQL, Transactional outbox, Kafka.",
		MustHave: []string{"Опыт работы с PostgreSQL", "Transactional outbox", "Kafka"},
		Role:     "go-primary",
		Profile:  "PostgreSQL (глубокое знание). Transactional outbox на PostgreSQL: НЕ использовал. Kafka (Stable ID).",
		Letter:   "PostgreSQL (глубокое знание), Kafka (Stable ID, 10 000 RPS). С outbox не работал.",
		Want:     "caveats",
	},

	// 10. All covered + high score — apply
	{
		Name:     "all-covered-100",
		Vacancy:  "PHP 8.3, Laravel, PostgreSQL, Redis, Git Flow, REST API.",
		MustHave: []string{"PHP 8.3", "Laravel", "PostgreSQL", "Redis", "Git Flow", "REST API"},
		Role:     "php-primary",
		Profile:  profileSanitized,
		Letter:   "PHP 8.3, Laravel (laravel-api), PostgreSQL (SQL-Top, pg_stat_statements), Redis, Git Flow, REST API.",
		Want:     "apply",
	},
}
