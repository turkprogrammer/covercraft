//go:build live

// Живой smoke-тест гибридного матчинга: требует настроенного LLM-endpoint
// в ~/.config/covercraft/settings.json. Запуск: go test -tags live ./internal/fit/

package fit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/turkprogrammer/covercraft/internal/llm"
	"github.com/turkprogrammer/covercraft/internal/settings"
)

func TestTmpLiveHybrid(t *testing.T) {
	s, err := settings.Load()
	if err != nil || s.Model == "" {
		t.Skipf("нет настроек модели: %v", err)
	}
	c := llm.Client{BaseURL: s.BaseURL, APIKey: s.APIKey, Model: s.Model, ReasoningEffort: s.ReasoningEffort, Timeout: 120 * time.Second}
	fn := LLMFunc(func(ctx context.Context, system, user string) (string, error) { return c.Generate(ctx, system, user) })

	vacancy := "Стек: Go — основной; PostgreSQL (inxbox/outbox); Logbroker (Kafka-like) — event bus; OpenTelemetry — трейсинг (HTTP, SQL, бизнес-логика); рядом Python-бэкенд. Задачи: развивать микросервис транзакций от создания платежа до терминального статуса с гарантиями консистентности и идемпотентности; интеграции с платёжными процессингами; event-driven цепочки через transactional outbox; DDD + Hexagonal Architecture; выстраивать observability."
	letter := "Здравствуйте! Профиль закрывает задачу по развитию микросервиса транзакций.\n" +
		"- Go и highload: Stable ID (Kafka, 10 000 RPS, at-least-once, идемпотентность через ClickHouse Upsert).\n" +
		"- Event-driven цепочки и гарантии консистентности: ProcessManager, 20+ воркеров, at-least-once, буферизованный продюсер (досылка при недоступности Kafka).\n" +
		"- DDD + Hexagonal Architecture: E-commerce-Lite (8 entities, 6 ports), Fraud Engine (domain/application/adapters).\n" +
		"- PostgreSQL: транзакции, индексы B-tree/GIN, autovacuum, репликация.\n" +
		"Честно о пробелах:\n" +
		"- OpenTelemetry — опыта интеграции нет; observability строил через Prometheus + Grafana, готов освоить.\n" +
		"- Transactional outbox не использовал; близкий опыт — событийный журнал в БД с polling-потребителями и идемпотентный Upsert."

	reqs := Requirements{Role: "go-primary"}
	for _, m := range []string{
		"Go — основной язык для новых сервисов",
		"Logbroker (Kafka-like) как event bus",
		"Развитие микросервиса транзакций с гарантиями консистентности и идемпотентности",
		"Дизайн API и доменной модели в стиле DDD + Hexagonal Architecture",
		"Проектирование event-driven цепочек через transactional outbox на PostgreSQL",
		"PostgreSQL для хранения состояния и transactional inbox/outbox",
		"OpenTelemetry — трейсинг на всех уровнях",
		"Реализация интеграций с платёжными процессингами",
		"Выстраивание observability",
	} {
		reqs.MustHave = append(reqs.MustHave, Requirement{Text: m, Kind: "must", Category: "stack"})
	}
	profile := LoadProfile("../../context")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	f, err := MapCoverage(ctx, fn, DefaultConcepts(), reqs, profile, letter, vacancy)
	if err != nil {
		t.Fatalf("MapCoverage (живая модель): %v", err)
	}
	t.Logf("HYBRID verdict=%s score=%d", f.Verdict, f.Score)
	for _, cc := range f.Covered {
		t.Logf("OK [%s] %s :: %s", cc.Source, cc.Text, cc.Note)
	}
	for _, cc := range f.Caveats {
		t.Logf("CB [%s] %s :: %s", cc.Source, cc.Text, cc.Note)
	}
	for _, m := range f.Missing {
		t.Logf("MS %s", m.Text)
	}
	fd := Evaluate(DefaultConcepts(), reqs, profile, letter, vacancy)
	t.Logf("DETERMINISTIC verdict=%s score=%d", fd.Verdict, fd.Score)
	if !strings.Contains(f.Verdict, "") {
		t.Logf("ok")
	}
}
