// Проверка на ЛОЖНЫЕ срабатывания: письмо, составленное только из фактов
// профиля, не должно вызывать warnings. Без неё универсальный guard легко
// превращается в шум, который пользователь научится игнорировать, — и тогда
// он перестанет ловить настоящие выдумки.
package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInfraClaimsNoFalsePositivesOnRealProfile(t *testing.T) {
	b, err := os.ReadFile("../../context/01-профиль-карта-фактов.md")
	if err != nil {
		t.Skipf("профиль недоступен: %v", err)
	}
	prof := string(b)
	// Письмо, написанное ТОЛЬКО по профилю — guard не должен ругаться.
	letter := "Здравствуйте!\n\n" +
		"*   **PostgreSQL:** индексы B-tree/GIN/partial/covering, autovacuum, мониторинг bloat; автор SQL-Top. Понимаю принципы WAL.\n" +
		"*   **Observability:** Prometheus + Grafana (3 дашборда, 23 панели, алертинг P99). Elasticsearch/Kibana для лог-агрегации в банковских микросервисах.\n" +
		"*   **Сеть:** gRPC и REST API (Росгосстрах), диагностика сетевой подсистемы Linux, Caddy/Nginx.\n" +
		"*   **Kafka:** at-least-once, идемпотентность через ClickHouse Upsert. Blue-Green Deploy — Stable ID.\n" +
		"*   **Go:** Fraud Engine (multi-tenant, X-API-Key), SQL-Top, mt — Go Runtime Tuner CLI.\n\n" +
		"Честно о пробелах:\n" +
		"*   *Пробел:* С Kubernetes опыта нет — мост на Docker Compose и systemd.\n" +
		"*   *Пробел:* С OpenTelemetry опыта нет, понимаю принципы трассировки.\n\n" +
		"Стек: Go, PostgreSQL, Kafka, ClickHouse, Redis, Prometheus, Grafana, Docker, Linux\n\nС уважением,"
	for _, f := range []string{"02-выжимка-rag-гео-и-stable-id.md", "03-ml-опыт-выжимка.md"} {
		if bb, err := os.ReadFile(filepath.Join("../../context", f)); err == nil {
			prof += "\n" + string(bb)
		}
	}
	r := CheckProfile(letter, prof)
	for _, w := range r.Warnings {
		t.Logf("WARNING: %s", w)
	}
	if len(r.Warnings) > 0 {
		t.Errorf("письмо, написанное только по профилю, не должно давать warnings (%d шт.)", len(r.Warnings))
	}
}
