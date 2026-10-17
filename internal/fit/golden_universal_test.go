package fit

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Универсальный корпус (Task 5): регрессия от ЛОЖНЫХ срабатываний чужого
// профиля. Детерминированный матчер (DefaultConcepts) заточен под профиль
// владельца, поэтому на чужом стеке он обязан НЕ выдать «apply» с
// выдуманным покрытием — требование, которое чужой профиль не закрывает,
// уходит в skip/caveats, а не в ложное соответствие.
//
// Профили — синтетические (internal/fit/testdata), без контактов: они
// доказывают переносимость, не раскрывая реальный профиль.

var universalLeakRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|[+]7[\s\-]\d{10}|https?://`)

// loadTestData читает синтетический профиль из testdata.
func loadTestData(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("testdata/%s: %v", name, err)
	}
	if m := universalLeakRe.FindString(string(b)); m != "" {
		t.Errorf("testdata/%s содержит контакты/URL: %q", name, m)
	}
	return string(b)
}

// TestGoldenUniversal — чужие профили не должны давать ложный «apply».
func TestGoldenUniversal(t *testing.T) {
	frontend := loadTestData(t, "profile-frontend.md")
	devops := loadTestData(t, "profile-devops.md")

	// Мультитечная (многопоточная) вакансия, заходящая в чужой домен
	// (Go/highload) и в чужой домен (фронтенд) — для каждого профиля
	// проверяем именно ТУ часть, которой у него НЕТ.
	multithreadedVacancy := "Senior-инженер. Требуются: Go и highload (Kafka, 10 000 RPS, " +
		"многопоточность, gRPC), PostgreSQL, Docker; плюс TypeScript/React (SSR, бандл, " +
		"производительность рендера). Многопоточные очереди и микросервисы."

	cases := []struct {
		name    string
		profile string
		role    string
		must    []string
	}{
		{
			// Фронтенд-кандидат не закрывает Go/highload/Kafka — у него
			// их нет в профиле; детерминированный матчер не должен
			// выдать «apply» по этим чужим требованиям.
			name:    "frontend-профиль-не-закрывает-go-highload",
			profile: frontend,
			role:    "go-primary",
			must: []string{
				"Go и highload (Kafka, многопоточность)",
				"10 000 RPS, gRPC",
				"Многопоточные очереди и микросервисы",
			},
		},
		{
			// DevOps-кандидат не закрывает React/TypeScript-фронтенд —
			// у него его нет в профиле; ложный «apply» здесь = фабрикация.
			name:    "devops-профиль-не-закрывает-react-frontend",
			profile: devops,
			role:    "frontend",
			must: []string{
				"TypeScript и React (SSR)",
				"Оптимизация бандла и рендера (LCP)",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reqs := mustReqs(c.must, nil, c.role)
			// Письмо пустое: покрытие может дать только профиль. Если
			// профиль чужой (не закрывает требования) — вердикт НЕ «apply».
			res := Evaluate(DefaultConcepts(), reqs, c.profile, "", multithreadedVacancy)
			got := res.Verdict
			if got == "apply_with_caveats" {
				got = "caveats"
			}
			t.Logf("вердикт=%q score=%d missing=%v caveats=%v", got, res.Score, res.Missing, res.Caveats)
			if got == "apply" {
				t.Fatalf("ложный «apply»: чужой профиль %q закрыл чужие требования %v — фабрикация", c.name, c.must)
			}
		})
	}
}
