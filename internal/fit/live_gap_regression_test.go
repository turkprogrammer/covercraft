package fit

import (
	"strings"
	"testing"
)

// Регресс живого кейса (октябрь 2026, paste_8 / mistral-large-4.0).
// Письмо: «- Kubernetes не эксплуатировал — понимаю архитектуру, опыт
// Docker Compose + GitLab CI переносится на K8s». sentences() режет по
// «—»/«;», и хвостовая клауза «переносится на K8s» не содержала ни
// отрицания, ни маркера понимания — требование «k8s» засчитывалось
// «закрыто в письме», хотя письмо честно заявляло пробел. В эталоне
// порядок обратный («переносится на K8s; K8s не эксплуатировал») и
// clauseNegatedFor ловил хвост — асимметрия порядка роняла честный
// пробел в cover. «Переносится на X» — декларация переносимости опыта,
// а не опыт с X: клауза должна гаситься understandingRe наравне с
// «готов перенести на X».
func TestTransfersOntoClauseIsNotFact(t *testing.T) {
	letter := "Чем могу быть полезен:\n- **Инфраструктура:** Docker Compose, systemd.\n\nАдаптация под ваш стек:\n- Kubernetes не эксплуатировал — понимаю архитектуру, опыт Docker Compose + GitLab CI переносится на K8s, Helm и OpenTelemetry вашей инфраструктуры."
	reqs := mustReqs([]string{"k8s"}, nil, "go-primary")
	res := Evaluate(DefaultConcepts(), reqs, "", letter, "Требуется k8s")
	for _, c := range res.Covered {
		if strings.EqualFold(c.Text, "k8s") {
			t.Errorf("k8s не может быть закрыт письмом, которое пишет «не эксплуатировал»: %+v", c)
		}
	}
	if len(res.Caveats) == 0 {
		t.Errorf("честный пробел k8s обязан уйти в caveat: missing=%+v covered=%+v", res.Missing, res.Covered)
	}
}

// Регресс живого кейса (октябрь 2026, paste_8): письмо без единого слова
// «ревью» закрывал «Участие в код-ревью» концептом «архитектурное
// управление» по сигналам ADR + Hexagonal/DDD. Ревью — конкретная
// практика, её закрытие возможно только признаками ревью-культуры, а не
// архитектурными стилями. Профиль факт подтверждает (context/01:14),
// поэтому корректное поведение — «в профиле есть, впиши в письмо».
func TestCodeReviewNotClosedByArchitectureStyles(t *testing.T) {
	letter := "Чем могу быть полезен:\n- **System Design:** ADR (10), Hexagonal Architecture, DDD; retries (exponential backoff), deadlines (context.WithTimeout)."
	profile := "Code review и менторство: регулярный code review в командах (Go и PHP проекты), ADR — документирование технических решений; наставничество джунов."
	reqs := mustReqs([]string{"Участие в код-ревью"}, nil, "go-primary")
	res := Evaluate(DefaultConcepts(), reqs, profile, letter, "Участие в код-ревью")
	for _, c := range res.Covered {
		if strings.Contains(c.Text, "код-ревью") && c.Source == SrcLetter {
			t.Errorf("код-ревью закрыт «в письме» без слова «ревью» — ложное покрытие: %+v", c)
		}
	}
	found := false
	for _, c := range res.Covered {
		if strings.Contains(c.Text, "код-ревью") && c.Source == SrcProfile {
			found = true
		}
	}
	if !found {
		t.Errorf("код-ревью должен закрыться профилем с нотой «впиши в письмо»: %+v", res.Covered)
	}
}
