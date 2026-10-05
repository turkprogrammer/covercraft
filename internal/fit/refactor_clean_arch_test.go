package fit

import (
	"strings"
	"testing"
)

// Профильные факты рефакторинга и чистой архитектуры из context/01 (реальные
// строки 8, 99, 101): Hexagonal/DDD/Clean + Rector + PHPStan Level 6 + code
// review + ADR. Фикстура детерминирована и не зависит от наличия context/.
const refactorProfileFixture = "Архитектуры: Hexagonal (Ports & Adapters), DDD, Layered, Event-Driven, Clean; парадигма KISS/YAGNI. " +
	"Symfony: E-commerce-Lite (7.2, Hexagonal Architecture, DDD, TDD); PHPStan Level 6 + Rector (E-commerce-Lite). " +
	"Rector — инструмент автоматического рефакторинга PHP-кода. " +
	"Регулярный code review в командах (Go и PHP), ADR — документирование технических решений."

// Тест-ситуация (PHP-вакансия, октябрь 2026): значимые требования
// «Проектирование чистой архитектуры приложений» и «Глубокий рефакторинг
// существующей кодовой базы» уходят в «[нет данных]», хотя профиль содержит
// прямые факты: чистая архитектура (Clean/Hexagonal/DDD), рефакторинг
// (Rector, PHPStan Level 6). Это занижает скор и скрывает от пользователя
// сильное соответствие.
//
// Причина: требования целиком кириллические, tokenRe = [a-zA-Z] извлекает
// из них ноль токенов → идёт концепт-путь, а концепт-триггеры (concepts.go)
// для «чистой архитектуры» и «рефакторинга» не срабатывают. «system design»
// требует «архитектурн…проект|решен» — «чистой архитектуры» без «проект/
// решен» рядом не проходит; «модернизация legacy» (модернизац|legacy|наследи)
// не знает «рефакторинг».
//
// Это баг: значимое соответствие не выводится в вердикт, а fit-fix не может
// его исправить (в нотах неизвестных нет маркера «впиши в письмо»).
func TestRefactorCleanArchRequirementsNotUnknown(t *testing.T) {
	reqs := Requirements{Role: "php-primary", MustHave: []Requirement{
		{Text: "Проектирование чистой архитектуры приложений", Kind: "must"},
		{Text: "Глубокий рефакторинг существующей кодовой базы", Kind: "must"},
	}}
	letter := "Здравствуйте! Меня интересует ваша вакансия.\n- Архитектура и код: Hexagonal (E-commerce-Lite), DDD, SOLID, KISS, YAGNI.\n- PHP: Symfony 7.2, PHPStan Level 6 + Rector.\nБуду рад обсудить. Спасибо за внимание!\n"

	f := Evaluate(DefaultConcepts(), reqs, refactorProfileFixture, letter, "PHP-разработчик (Senior)")

	// Оба значимых требования обязаны закрыться (письмом с весом 1.0 или
	// профилем 0.7), а не уйти в «нет данных». В текущем коде они unknown —
	// это и есть фиксируемое отклонение.
	for _, c := range append(append([]Req{}, f.Covered...), f.Caveats...) {
		if !isRefactorReq(c.Text) {
			continue
		}
		if c.Source == SrcUnknown {
			t.Errorf("требование «%s» ушло в [нет данных], хотя профиль содержит факты рефакторинга/чистой архитектуры:\n  нота: %s", c.Text, c.Note)
			continue
		}
		t.Logf("ok: «%s» закрыто [%s]: %s", c.Text, c.Source, c.Note)
	}
	// В missing эти требования попадать не должны: факт в профиле есть.
	for _, c := range f.Missing {
		if isRefactorReq(c.Text) {
			t.Errorf("требование «%s» в missing, хотя профиль имеет факт: %s", c.Text, c.Note)
		}
	}
}

func isRefactorReq(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "рефакторинг") || strings.Contains(low, "чистой архитектуры")
}
