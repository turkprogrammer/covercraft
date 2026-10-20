package audit

import (
	"strings"
	"testing"
)

// symfonyVacancy — вакансия с Symfony в стеке: обязательство CLIFM.Ru
// привязано к ней, а не к любой PHP-вакансии.
const symfonyVacancy = `Senior Backend Engineer.
Стек: PHP 8, Symfony 7, MariaDB, PostgreSQL, RabbitMQ.`

// TestSymfonyLetterWithoutClifmFlagged — регресс живого кейса: профиль
// содержит production-проект CLIFM.Ru (Terminal Radio), но письмо называет
// только E-commerce-Lite и URL-Shortener. Без обязательства аудит молчал,
// и модель стабильно теряла production-проект в пользу pet-проектов.
func TestSymfonyLetterWithoutClifmFlagged(t *testing.T) {
	loadCandidateRules(t)
	letter := `Здравствуйте! Меня заинтересовала ваша вакансия Senior Backend Engineer.

Чем могу быть полезен:
• Symfony: E-commerce-Lite (7.2, Hexagonal Architecture, DDD, TDD), Symfony-URL-Shortener (8.0, REST API, PHPUnit 13).
• Go: Fraud Engine (multi-tenancy, Random Forest на Go), Stable ID (Kafka, 10 000 RPS).

Адаптация под ваш стек: пробелов нет — всё закрыто фактами.

Стек: Go, PHP, ML, Kafka, PostgreSQL, Docker

+7 (000) 000-00-00 | Telegram: @example

Буду рад обсудить ваши задачи. Спасибо за внимание!`

	got := strings.Join(Check(letter, symfonyVacancy).Warnings, "\n")
	if !strings.Contains(got, "CLIFM.Ru") {
		t.Errorf("потеря production-проекта CLIFM.Ru не помечена:\n%s", got)
	}
}

// TestSymfonyLetterWithClifmClean — тот же случай с CLIFM.Ru в письме:
// обязательство закрыто, замечания о потере быть не должно.
func TestSymfonyLetterWithClifmClean(t *testing.T) {
	loadCandidateRules(t)
	letter := `Здравствуйте! Меня заинтересовала ваша вакансия Senior Backend Engineer.

Чем могу быть полезен:
• Symfony: CLIFM.Ru / Terminal Radio (production, PHP/Symfony), E-commerce-Lite (7.2, Hexagonal Architecture, DDD, TDD), Symfony-URL-Shortener (8.0, REST API, PHPUnit 13).
• Go: Fraud Engine (multi-tenancy, Random Forest на Go), Stable ID (Kafka, 10 000 RPS).

Адаптация под ваш стек: пробелов нет — всё закрыто фактами.

Стек: Go, PHP, ML, Kafka, PostgreSQL, Docker

+7 (000) 000-00-00 | Telegram: @example

Буду рад обсудить ваши задачи. Спасибо за внимание!`

	for _, w := range Check(letter, symfonyVacancy).Warnings {
		if strings.Contains(w, "CLIFM.Ru") {
			t.Errorf("CLIFM.Ru есть в письме, а обязательство сработало: %s", w)
		}
	}
}

// TestClifmObligationIgnoresNonSymfonyVacancy — обязательство привязано к
// Symfony: Laravel-вакансия не должна требовать CLIFM.Ru.
func TestClifmObligationIgnoresNonSymfonyVacancy(t *testing.T) {
	loadCandidateRules(t)
	letter := `Здравствуйте! Меня заинтересовала ваша вакансия PHP-разработчик.

Чем могу быть полезен:
• Laravel (laravel-api, LaravelShop, LaravelBlog), PHP 8.x, production.
• Go: Fraud Engine (multi-tenancy, Random Forest на Go), Stable ID (Kafka, 10 000 RPS).

Адаптация под ваш стек: пробелов нет — всё закрыто фактами.

Стек: Go, PHP, ML, Kafka, PostgreSQL, Docker

+7 (000) 000-00-00 | Telegram: @example

Буду рад обсудить ваши задачи. Спасибо за внимание!`

	got := strings.Join(Check(letter, "PHP-разработчик, Laravel, MySQL, Redis.").Warnings, "\n")
	if strings.Contains(got, "CLIFM.Ru") {
		t.Errorf("Laravel-вакансия не должна требовать CLIFM.Ru:\n%s", got)
	}
}
