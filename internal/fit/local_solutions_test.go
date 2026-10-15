package fit

import (
	"strings"
	"testing"
)

// Живой кейс LLM-платформы (октябрь 2026): требование «практический опыт
// работы с локальными решениями» целиком кириллическое — ноль латинских
// токенов, token-матчинг его не видит. В профиле есть живой факт
// (MU-TH-UR 6000: VPS $10/мес, ONNX Runtime CPU-инференс, int8, без
// внешних API). Без концепта «локальные решения» оба требования —
// «локальные решения» и «LLM в локальной инфраструктуре» — уходили в
// «нет данных» (0.2), а второе закрывалось RAG-мостом (0.5), хотя факт
// реальный и сильный.

const localSolutionsProfile = `
ML-факты:
- Локальный AI-ассистент (Go-демон muther-engine) на VPS $10/мес: ONNX Runtime инференс на CPU, int8-квантование модели (5.2GB -> 620MB), без внешних API-провайдеров, без GPU. Precision@3 0.94.
- Go-сервис: Kafka, 10 000 RPS, Prometheus + Grafana.
`

// Профиль БЕЗ локальных фактов: негативный контроль — концепт не должен
// закрывать «локальные решения» по чужим сигналам.
const noLocalProfile = `
- Go-сервис: Kafka, 10 000 RPS, PostgreSQL, Prometheus + Grafana.
`

func TestLocalSolutionsConceptClosesByProfile(t *testing.T) {
	reqs := mustReqs([]string{
		"практический опыт работы с локальными решениями",
	}, nil, "ml-research")
	letter := "Здравствуйте! Чем могу быть полезен:\n- ML: RAG-система, F1 0.92, event-driven обучение.\nБуду рад обсудить.\n"
	f := Evaluate(DefaultConcepts(), reqs, localSolutionsProfile, letter, "AI-платформа")
	for _, c := range f.Covered {
		if c.Source != SrcProfile || !strings.Contains(c.Note, "впиши") {
			t.Fatalf("«локальные решения» обязан закрыться профилем с маркером «впиши в письмо», получено: source=%s note=%s", c.Source, c.Note)
		}
	}
	if len(f.Covered) != 1 {
		t.Fatalf("ожидало закрытие по профилю, missing=%+v caveats=%+v", f.Missing, f.Caveats)
	}
}

func TestLocalSolutionsConceptNoFactsStaysUnknown(t *testing.T) {
	reqs := mustReqs([]string{
		"практический опыт работы с локальными решениями",
	}, nil, "ml-research")
	letter := "Здравствуйте! Чем могу быть полезен:\n- Go: Kafka, 10 000 RPS, PostgreSQL.\nБуду рад обсудить.\n"
	f := Evaluate(DefaultConcepts(), reqs, noLocalProfile, letter, "AI-платформа")
	found := false
	for _, c := range f.Caveats {
		if c.Source == SrcUnknown {
			found = true
		}
	}
	if !found {
		t.Fatalf("без локальных фактов в профиле требование должно остаться «нет данных», caveats=%+v missing=%+v", f.Caveats, f.Missing)
	}
}

func TestLocalInfraLLMRequirementClosesByProfileNotBridge(t *testing.T) {
	// «LLM в локальной корпоративной инфраструктуре»: до концепта
	// закрывалось RAG-мостом (0.5). Теперь профильные факты (VPS, ONNX,
	// int8) дают закрытие по профилю (0.9) с маркером «впиши в письмо».
	reqs := mustReqs([]string{
		"практический опыт настройки, оптимизации и развёртывания больших языковых моделей в локальной корпоративной инфраструктуре",
	}, nil, "ml-research")
	letter := "Здравствуйте! Чем могу быть полезен:\n- LLM: DeepSeek R1, Llama-3.3-70B, RAG-пайплайн.\nБуду рад обсудить.\n"
	f := Evaluate(DefaultConcepts(), reqs, localSolutionsProfile, letter, "AI-платформа")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcProfile {
		t.Fatalf("«локальная инфраструктура» обязано закрыться профилем, а не мостом: covered=%+v caveats=%+v missing=%+v", f.Covered, f.Caveats, f.Missing)
	}
}

func TestLocalSolutionsConceptClosesInLetter(t *testing.T) {
	// После fit-fix письмо содержит локальный факт — закрытие по письму
	// (1.0), не по профилю.
	reqs := mustReqs([]string{
		"практический опыт работы с локальными решениями",
	}, nil, "ml-research")
	letter := "Здравствуйте! Чем могу быть полезен:\n- Локальный инференс: ONNX Runtime на CPU, int8-квантование (5.2GB -> 620MB), VPS без GPU, без внешних API.\nБуду рад обсудить.\n"
	f := Evaluate(DefaultConcepts(), reqs, localSolutionsProfile, letter, "AI-платформа")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("локальный факт в письме обязан закрыть требование письмом: covered=%+v caveats=%+v missing=%+v", f.Covered, f.Caveats, f.Missing)
	}
}

func TestLocalSolutionsLiveProfile(t *testing.T) {
	profile := loadRealProfile()
	if profile == "" {
		t.Skip("нет context/ — тест не имеет смысла")
	}
	reqs := mustReqs([]string{
		"практический опыт работы с локальными решениями",
		"практический опыт настройки, оптимизации и развёртывания больших языковых моделей в локальной корпоративной инфраструктуре",
	}, nil, "ml-research")
	letter := "Здравствуйте! Чем могу быть полезен:\n- ML/LLM: DeepSeek R1, RAG-пайплайн, event-driven обучение.\nБуду рад обсудить.\n"
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "AI-платформа")
	for _, m := range f.Missing {
		t.Errorf("локальные требования не должны быть missing: %s", m.Text)
	}
	// Хотя бы одно из двух требований — закрыто профилем или письмом.
	letterClosed := 0
	for _, c := range f.Covered {
		if c.Source == SrcProfile || c.Source == SrcLetter {
			letterClosed++
		}
	}
	if letterClosed == 0 {
		t.Errorf("ожидало закрытие по профилю/письму локальных требований: covered=%+v caveats=%+v", f.Covered, f.Caveats)
	}
}
