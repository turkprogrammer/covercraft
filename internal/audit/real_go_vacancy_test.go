package audit

import (
	"strings"
	"testing"
)

// Регресс по реальной go-primary highload-вакансии (октябрь 2026): письмо,
// опускающее часть 5 Go-сервисов, обязано получить «Потерян факт» по каждому
// отсутствующему (Bundle/Domain/Geo), при этом настоящие (Stable ID, Fraud
// Engine) не должны ложно срабатывать. Фиксирует детерминизм-фикс obligations.
func TestRealGoVacancyFireGoPrimaryObligations(t *testing.T) {
	vacancy := `Чем Вы будете заниматься:

    Разработкой и поддержкой высоконагруженных бэкенд-сервисов и API (для Web, Mobile и Smart TV приложений).

    Проектированием микросервисной архитектуры, обеспечением отказоустойчивости и производительности системы.

    Интегрированием платформы с внешними и внутренними системами (биллинг, сторонние API).

    Оптимизированием запросов и устранением узких мест.

Мы ждём от Вас:

    Уверенное знание языка Go, PostgreSQL (умение оптимизировать запросы, работать с индексами) и Redis.

    Понимание принципов микросервисной архитектуры, REST, gRPC.

    Опыт работы с очередями сообщений (NATS).

    Уверенное владение Docker, Git, базовое понимание CI/CD.`
	letter := `Здравствуйте! Меня заинтересовала ваша вакансия Go-разработчика.

Чем могу быть полезен:
• Fraud Engine (Random Forest на чистом Go, 92% F1, P95 < 4.2ms), 155+ тестов.

Адаптация под ваш стек: NATS не использовал — Kafka (Stable ID: consumer groups, буферизация, идемпотентность) переносит паттерны очередей.

Стек: Go, PostgreSQL, Redis, Kafka, Docker, Git.

+7 (000) 000-00-00 | Telegram: @example
Буду рад обсудить ваши задачи. Спасибо за внимание!`
	got := strings.Join(Check(letter, vacancy).Warnings, "\n")
	for _, service := range []string{"Bundle ID", "Domain ID", "Geo-mapping"} {
		if !strings.Contains(got, "GO-PRIMARY обязан быть в письме") || !strings.Contains(got, service) {
			t.Errorf("GO-PRIMARY: потеря %q не помечена\n%s", service, got)
		}
	}
	// Stable ID есть в письме (адаптация), Fraud Engine есть — их обязательства
	// обязаны молчать; замечание «Потерян факт» по ним отсутствует.
	for _, present := range []string{"Stable ID", "Fraud Engine"} {
		if i := strings.Index(got, "Потерян факт: "+present); i >= 0 {
			t.Errorf("«%s» есть в письме, а обязательство сработало:\n%s", present, got)
		}
	}
}
