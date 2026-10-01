package fit

import (
	"strings"
	"testing"
)

// Живой кейс: письмо SRE-вакансии, проговорённое end-to-end (сентябрь 2026).
// Отдельный файл, а не тест-кейс в fit_test.go: он хранит РЕАЛЬНЫЙ текст письма
// как регрессию против четырёх форм, которые матчер принимал за опыт —
// «Опыта администрирования кластеров нет», «принципы трассировки понятны»,
// «Опыт интеграции OpenTelemetry/Tempo отсутствует», «понимаю принципы WAL».
// При любой правке understandingRe/negRe/concepts.go этот текст должен остаться
// честным: k8s и трейсинг — пробелы, WAL — разрешённый профилем мост.

const sreLiveLetter = "Здравствуйте!" +
	"\n\nМой 17-летний опыт в бэкенд-архитектуре и работе с высоконагруженными системами (до 10K RPS) позволяет мне эффективно решать задачи SRE." +
	"\n\n**Observability:** Построил полный стек мониторинга на Prometheus + Grafana (3 дашборда, 23 панели, алертинг по P99/error rate). " +
	"Имею практический опыт работы с Elasticsearch/Kibana для лог-агрегации. " +
	"*Пробел:* Опыт интеграции OpenTelemetry/Tempo отсутствует, но принципы трассировки понятны через работу с event-driven журналами событий." +
	"\n\n**Kubernetes:** *Пробел:* Опыта администрирования кластеров нет. " +
	"Мост: Готов быстро освоить отладку приложений в K8s, опираясь на фундамент в диагностике процессов, логов и метрик в Linux-среде."

const sreLiveProfile = "PostgreSQL: индексы B-tree/GIN/partial/covering, autovacuum, execution plans." +
	"\n- **WAL-G/Patroni/pg_basebackup:** НЕ работал; допустимо «понимаю принципы WAL»." +
	"\n- **Prometheus + Grafana:** 3 дашборда, 23 панели, алертинг P99 > 100ms, error rate > 1%." +
	"\n- **Elasticsearch/Kibana:** лог-агрегация в банковских микросервисах (Росгосстрах)."

func sreLiveReq(text string) Requirement {
	return Requirement{Text: text, Kind: "must", Category: "stack"}
}

// RED-1: живое письмо называет k8s-пробел словами «Опыта администрирования
// кластеров нет» — слово «нет» стоит ПОСЛЕ «опыта», через два слова.
func TestSRELiveKubernetesGapStillHonest(t *testing.T) {
	reqs := Requirements{Role: "go-primary", MustHave: []Requirement{
		sreLiveReq("Глубокое понимание Kubernetes и опыт отладки приложений в нем"),
	}}
	f := Evaluate(DefaultConcepts(), reqs, sreLiveProfile, sreLiveLetter, "SRE, Managed Kubernetes")
	for _, r := range []struct {
		src  string
		name string
		list []Req
	}{
		{SrcLetter, "Covered", f.Covered},
		{SrcBridge, "Caveats", f.Caveats},
		{SrcMissing, "Missing", f.Missing},
	} {
		for _, c := range r.list {
			if strings.Contains(c.Note, "закрыто в письме") || c.Source == SrcLetter {
				t.Errorf("k8s не должен закрываться письмом, в котором пробел назван: %q (в %s)", c.Note, r.name)
			}
		}
	}
	if f.Verdict == Apply {
		t.Errorf("вердикт = apply при названном k8s-пробеле: %+v", f)
	}
}

// RED-2: «принципы трассировки ПОНЯТНЫ» — декларация понимания, understandingRe
// ловит только «понима[юе]» и младшей формой «понятны» считался факт.
func TestSRELiveTracingUnderstoodIsNotFact(t *testing.T) {
	clause := "но принципы трассировки понятны через работу с event-driven журналами событий"
	if !understandingRe.MatchString(clause) {
		t.Errorf("«понятны» должно считаться декларацией понимания, а не фактом")
	}
}

// RED-3: пробел назван СИНОНИМОМ: требование «трейсинг», а письмо пишет
// «Опыт интеграции OpenTelemetry/Tempo отсутствует». honestGapNote сравнивает
// токены буквально, «трейсинг» в письме не встречается — пробел не виден.
func TestSRELiveObservabilityGapNamedBySynonym(t *testing.T) {
	reqs := Requirements{Role: "go-primary", MustHave: []Requirement{
		sreLiveReq("Практический опыт построения и использования систем observability (мониторинг, логи, трейсинг)"),
	}}
	f := Evaluate(DefaultConcepts(), reqs, sreLiveProfile, sreLiveLetter, "observability: мониторинг, логи, трейсинг")
	for _, c := range append(append([]Req{}, f.Caveats...), f.Missing...) {
		t.Logf("нота: %q", c.Note)
	}
	for _, c := range f.Covered {
		if c.Source == SrcLetter {
			t.Errorf("observability с названным пробелом по трейсингу не должна быть «закрыто в письме»: %q", c.Note)
		}
	}
}
