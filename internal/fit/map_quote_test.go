package fit

import (
	"context"
	"strings"
	"testing"
)

// Регресс платёжной вакансии: цитата модели с запятой («PostgreSQL,
// transactional outbox») не должна обрезаться до первой клаузы — иначе
// токены transactional/outbox выпадают из проверки и требование уходит в
// unknown, хотя письмо его закрывает.
func TestMapCoverageQuoteAcrossComma(t *testing.T) {
	reqs := mustReqs([]string{"PostgreSQL для состояния и transactional outbox"}, nil, "go-primary")
	letter := "PostgreSQL, transactional outbox на уровне приложения (буферизация, идемпотентность)."
	raw := `{"items":[{"text":"PostgreSQL для состояния и transactional outbox","source":"letter","quote":"PostgreSQL, transactional outbox","note":"закрыто"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("цитата через запятую должна закрывать требование письмом: covered=%+v caveats=%+v missing=%+v", f.Covered, f.Caveats, f.Missing)
	}
	if !strings.Contains(f.Covered[0].Note, "цитата") {
		t.Errorf("нота должна приводить цитату: %q", f.Covered[0].Note)
	}
}

// Отрицание в СОСЕДНЕМ предложении не роняет факт из текущего: граница
// предложения — точка, а не запятая, поэтому «PG есть» остаётся фактом,
// даже если дальше сказано «outbox не использовал».
func TestMapCoverageNegationNextSentenceNotBlocking(t *testing.T) {
	reqs := mustReqs([]string{"PostgreSQL для хранения состояния"}, nil, "go-primary")
	letter := "PostgreSQL: транзакции, индексы B-tree. Transactional outbox не использовал."
	raw := `{"items":[{"text":"PostgreSQL для хранения состояния","source":"letter","quote":"PostgreSQL: транзакции, индексы B-tree","note":"PG есть"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("отрицание в другом предложении не должно ронять факт: %+v", f)
	}
}
