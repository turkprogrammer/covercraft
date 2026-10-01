package fit

import (
	"strings"
	"testing"
)

// Живое письмо на PHP-архитектора (прогон, сентябрь 2026). Регрессия против
// ложного «не закрыто»: требование «Работа с реляционными и NoSQL базами
// данных, понимание вопросов консистентности и производительности» почти целиком
// кириллическое, поэтому tokenRe извлекает из него ОДИН токен — «nosql».
// Ветка len(tokens)==0 не срабатывает, концепт-путь пропускается целиком, и
// требование уходило в missing, хотя письмо прямо писало «Экспертное владение
// PostgreSQL, MySQL, ClickHouse, Redis, Oracle» и «Оптимизация запросов,
// индексов», а концепт «реляционные БД и SQL» находил оба своих сигнала.
const phpDbLetterLive = "*   **Базы данных (SQL):** Экспертное владение PostgreSQL, MySQL, ClickHouse, Redis, Oracle. " +
	"Оптимизация запросов, индексов и конфигурации для обеспечения консистентности и высокой скорости."

const phpDbProfileLive = "PostgreSQL: индексы B-tree/GIN/partial/covering, autovacuum, execution plans; " +
	"ClickHouse Upsert (идемпотентность), Redis, MySQL, Oracle."

// TestPHPDbRequirementClosedByConcept — требование с одним токеном «nosql»,
// которое в письме не написано дословно, но смысл («PostgreSQL, MySQL,
// ClickHouse, Redis, Oracle», «оптимизация запросов, индексов») подтверждён
// двумя сигналами концепта. Такое требование обязано закрываться.
func TestPHPDbRequirementClosedByConcept(t *testing.T) {
	reqs := Requirements{Role: "php-primary", MustHave: []Requirement{
		{Text: "Работа с реляционными и NoSQL базами данных, понимание вопросов консистентности и производительности", Kind: "must", Category: "domain"},
	}}
	f := Evaluate(DefaultConcepts(), reqs, phpDbProfileLive, phpDbLetterLive, "PHP-архитектор, БД")
	if f.Verdict == Skip {
		t.Fatalf("письмо не должно получать skip: %+v", f)
	}
	for _, c := range f.Missing {
		if strings.Contains(c.Text, "базами данных") {
			t.Errorf("требование о БД не должно быть missing: %q", c.Note)
		}
	}
	if len(f.Covered) == 0 {
		t.Errorf("ожидалось покрытие по признакам письма; получили %+v", f)
	}
}

// TestPHPSoloOwnershipRequirementClosed — живой кейс: требование «Умение
// самостоятельно вести сложные технические задачи от анализа до результата»
// уходило в «[нет данных]», хотя письмо прямо писало «Ведение сложных задач
// от анализа до продакшена в одиночку или как ключевой инженер».
// Причина: требование без латиницы и без стажа не задевало триггеры
// концептов (там «опыт инженер», «N лет»), а сигналов про самостоятельное
// ведение не было ни в одном концепте.
func TestPHPSoloOwnershipRequirementClosed(t *testing.T) {
	reqs := Requirements{Role: "php-primary", MustHave: []Requirement{
		{Text: "Умение самостоятельно вести сложные технические задачи от анализа до результата", Kind: "must", Category: "other"},
	}}
	letter := "*   **Code Review и самостоятельность:** Регулярный code review в командах Go/PHP. " +
		"Ведение сложных задач от анализа до продакшена в одиночку или как ключевой инженер (Fraud Engine). " +
		"Архитектурные решения через ADR (10 шт.)."
	f := Evaluate(DefaultConcepts(), reqs, phpDbProfileLive, letter, "PHP-архитектор")
	if f.Verdict == Skip {
		t.Fatalf("письмо не должно получать skip: %+v", f)
	}
	if len(f.Covered) == 0 {
		t.Errorf("требование о самостоятельном ведении задач должно закрываться; получили %+v", f)
	}
}
