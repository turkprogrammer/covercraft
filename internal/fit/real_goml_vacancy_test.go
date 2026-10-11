package fit

import (
	"strings"
	"testing"
)

// Реальная вакансия Golang-разработчик (октябрь 2026) — эталонная для
// роли go-primary с ML-требованиями. эталонное письмо (без строки
// контактов — no-leaks) обязано дать вердикт apply_with_caveats:
// все факты закрыты письмом, честные пробелы (k8s/RabbitMQ/MariaDB) —
// оговорки, не missing.
const realGoMLVacancy = `Golang-разработчик

Что по задачам?
• Разработка и поддержка backend-сервисов на Go;
• Проектирование архитектуры приложений и оптимизация производительности;
• Интеграция сервисов с различными API и базами данных;
• Участие в код-ревью и обеспечение качества кода;
• Совместная работа с командой разработчиков и QA;
• Observability и производительность: логирование, трассировка, метрики, профайлинг (CPU/mem/latency);
• CI/CD и платформа: поддержка и развитие пайплайнов, шаблонов сервисов, внутренних библиотек

Ключевые навыки: PostgreSQL, Git, Golang, Docker, Английский язык, MySQL, MariaDB,
CI/CD, Kafka, Redis, RabbitMQ, HTTP, REST API, WebSocket, DevOps, Go от 3-х лет,
Микросервисная архитектура, Проектирование ПО, best practices разработка,
open source компоненты, k8s, ML-модели`

// Имитация разбора LLM-шага: все ключевые навыки + обязанности как must.
var realGoMLMusts = []string{
	"Разработка и поддержка backend-сервисов на Go",
	"Проектирование архитектуры приложений",
	"Оптимизация производительности",
	"Интеграция сервисов с API и базами данных",
	"Участие в код-ревью",
	"Обеспечение качества кода",
	"Совместная работа с командой разработчиков и QA",
	"Observability: логирование, трассировка, метрики, профайлинг",
	"CI/CD: поддержка пайплайнов",
	"Шаблоны сервисов и внутренние библиотеки",
	"PostgreSQL",
	"Git",
	"Golang",
	"Docker",
	"MySQL",
	"MariaDB",
	"Kafka",
	"Redis",
	"RabbitMQ",
	"HTTP",
	"REST API",
	"WebSocket",
	"DevOps",
	"Go от 3-х лет",
	"Микросервисная архитектура",
	"Проектирование ПО",
	"best practices разработка",
	"open source компоненты",
	"k8s",
	"ML-модели",
}

// Профиль — реальный, как его собирает программа (LoadProfile читает все
// context/*.md). Тест верен только против реального артефакта: письмо
// генерируется из него, и факты, которых в нём нет, модель написать не может.
// (Контекст локальный, в git не попадает — см. .git/info/exclude.)
var realProfile = func() string {
	p := LoadProfile("../../context", nil)
	if p == "" {
		return ""
	}
	return p
}()

func loadRealProfile() string { return realProfile }

// эталонное письмо — эталон пользователя (октябрь 2026), без строки контактов.
const realGoMLLetter = `Здравствуйте! Меня заинтересовала ваша вакансия Golang-разработчик. Мой профиль (17 лет бэкенда, PHP с 2005, 3 года Go) закрывает задачу по разработке и поддержке backend-сервисов на Go, проектированию архитектуры и оптимизации производительности.

Чем могу быть полезен:

- **Go и highload:** Stable ID (Kafka, 10 000 RPS, at-least-once, идемпотентность через ClickHouse Upsert), Fraud Engine (Random Forest на Go, multi-tenancy, 92% F1, 1000+ RPS, P95 < 4.2ms), Bundle ID (3-уровневая классификация iOS/Android, 760–850 эл/с), Domain ID (IAB-таксономия, 416K+ доменов), Geo-mapping Service (Llama-3.3-70B-Instruct, успешность маппингов +150%, покрытие регионов 90%, снижение стоимости API на 70%, 3.1M+ записей); конкурентный код: sync.Mutex, channels, ProcessManager (20+ воркеров, lock-free accumulator, graceful shutdown); race detector — чист; профилирование: pprof (heap, cpu), GOGC, GOMEMLIMIT, GOMAXPROCS, gctrace.

- **ML-модели и их эксплуатация в production:** 5 production-сервисов с ML/GenAI-интеграцией — Fraud Engine (Custom Random Forest на чистом Go, 100 деревьев, hot-reload через fsnotify, Platt Scaling + SHAP-like Explainer, 92% F1, 1000+ RPS, P99 < 46ms), Stable ID (Random Forest, event-driven обучение через Kafka, A/B тестирование моделей traffic split 50/50, blue-green deploy моделей, 10 000 RPS), Bundle ID (каскад regex → ClickHouse → LLM DeepSeek R1, 8x ускорение, экономия токенов 60–70%), Domain ID (AI-классификатор IAB Content Taxonomy 3.1, 85% доменов без AI, 99.9% точности whitelist), Geo-mapping Service (Llama-3.3-70B-Instruct через Intelligence.io API, 407 городов, coverage 50% → 90%, снижение стоимости API на 70%).

- **System Design и отказоустойчивость:** проектирование архитектуры приложений: ADR (10), Hexagonal Architecture, DDD, SOLID/GRASP; оптимизация производительности (pprof, GOGC, GOMEMLIMIT, GOMAXPROCS, gctrace, буферизация, multi-level кэш); отказоустойчивость: retries (exponential backoff), deadlines (context.WithTimeout), идемпотентность (ClickHouse Upsert), консистентность данных, at-least-once (Kafka consumer groups), Circuit Breaker + token bucket rate limiting (gobreaker), graceful shutdown всех сервисов.

- **Командная работа, процессы, CI/CD:** уверенно владею Git (17 лет); совместная работа с командой разработчиков и QA, Agile (Scrum и Kanban — планирование спринтов, стендапы, груминг, ретроспективы, story points); regular code review (Go/PHP) + ADR; 155+ тестов (unit, integration, E2E); table-driven tests — стандарт; fuzzing — vpnctl; golangci-lint; Prometheus + Grafana (3 дашборда, 23 панели, алертинг P99 > 100ms / error rate > 1%), структурные логи, pprof (heap, cpu), gctrace; Docker Compose, systemd; запуск и поддержка готовых GitLab CI/CD пайплайнов (банк Росгосстрах); GitHub Actions (build + go test — CI написан сам).

- **Микросервисы, БД, брокеры, сеть:** интеграции сервисов с базами данных и API: микросервисы банка Росгосстрах (gRPC, Elasticsearch, Oracle); PostgreSQL (глубокое знание, тюнинг, индексы B-tree/GIN/partial/covering, execution plans, pg_stat_statements), MySQL 8.0, ClickHouse (Upsert, материализованные представления, Buffer/flush), Elasticsearch (лог-агрегация, Kibana, full-text search), Redis (кэш, multi-level cache memory → Postgres → disk, 1M+ записей), Kafka (consumer groups, partitioning, dead letter, at-least-once), WebSocket (Laravel + Ratchet, real-time чат), gRPC (микросервисы банка Росгосстрах), REST API; MySQL (Task Flow) покрывает реляционные сценарии.

Адаптация под ваш стек:

- Kubernetes: понимаю архитектуру оркестрации и принципы работы сервисов в кластере; прочная база в Docker Compose и GitLab CI/CD — переносится на K8s, Helm и OpenTelemetry; K8s не эксплуатировал, готов оперативно углубиться.

- Шаблоны сервисов и внутренние библиотеки: разработал собственные внутренние инструменты (CoverCraft — генератор писем на Go 1.26, SQL-Top — TUI-профайлер SQL, mt — runtime tuner; open-source MIT), запускал готовые GitLab CI/CD пайплайны (банк Росгосстрах); шаблоны сервисов и внутренние библиотеки — естественное расширение, готов развивать.

- OpenTelemetry/распределённая трассировка: Prometheus + Grafana (3 дашборда, 23 панели) + структурные логи — та же наблюдаемость; принципы трассировки понятны через event-driven журналы и Kafka; готов освоить оперативно.

- MariaDB: PostgreSQL/MySQL покрывают реляционные сценарии; MariaDB — естественное расширение, готов освоить оперативно.

- Kafka, RabbitMQ, брокеры: Kafka (consumer groups, partitioning, dead letter, at-least-once) — production; паттерны очередей переносимы на RabbitMQ (consumer groups ↔ queues, DLQ, routing); RabbitMQ — быстрый переход, готов освоить оперативно.

Стек: Go, PHP, ML, Kafka, Redis, PostgreSQL, MySQL, Docker, ClickHouse, Prometheus, Grafana, Linux, RabbitMQ`

func TestRealGoMLVacancy(t *testing.T) {
	reqs := mustReqs(realGoMLMusts, []string{"Английский язык"}, "go-primary")
	profile := loadRealProfile()
	if profile == "" {
		t.Skip("нет context/ — тест не имеет смысла (контекст локальный артефакт)")
	}
	res := Evaluate(DefaultConcepts(), reqs, profile, realGoMLLetter, realGoMLVacancy)

	// Честные пробелы (k8s, RabbitMQ, MariaDB, DevOps) — оговорки, не missing.
	if res.Verdict == Skip {
		t.Fatalf("вердикт skip — честные пробелы не должны ронять вердикт: score=%d, missing=%+v",
			res.Score, res.Missing)
	}

	wantCovered := []string{
		"WebSocket",
		"ML-модели",
		"код-ревью",
		"Проектирование архитектуры",
		"best practices",
		"RabbitMQ",
	}
	var coveredTexts []string
	for _, c := range res.Covered {
		coveredTexts = append(coveredTexts, c.Text)
	}
	for _, want := range wantCovered {
		ok := false
		for _, got := range coveredTexts {
			if strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("требование %q не в covered (закрывается письмом): covered=%v", want, coveredTexts)
		}
	}

	// Честные пробелы обязаны быть оговорками (bridge/honest gap), а не missing.
	// RabbitMQ здесь не проверяем: письмо называет его позитивно (стек + мост
	// через Kafka-паттерны), матчер засчитывает его закрытым — это верно.
	var caveatTexts []string
	for _, c := range res.Caveats {
		caveatTexts = append(caveatTexts, c.Text)
	}
	for _, want := range []string{"k8s", "MariaDB"} {
		ok := false
		for _, got := range caveatTexts {
			if strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("честный пробел %q не в caveats: caveats=%v, missing=%v", want, caveatTexts, res.Missing)
		}
	}

	if len(res.Missing) > 0 {
		t.Errorf("нет missing — все незакрытые требования честно оговорены в письме: missing=%+v", res.Missing)
	}

	t.Logf("verdict=%s score=%d, covered=%d caveats=%d missing=%d",
		res.Verdict, res.Score, len(res.Covered), len(res.Caveats), len(res.Missing))
}
