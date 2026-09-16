package fit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

/* ---------- ParseExtraction ---------- */

const goodJSON = `{"role":"go-primary","mustHave":[{"text":"Опыт с Kafka","kind":"must","category":"stack"}],"niceToHave":[{"text":"Знание ClickHouse будет плюсом","kind":"nice","category":"stack"}],"soft":[{"text":"Самоорганизованность","kind":"soft","category":"other"}]}`

func TestParseExtractionValid(t *testing.T) {
	r, err := ParseExtraction(goodJSON)
	if err != nil {
		t.Fatalf("ParseExtraction: %v", err)
	}
	if r.Role != "go-primary" || len(r.MustHave) != 1 || len(r.NiceToHave) != 1 || len(r.Soft) != 1 {
		t.Fatalf("неполный разбор: %+v", r)
	}
	if r.MustHave[0].Kind != "must" || r.NiceToHave[0].Kind != "nice" || r.Soft[0].Kind != "soft" {
		t.Errorf("kind должен нормализоваться по корзине: %+v", r)
	}
}

func TestParseExtractionMarkdownWrapped(t *testing.T) {
	// Модель часто оборачивает JSON в ```json ... ``` — разбор обязан
	// вытащить JSON из мусора.
	raw := "```json\n" + goodJSON + "\n```"
	r, err := ParseExtraction(raw)
	if err != nil {
		t.Fatalf("ParseExtraction: %v", err)
	}
	if r.Role != "go-primary" {
		t.Errorf("role = %q", r.Role)
	}
}

func TestParseExtractionNoJSON(t *testing.T) {
	if _, err := ParseExtraction("модель отказалась и написала прозу"); err == nil {
		t.Error("хочу ошибку, когда JSON нет вовсе")
	}
}

func TestParseExtractionBrokenJSON(t *testing.T) {
	if _, err := ParseExtraction(`{"role": "go-primary", "mustHave": [`); err == nil {
		t.Error("хочу ошибку на битом JSON")
	}
}

/* ---------- ExtractRequirements ---------- */

func TestExtractRequirementsUsesLLMSeam(t *testing.T) {
	var gotSystem, gotUser string
	fn := func(_ context.Context, system, user string) (string, error) {
		gotSystem, gotUser = system, user
		return goodJSON, nil
	}
	r, err := ExtractRequirements(context.Background(), fn, "вакансия-текст")
	if err != nil {
		t.Fatalf("ExtractRequirements: %v", err)
	}
	if r.Role != "go-primary" {
		t.Errorf("role = %q", r.Role)
	}
	if gotUser != "вакансия-текст" {
		t.Errorf("вакансия должна идти в user-промпт: %q", gotUser)
	}
	if !strings.Contains(gotSystem, "mustHave") {
		t.Errorf("системный промпт должен фиксировать схему JSON: %.50q", gotSystem)
	}
}

func TestExtractRequirementsSurfacesError(t *testing.T) {
	fn := func(_ context.Context, _, _ string) (string, error) { return "", errors.New("429") }
	if _, err := ExtractRequirements(context.Background(), fn, "v"); err == nil {
		t.Error("хочу ошибку LLM наружу")
	}
}

func TestExtractRequirementsNilFn(t *testing.T) {
	if _, err := ExtractRequirements(context.Background(), nil, "v"); err == nil {
		t.Error("хочу ошибку при nil-функции")
	}
}
