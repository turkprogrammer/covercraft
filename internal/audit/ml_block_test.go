package audit

import (
	"strings"
	"testing"
)

// Регресс по живым результатам (октябрь 2026, agnes-2.5-flash /
// mistral-large-4.0): письмо перечисляет все 5 ML-сервисов внутри буллета
// «Go и highload» — fit засчитывает «ML-модели» по концепту, вердикт Apply,
// и письмо уходит БЕЗ выделенного ML-блока, который требует эталон
// пользователя. Обязательство закрывает дырку детерминистически: автоправка
// дописывает блок по замечанию «Потерян факт».
func TestMLVacancyRequiresDedicatedMLBlock(t *testing.T) {
	vacancy := `Ключевые навыки: PostgreSQL, Git, Golang, Docker, Kafka, Redis,
RabbitMQ, HTTP, REST API, WebSocket, DevOps, Go от 3-х лет,
Микросервисная архитектура, best practices разработка, k8s, ML-модели`

	noBlock := `Здравствуйте!

Чем могу быть полезен:
- **Go и highload:** Stable ID (Kafka, 10 000 RPS), Fraud Engine (Random Forest на Go, 92% F1), Bundle ID (760–850 эл/с), Domain ID (416K+ доменов), Geo-mapping (Llama-3.3-70B, +150%).
- **System Design:** ADR (10), Hexagonal Architecture, DDD.

Адаптация под ваш стек:
- Kubernetes не эксплуатировал — готов освоить.

Стек: Go, PHP, ML, Kafka, Redis, PostgreSQL, Docker.`

	withBlock := `Здравствуйте!

Чем могу быть полезен:
- **Go и highload:** Stable ID (Kafka, 10 000 RPS), Fraud Engine (Random Forest на Go, 92% F1).
- **ML-модели и их эксплуатация в production:** 5 production-сервисов — Fraud Engine (Random Forest на чистом Go), Stable ID (event-driven обучение через Kafka), Bundle ID (DeepSeek R1), Domain ID (AI-классификатор IAB), Geo-mapping (Llama-3.3-70B-Instruct).

Адаптация под ваш стек:
- Kubernetes не эксплуатировал — готов освоить.

Стек: Go, PHP, ML, Kafka, Redis, PostgreSQL, Docker.`

	got := strings.Join(Check(noBlock, vacancy).Warnings, "\n")
	if !strings.Contains(got, "Потерян факт: ML-модели") {
		t.Errorf("письмо без отдельного ML-буллета обязано получить замечание об ML-блоке:\n%s", got)
	}
	got2 := strings.Join(Check(withBlock, vacancy).Warnings, "\n")
	if strings.Contains(got2, "Потерян факт: ML-модели") {
		t.Errorf("письмо с выделенным ML-буллетом не должно получать это замечание:\n%s", got2)
	}
	// Вакансия без ML — обязательство молчит (PHP-письма не обязаны
	// тащить ML-блок): need-регэксп срабатывает только по требованию ML.
	phpVac := "Требуется PHP-разработчик: Yii2, Laravel, MySQL, Redis."
	got3 := strings.Join(Check(noBlock, phpVac).Warnings, "\n")
	if strings.Contains(got3, "Потерян факт: ML-модели") {
		t.Errorf("PHP-вакансия без ML не должна требовать ML-блок:\n%s", got3)
	}
}
