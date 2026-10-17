package audit

import (
	"strings"
	"testing"
)

const phpVacancy = `Ищем PHP-разработчика (Middle + / Senior).
Стек: PHP 8+; Laravel; PostgreSQL; Redis; Horizon.
Проектировать и разрабатывать новый функционал, высоконагруженные модули,
оценка эффекта через A/B тесты.`

// fullLetter оборачивает тело письма в структурную обвязку v4 (секция
// адаптации, строка стека, контакты, финальная строка): тесты отдельных правил
// не должны спотыкаться об инварианты структуры — их проверяет
// TestV4VerbatimObligations.
func fullLetter(body string) string {
	return body + "\nАдаптация под ваш стек: пробелов нет — всё закрыто фактами.\n" +
		"Стек: Go, PHP, ML, Kafka, PostgreSQL, Docker\n" +
		"+7 (000) 000-00-00 | Telegram: @handle | https://example.org/ | github.com/example\n" +
		"Буду рад обсудить ваши задачи. Спасибо за внимание!\n"
}

func TestCleanLetterPasses(t *testing.T) {
	loadCandidateRules(t)
	letter := `Здравствуйте! Меня заинтересовала ваша вакансия PHP-разработчик (Middle + / Senior).

Чем могу быть полезен:
• Laravel production (laravel-api), Symfony 7.2/8.0 (E-commerce-Lite), Lumen, Yii2 — коммерческий production; PHPUnit + TDD.
• Highload: Fraud Engine (Random Forest на Go, 92% F1, P95 < 4.2ms), Stable ID (Kafka, 10 000 RPS).
• A/B: event-driven сравнение версий моделей через Kafka (traffic split 50/50).

Адаптация под ваш стек: пробелов нет — всё закрыто фактами.

Стек: Go, PHP, ML, PostgreSQL, Redis, Kafka, Docker, Linux.

+7 (000) 000-00-00 | Telegram: @example

Буду рад обсудить ваши задачи. Спасибо за внимание!`
	r := Check(letter, phpVacancy)
	if !r.OK() {
		t.Errorf("чистое письмо не должно давать предупреждений:\n%s", strings.Join(r.Warnings, "\n"))
	}
}

func TestFrameworkInStackFlagged(t *testing.T) {
	loadCandidateRules(t)
	letter := `Здравствуйте!

Стек: Go, PHP, ML, Laravel, Symfony, Lumen, Yii2, Docker.`
	r := Check(letter, "")
	if len(r.Warnings) == 0 {
		t.Fatal("фреймворки в стеке не помечены")
	}
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "в строке стека") {
			found = true
		}
	}
	if !found {
		t.Errorf("хотел предупреждение про стек, получил: %v", r.Warnings)
	}
}

func TestForbiddenPatterns(t *testing.T) {
	cases := map[string]string{
		"Использую Copilot каждый день":                      "Copilot",
		"ChatGPT ускоряет разработку":                        "ChatGPT",
		"С Python не работал (основной стек — Go)":           "основной стек",
		"поиск ближайших соседей реализовывал на ClickHouse": "соседей",
		"ClickHouse для кэширования эмбеддингов":             "эмбеддингов",
		"Redis (кэш, блокировки) в production":               "блокиров",
	}
	for letter, name := range cases {
		r := Check(letter, "")
		if r.OK() {
			t.Errorf("паттерн %q не пойман", name)
		}
	}
}

func TestPlaceholderFlagged(t *testing.T) {
	r := Check(`Здравствуйте! Меня заинтересовала ваша вакансия [Название вакансии].`, "")
	if r.OK() {
		t.Fatal("плейсхолдер не пойман")
	}
	if !strings.Contains(strings.Join(r.Warnings, "|"), "Плейсхолдер") {
		t.Errorf("нет предупреждения про плейсхолдер: %v", r.Warnings)
	}
}

func TestButPatternInGaps(t *testing.T) {
	letter := `Честно о пробелах:
• Kafka: опыт работы с Kafka есть, но настройка брокера не реализовывал.`
	r := Check(letter, "")
	if r.OK() {
		t.Fatal("«опыт есть, но» в пробелах не пойман")
	}
}

func TestMissingObligations(t *testing.T) {
	loadCandidateRules(t)
	// PHP-вакансия, но нет Symfony/Yii2/Lumen/PHPUnit/A-B/Fraud Engine/метрик.
	letter := `Здравствуйте! Меня заинтересовала ваша вакансия PHP-разработчик (Middle + / Senior).

Чем могу быть полезен:
• Laravel production (laravel-api, LaravelShop).
• PostgreSQL, Redis.

Стек: Go, PHP, PostgreSQL, Redis, Docker.`
	r := Check(letter, phpVacancy)
	want := []string{"Symfony", "Yii2", "Lumen", "PHPUnit", "Fraud Engine", "A/B"}
	got := strings.Join(r.Warnings, "\n")
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("не помечена потеря факта %q:\n%s", w, got)
		}
	}
}

func TestNoVacancySkipsObligationWarnings(t *testing.T) {
	loadCandidateRules(t)
	letter := `Здравствуйте! Стек: Go, PHP.` // голый текст без обязательств
	r := Check(fullLetter(letter), "")
	if !r.OK() {
		t.Errorf("без вакансии обязательства проверять нельзя: %v", r.Warnings)
	}
}

func TestGoPrimaryAllFiveServicesMandatory(t *testing.T) {
	loadCandidateRules(t)
	// GO-PRIMARY вакансия: пять Go-сервисов обязаны попасть в письмо.
	// Письмо называет Fraud Engine, но молчит про остальные — аудит обязан
	// назвать каждый теряемый сервис (закрывает рандомизацию сэмплов).
	vacancy := "Ищем Go-разработчика (Senior). Высоконагруженные сервисы, latency < 100ms."
	letter := `Здравствуйте! Меня заинтересовала ваша вакансия Go-разработчика.

Чем могу быть полезен:
• Fraud Engine (Random Forest на Go, 92% F1, P95 < 4.2ms).

Адаптация под ваш стек: пробелов нет — всё закрыто фактами.

Стек: Go, ML, PostgreSQL, Redis, Kafka, Docker.

+7 (000) 000-00-00 | Telegram: @example
Буду рад обсудить ваши задачи. Спасибо за внимание!`
	got := strings.Join(Check(letter, vacancy).Warnings, "\n")
	for _, service := range []string{"Stable ID", "Bundle ID", "Domain ID", "Geo-mapping"} {
		if !strings.Contains(got, "GO-PRIMARY обязан быть в письме") || !strings.Contains(got, service) {
			t.Errorf("GO-PRIMARY: потеря сервиса %q не помечена:\n%s", service, got)
		}
	}
	// Fraud Engine есть в письме — обязательство по нему закрыто, warning нет.
	if strings.Contains(got, "Fraud Engine") && strings.Contains(got, "Потерян факт") {
		t.Errorf("Fraud Engine есть в письме, а обязательство сработало:\n%s", got)
	}
}

func TestGoPrimaryMandatoryNotAppliedToPhpHightload(t *testing.T) {
	loadCandidateRules(t)
	// PHP-primary highload-вакансия: чужие Go-сервисы не навязываются,
	// даже если в вакансии есть сигналы highload (см. TestCleanLetterPasses).
	vacancy := "Ищем PHP-разработчика (Senior). Высоконагруженные модули, A/B тесты."
	letter := `Здравствуйте! Меня заинтересовала ваша вакансия PHP-разработчика.

Чем могу быть полезен:
• Fraud Engine (Random Forest на Go, 92% F1, P95 < 4.2ms), Stable ID (10 000 RPS).

Адаптация под ваш стек: пробелов нет — всё закрыто фактами.

Стек: Go, PHP, ML, PostgreSQL, Kafka, Docker.

+7 (000) 000-00-00 | Telegram: @example
Буду рад обсудить ваши задачи. Спасибо за внимание!`
	got := strings.Join(Check(letter, vacancy).Warnings, "\n")
	for _, service := range []string{"Stable ID", "Bundle ID", "Domain ID", "Geo-mapping"} {
		// Bundle/Domain/Geo — Go-сервисы, для PHP-письма навязывать нельзя.
		if service != "Stable ID" && strings.Contains(got, service) && strings.Contains(got, "GO-PRIMARY обязан") {
			t.Errorf("PHP-primary: сервис %q не должен требовать GO-PRIMARY:\n%s", service, got)
		}
	}
}

func TestStackLineVariants(t *testing.T) {
	loadCandidateRules(t)
	// Жирный маркер «**Стек:**» тоже должен ловиться.
	letter := "Что-то выше.\n**Стек:** Go, PHP, ML, Laravel."
	r := Check(letter, "")
	if !strings.Contains(strings.Join(r.Warnings, "|"), "laravel") {
		t.Errorf("«**Стек:** … Laravel» не пойман: %v", r.Warnings)
	}
	// Слово «стек» в тексте (не строка стека) — не ложное срабатывание.
	ok := "Мы стекали микросервисы годами.\nСтек: Go, PHP, PostgreSQL."
	if r2 := Check(fullLetter(ok), ""); !r2.OK() {
		t.Errorf("ложное срабатывание на слове «стек»: %v", r2.Warnings)
	}
}

func TestDuplicateTechInBulletsAndGaps(t *testing.T) {
	letter := `Чем могу быть полезен:
• Очереди: Kafka и Horizon в production.

Честно о пробелах:
• С Elasticsearch не работал; с Horizon знаком, готов освоить интерфейс.`
	r := Check(letter, "")
	got := strings.Join(r.Warnings, "\n")
	if !strings.Contains(got, "horizon") || !strings.Contains(got, "одновременно") {
		t.Errorf("дубль Horizon (буллеты + пробелы) не помечен:\n%s", got)
	}
}

func TestMetricAttributionWrongProject(t *testing.T) {
	loadCandidateRules(t)
	// «155+ тестов» при PHPUnit без Go-контекста — ошибка атрибуции.
	letter := `Чем могу быть полезен:
• Тесты: PHPUnit + TDD (155+ тестов), PHPStan Level 6.`
	r := Check(letter, "")
	got := strings.Join(r.Warnings, "\n")
	if !strings.Contains(got, "атрибуц") || !strings.Contains(got, "155+") {
		t.Errorf("ошибочная атрибуция 155+ не помечена:\n%s", got)
	}
}

func TestMetricAttributionCorrectProject(t *testing.T) {
	loadCandidateRules(t)
	letter := `Чем могу быть полезен:
• Go и highload: Fraud Engine (Random Forest на чистом Go, 92% F1, P95 < 4.2ms), 155+ тестов.`
	r := Check(letter, "")
	for _, w := range r.Warnings {
		if strings.Contains(w, "атрибуц") {
			t.Errorf("корректная атрибуция помечена ошибочно: %s", w)
		}
	}
}

func TestDebeziumNotFalseClickHouseDuplicate(t *testing.T) {
	// Регрессия: «Debezium» содержит подстроку «clickhouse» — не должен
	// считаться упоминанием ClickHouse в секции пробелов.
	letter := `Чем могу быть полезен:
• Идемпотентность через ClickHouse Upsert (Stable ID, Kafka).

Честно о пробелах:
• NiFi/Camel/Debezium/OpenTelemetry — опыта нет, готов освоить.`
	r := Check(letter, "")
	for _, w := range r.Warnings {
		if strings.Contains(w, "одновременно") {
			t.Errorf("ложный дубль из-за Debezium: %s", w)
		}
	}
}

func TestStackLineAfterGapsNotFalseDuplicate(t *testing.T) {
	// Регрессия: Kafka/ClickHouse в строке стека (после секции пробелов) —
	// законные; детектор дублей обязан останавливаться на границе «Стек:».
	letter := `Чем могу быть полезен:
• Go и highload: Stable ID (Kafka, 10 000 RPS), ClickHouse Upsert.

Честно о пробелах:
• С MongoDB не работал; опыт с NoSQL ограничен Redis, готов освоить.

Стек: Go, PHP, ML, PostgreSQL, Redis, Kafka, ClickHouse, Docker

+7 (000) 000-00-00 | Telegram: @example`
	r := Check(letter, "")
	for _, w := range r.Warnings {
		if strings.Contains(w, "одновременно") {
			t.Errorf("ложный дубль из-за строки стека: %s", w)
		}
	}
}

func TestHonestGapNotFlaggedAsDuplicate(t *testing.T) {
	// Horizon только в пробелах («не работал») — это правильный пробел, не дубль.
	letter := `Чем могу быть полезен:
• Laravel Queue/Event в production.

Честно о пробелах:
• С Horizon не работал, готов быстро освоить.`
	r := Check(letter, "")
	for _, w := range r.Warnings {
		if strings.Contains(w, "одновременно") {
			t.Errorf("честный пробел помечен как дубль: %s", w)
		}
	}
}

func TestBareStackLineAfterGapsNotFalseDuplicate(t *testing.T) {
	// Регрессия: модель иногда пишет строку стека БЕЗ префикса «Стек:».
	// Детектор дублей не должен считать её частью секции пробелов.
	letter := `Чем могу быть полезен:
• Go и highload: Stable ID (Kafka, 10 000 RPS), ClickHouse Upsert.

Честно о пробелах:
• С MongoDB не работал; опыт с NoSQL ограничен Redis, готов освоить.

Go, PHP, ML, PostgreSQL, Redis, Kafka, ClickHouse, Docker

+7 (000) 000-00-00 | Telegram: @example`
	r := Check(letter, "")
	for _, w := range r.Warnings {
		if strings.Contains(w, "одновременно") {
			t.Errorf("ложный дубль из-за голой строки стека (без «Стек:»): %s", w)
		}
	}
}

func TestBareStackLineWithFrameworkFlagged(t *testing.T) {
	loadCandidateRules(t)
	// Голая строка стека (без «Стек:») с фреймворками тоже должна ловиться.
	letter := `Чем могу быть полезен:
• Что-то.

Честно о пробелах:
• С Kubernetes не работал, готов освоить.

Go, PHP, ML, Laravel, Symfony, Yii2, Docker`
	r := Check(letter, "")
	got := strings.Join(r.Warnings, "|")
	if !strings.Contains(got, "в строке стека") {
		t.Errorf("голая строка стека с фреймворками не поймана: %s", got)
	}
}

func TestRedisLocksAllowedWhenVacancyNeeds(t *testing.T) {
	// Вакансия прямо требует блокировки/rate limiting — упоминание законно.
	r := Check(fullLetter("• Инфраструктура: Redis (кэш, блокировки)."), "Требуется rate limiting и блокировки на Redis")
	if !r.OK() {
		t.Errorf("блокировки при требовании вакансии помечены ошибочно: %v", r.Warnings)
	}
}

// Живой регресс платёжной вакансии: технология в секции пробелов как ЯКОРЬ
// МОСТА — не дубль. Маркер отрицания («не использовал») стоит до неё, в
// клаузе другого требования; хвостовой поиск давал ложный warning, а auto-fix
// по нему требовал убрать якорь (сломав мост) и жёг генерации.
func TestBridgeAnchorInGapsIsNotDuplicate(t *testing.T) {
	letter := `Чем могу быть полезен:
• Go и highload: Stable ID (Kafka, at-least-once, идемпотентность через ClickHouse Upsert).
• Event-driven архитектура: Kafka consumer groups, буферизация при недоступности брокера.

Честно о пробелах:
• Transactional outbox: не использовал; мост — буферизация + идемпотентность + at-least-once (Stable ID: буферизованный продюсер, досылка при недоступности Kafka, идемпотентный Upsert).

Стек: Go, Kafka, PostgreSQL`
	r := Check(letter, "")
	if got := strings.Join(r.Warnings, "\n"); strings.Contains(got, "kafka") {
		t.Errorf("якорь моста помечен дублем (ложный warning жёг auto-fix):\n%s", got)
	}
}

// Обратный случай: утвердительная подача технологии в пробелах при заявленном
// факте в буллетах — противоречие, warning обязан остаться.
func TestAffirmativeTechInGapsStillWarns(t *testing.T) {
	letter := `• Очереди: Kafka в production, consumer groups.

Честно о пробелах:
• С Elasticsearch не работал; с Kafka работал в двух проектах.

Стек: Go`
	r := Check(letter, "")
	if got := strings.Join(r.Warnings, "\n"); !strings.Contains(got, "kafka") {
		t.Errorf("утвердительный дубль Kafka не помечен:\n%s", got)
	}
}

// Регресс Kairon.Finance (2026-09-29): письмо писало «Fraud Detection
// Engine», а владельцем метрик регистронезависимо значился только «Fraud
// Engine» — детектор атрибуции и highload-обязательство давали ложные
// warning'и на корректное письмо («метрика стоит не рядом со своим
// проектом», «Fraud Engine отсутствует»). Полное название проекта —
// легитимный владелец.
func TestFraudDetectionEngineCountsAsOwner(t *testing.T) {
	loadCandidateRules(t)
	letter := `• Мой Fraud Detection Engine на чистом Go обеспечивает 92% F1 и 1000+ RPS при P95 < 4.2ms.`
	r := Check(letter, "требуются навыки highload разработки")
	got := strings.Join(r.Warnings, "\n")
	if strings.Contains(got, "Проверь атрибуцию") {
		t.Errorf("ложная атрибуция на «Fraud Detection Engine»:\n%s", got)
	}
	if strings.Contains(got, "Fraud Engine (92% F1") {
		t.Errorf("обязательство ложно считает Fraud Engine потерянным:\n%s", got)
	}
}

// TestV4VerbatimObligations — требования v4, проверяемые детерминированно:
// секция адаптации, финальная строка, строка контактов, префикс стека и
// запрет самоуничижительного начала строки пробела.
func TestV4VerbatimObligations(t *testing.T) {
	loadCandidateRules(t)
	vac := "Ищем Go-разработчика. Требуется Kafka, PostgreSQL, Kubernetes."
	clean := "Go: Fraud Engine (multi-tenancy), Stable ID (Kafka, 10 000 RPS), Bundle ID (760–850 эл/с), Domain ID (329 847), Geo-mapping (гео-кластеризация).\n" +
		"Адаптация под ваш стек: Kubernetes не эксплуатировал — опыт Docker Compose переносится.\n" +
		"Стек: Go, PHP, ML, Kafka, PostgreSQL, Docker, Linux\n" +
		"+7 (000) 000-00-00 | Telegram: @handle | https://example.org/ | github.com/example\n" +
		"Буду рад обсудить ваши задачи. Спасибо за внимание!"
	if w := Check(clean, vac).Warnings; len(w) != 0 {
		t.Errorf("полное письмо не должно давать замечаний: %v", w)
	}

	missing := Check("Go: Fraud Engine. Kubernetes не эксплуатировал.", vac).Warnings
	// Контакты — не здесь: Check профиля не видит, а требовать их у того, у
	// кого в context/ их нет, значит заставлять модель выдумывать (v4 §2.6).
	for _, want := range []string{"финальная строка", "Стек:", "Адаптация"} {
		if !strings.Contains(strings.Join(missing, " "), want) {
			t.Errorf("ожидал замечание про %q, получено: %v", want, missing)
		}
	}

	// Оборудование и протоколы — не заявка на инструмент (живая проба на
	// инородном профиле, сентябрь 2026), а выдуманный инструмент — по-прежнему.
	for _, ordinary := range []string{
		"Нагрузка на CPU: 30 000 RPS, память 8 GB RAM, хранение на SSD.",
		"API отдаётся по HTTP, конфигурация в YAML, сборка в CI.",
	} {
		if w := CheckInfraClaims(ordinary, "Java, Spring Boot, Kafka, PostgreSQL."); len(w.Warnings) > 0 {
			t.Errorf("обычный словарь не должен читаться как заявка: %v", w.Warnings)
		}
	}
	if w := CheckInfraClaims("Опыт с DWH и ClickHouse.", "Java, Spring Boot."); len(w.Warnings) == 0 {
		t.Error("выдуманный инструмент вне профиля обязан ловиться")
	}

	// Русские слова в регуляках аудита: \w, \W, \b и [[:alpha:]] в Go
	// работают только по ASCII, поэтому после кириллицы они не срабатывают
	// и заявка «искусственный интеллект» проходила молча.
	if !aiToolClaimRe.MatchString("Опыт с искусственным интеллектом в разработке.") {
		t.Error("«искусственный интеллект» должен распознаваться как заявка ИИ-класса")
	}
	if !aiToolAnonFactRe.MatchString("ИИ-инструменты в разработке — постоянное использование.") {
		t.Error("анонимизированный факт об ИИ-инструментах должен распознаваться")
	}

	// Секция «Адаптация под ваш стек» не заменяет строку «Стек: »: префикс
	// искался в любом месте строки, и обязательная секция закрывала собой
	// отсутствие строки стека (живая проба на чужом профиле, сентябрь 2026).
	adaptOnly := "Go: highload-сервис.\nАдаптация под ваш стек: Kubernetes не эксплуатировал — есть Docker Compose."
	if w := Check(adaptOnly, vac).Warnings; !strings.Contains(strings.Join(w, " "), "строки стека") {
		t.Errorf("«Адаптация под ваш стек» не должна засчитываться за строку стека: %v", w)
	}

	// Контакты: профиль их содержит — письмо обязано содержать тоже.
	noContacts := "Go: highload-сервис.\nАдаптация под ваш стек: Kubernetes не эксплуатировал.\n" +
		"Стек: Go, Kafka\nБуду рад обсудить ваши задачи. Спасибо за внимание!"
	profWith := "Telegram: @handle | https://example.org/\nСтек: Go, Kafka, PostgreSQL."
	if w := CheckProfile(noContacts, profWith).Warnings; !strings.Contains(strings.Join(w, " "), "контакты") {
		t.Errorf("в профиле есть контакты — письмо без них обязано получить замечание: %v", w)
	}
	profWithout := "Стек: Go, Kafka, PostgreSQL. Опыт: highload-сервис, Kafka."
	if w := CheckProfile(noContacts, profWithout).Warnings; strings.Contains(strings.Join(w, " "), "контакты") {
		t.Errorf("в профиле нет контактов — требовать их значит заставлять выдумать: %v", w)
	}

	// Самоуничижительное начало строки пробела — красный флаг Senior (v4 §4).
	weak := Check("Адаптация под ваш стек:\nне работал с Kubernetes, но есть Docker Compose.", vac).Warnings
	if !strings.Contains(strings.Join(weak, " "), "не работал") {
		t.Errorf("строка пробела, начатая с «не работал», обязана быть замечена: %v", weak)
	}
}

// TestFabricatedNameFlagged — класс фабрикаций, который постпроверка молча
// пропускала: имя, выдуманное моделью из Telegram-хендла. Живой случай —
// «Меня зовут Турал…» и подпись «С уважением, / Турал», при том что имени
// кандидата в профиле нет.
func TestFabricatedNameFlagged(t *testing.T) {
	intro := "Меня зовут Турал. Уверен, что мой опыт будет полезен."
	if got := strings.Join(Check(intro+"\n\nС уважением,\nТурал", "").Warnings, " "); !strings.Contains(got, "Меня зовут") {
		t.Errorf("«Меня зовут X» без имени в профиле не помечено: %v", Check(intro, "").Warnings)
	}
	sig := "Go, PHP, Kafka.\n\nС уважением,\nТурал"
	if got := strings.Join(Check(sig, "").Warnings, " "); !strings.Contains(got, "подпись") {
		t.Errorf("подпись с именем, которого нет в профиле, не помечена: %v", Check(sig, "").Warnings)
	}
}

// TestSignatureContactsNotMistakenForName — строка контактов после
// «С уважением» не имя: в ней цифры, «|», «@» и точки. Ложное срабатывание
// здесь было бы хуже пропуска.
func TestSignatureContactsNotMistakenForName(t *testing.T) {
	letter := "Go, PHP, Kafka.\n\nБуду рад обсудить ваши задачи.\n\nС уважением,\n" +
		"+7 (000) 000-00-00 | Telegram: @example | https://yusupov-tech.ru/ | github.com/turkprogrammer"
	for _, w := range Check(letter, "").Warnings {
		if strings.Contains(w, "подпись") {
			t.Errorf("строка контактов ошибочно принята за имя: %s", w)
		}
	}
}

// TestCheckProfileFlagsTermsAbsentFromProfile — живой баг (вакансия IAM,
// сентябрь 2026): письмо заявило «XSSI sanitization» и «basic auth», которых
// НЕТ ни в одном context/*.md (в профиле только XSS sanitization). Audit работал
// без профиля, поэтому выдумка проходила как есть, а требование «основы
// веб-безопасности» оставалось незакрытым — то есть письмо врало и не помогало.
func TestCheckProfileFlagsTermsAbsentFromProfile(t *testing.T) {
	loadCandidateRules(t)
	profile := "CMS Blog: Go (Hexagonal, httprouter) + React; SQLite, JWT, XSS sanitization, роли admin/editor. " +
		"TLS/SSL: Caddy (Fraud Engine production). 152-ФЗ: PII-маскирование в логах."
	letter := "Здравствуйте! Веб-безопасность: XSSI sanitization, JWT и basic auth-механизмами, " +
		"TLS/SSL (Caddy), PII-маскирование (152-ФЗ). Спасибо!"

	r := CheckProfile(letter, profile)
	joined := strings.Join(r.Warnings, "\n")
	for _, want := range []string{"XSSI", "basic auth"} {
		if !strings.Contains(joined, want) {
			t.Errorf("письмо заявило %q, которого нет в профиле — audit должен ругаться; получили: %v", want, r.Warnings)
		}
	}
	// Реальные факты профиля ругаться не должны.
	for _, ok := range []string{"XSS sanitization", "Caddy", "152-ФЗ", "JWT"} {
		if strings.Contains(joined, ok) {
			t.Errorf("ложное срабатывание на подтверждённом факте %q: %v", ok, r.Warnings)
		}
	}
}

// TestCheckProfileEmptyProfileIsSilent — без профиля (нет context/*.md) audit
// не может судить о выдумках: молчит, иначе каждое письмо получало бы тонну
// ложных замечаний у пользователей без профиля.
// TestCheckProfileDeclinedTermIsNotConfirmation — строка-запрет в профиле
// («⛔ НЕ писать про Kafka») не должна превращаться в подтверждение факта.
// Живой прогон на чужих профилях, сентябрь 2026: правила claimable рабочего
// стиля карт фактов выключали проверку на самих запрещённых терминах, и
// письмо, заявившее Kafka как свой инструмент, проходило молча.
func TestCheckProfileDeclinedTermIsNotConfirmation(t *testing.T) {
	t.Cleanup(resetDefaults)
	// Профиль отрицает Kafka и утверждает про другое.
	profile := "Python 3.11, PyTorch, Celery, MLflow.\n" +
		"- ⛔ НЕ писать про Kafka, Airflow, Spark, dbt: оркестрация только Celery."
	setClaimableTerms(t, claimableTerm{pat: "Kafka", msg: "«Kafka» заявлен, а в профиле его нет"})
	// Письмо заявляет Kafka утвердительно — это выдумка, должна ругаться.
	claimed := "Здравствуйте! Опыт работы с Kafka: consumer groups, партиционирование, брокеры."
	if r := CheckProfile(claimed, profile); len(r.Warnings) == 0 {
		t.Error("запрет в профиле не должен считаться подтверждением факта: письмо заявило Kafka — нужны замечания")
	}
	// Честный пробел — не заявка, даже если термин в письме назван.
	gap := "Здравствуйте! Опыта с Kafka нет: готов освоить под ваш стек."
	if r := CheckProfile(gap, profile); len(r.Warnings) != 0 {
		t.Errorf("честно названный пробел ругаться не должен, получили: %v", r.Warnings)
	}
}

// TestContainsTermWordBoundary — термин ищется как отдельное слово, а не как
// подстрока: «CLI» не должен совпадать с «ClickHouse» и «Kafka» — с
// «Kafka-флоу» (там совпадение настоящее).
func TestContainsTermWordBoundary(t *testing.T) {
	for _, tc := range []struct {
		hay, term string
		want      bool
	}{
		{"реальные из ClickHouse для приёмки", "CLI", false},
		{"mt: Go Runtime Tuner CLI", "CLI", true},
		{"- Kafka-флоу обучения", "Kafka", true},
		{"- НЕ заявлять про Kafka", "Kafka", true},
		{"анонимноkafka", "Kafka", false},
	} {
		if got := containsTerm(tc.hay, tc.term); got != tc.want {
			t.Errorf("containsTerm(%q, %q) = %v, ждали %v", tc.hay, tc.term, got, tc.want)
		}
	}
}

// TestCheckProfileBulletLabelIsNotClaim — термин в заголовке буллета при
// фактически обратном содержании («GraphQL: работал исключительно с REST»)
// не выдумка. Живой прогон на чужом профиле, сентябрь 2026: слово «работал»
// рядом с заголовком давало две выдумки подряд — из claimableTerms и из
// CamelCase-guard.
func TestCheckProfileBulletLabelIsNotClaim(t *testing.T) {
	setClaimableTerms(t, claimableTerm{pat: "GraphQL", msg: "«GraphQL» заявлен, а в профиле его нет"})
	profile := "React 18, TypeScript, TanStack Query, Jest, Playwright.\n- ⛔ НЕ писать про GraphQL, Next.js, MobX."
	letter := "Здравствуйте!\n*   **GraphQL:** работал исключительно с REST API (TanStack Query); " +
		"понимание схем данных позволит быстро перейти на Apollo.\n\nС уважением,"
	if r := CheckProfile(letter, profile); len(r.Warnings) != 0 {
		t.Errorf("термин в заголовке при обратном содержании — не выдумка, получили: %v", r.Warnings)
	}
	// А вот настоящая заявка GraphQL как своего опыта — ругаться обязана.
	claim := "Здравствуйте! Опыт разработки схем GraphQL, запросы и мутации, Apollo Client.\n\nС уважением,"
	if r := CheckProfile(claim, profile); len(r.Warnings) == 0 {
		t.Error("заявленный опыт GraphQL без факта в профиле должен ругаться")
	}
}

// TestClaimPositivelyVsVacancyEcho — термин в письме: заявление это или цитата
// требования вакансии. Живой прогон на трёх чужих профилях (сентябрь 2026):
// требование, процитированное в заголовке буллета, и мета-ссылка на вакансию
// читались как выдумка — 6 ложных замечаний на трёх письмах.
func TestClaimPositivelyVsVacancyEcho(t *testing.T) {
	profile := "React 18, TypeScript, Vite, Redux Toolkit, TanStack Query, Jest, Playwright, Docker Compose.\n" +
		"- ⛔ НЕ писать про GraphQL, Next.js, MobX, DWH."
	setClaimableTerms(t,
		claimableTerm{pat: "GraphQL", msg: "GraphQL выдуман"},
		claimableTerm{pat: "MobX", msg: "MobX выдуман"},
		claimableTerm{pat: "Next.js", msg: "Next.js выдуман"},
		claimableTerm{pat: "DWH", msg: "DWH выдуман"},
		claimableTerm{pat: "Kubernetes", msg: "Kubernetes выдуман"},
	)
	cases := []struct {
		name   string
		letter string
		want   bool // ждём замечание
	}{
		{"перечисление в заголовке", "Здравствуйте!\n*   **Redux Toolkit или MobX, React Query:** " +
			"состояние через Redux Toolkit и TanStack Query.\nС уважением,", false},
		{"через слэш в заголовке", "Здравствуйте!\n*   **Jenkins/Docker:** пайплайны в Jenkins.\nС уважением,", false},
		{"через запятую в заголовке", "Здравствуйте!\n*   **Docker, Kubernetes (желательно):** " +
			"использовал локально для тестирования.\nС уважением,", false},
		{"мета-ссылка на вакансию", "Здравствуйте! В вакансии указаны Next.js (SSR) и GraphQL как обязательные.\n" +
			"У меня в опыте их нет, поэтому здесь пробел.\nС уважением,", false},
		{"будущее действие", "Здравствуйте!\n*   Kubernetes: развёртывание через Docker Compose, могу перейти на K8s.\n" +
			"С уважением,", false},
		{"выдумка без глагола", "Здравствуйте!\n*   **Доступность:** DWH LCP 1.4 с, WCAG 2.1 AA.\nС уважением,", true},
		{"выдумка с глаголом", "Здравствуйте!\n*   Опыт с GraphQL: схемы, запросы, мутации.\nС уважением,", true},
	}
	for _, tc := range cases {
		got := len(CheckProfile(tc.letter, profile).Warnings) > 0
		if got != tc.want {
			t.Errorf("%s: замечание=%v, ждали %v (письмо: %q)", tc.name, got, tc.want, tc.letter)
		}
	}
}

// TestNegRegexHasNoEmptyAlternative — страховка от класса регрессий, который
// стоил двух кругов: висячая «|» в регуляторе создаёт пустую альтернативу, и
// regexp.MatchString совпадает НУЛЕВОЙ длиной в любой точке — проверка
// включается целиком или выключается целиком, молча.
func TestNegRegexHasNoEmptyAlternative(t *testing.T) {
	if claimPositively("XSSI sanitization", "XSSI") != true {
		t.Error("заявление без глагола должно считаться заявлением")
	}
	if claimPositively("", "XSSI") {
		t.Error("пустое письмо не может содержать заявление")
	}
}

func TestCheckProfileEmptyProfileIsSilent(t *testing.T) {
	r := CheckProfile("Веб-безопасность: XSSI sanitization, basic auth, Hydra.", "")
	if len(r.Warnings) != 0 {
		t.Errorf("без профиля проверять нечего, получили: %v", r.Warnings)
	}
}

// TestNoSQLCategoryWithEngineInProfile — регресс живого прогона АФЛТ
// (сентябрь 2026). Письмо писало «NoSQL и микросервисы: ClickHouse,
// Elasticsearch», а guard ловил «NoSQL» как фабрикацию, потому что искал
// буквальное слово «NoSQL» в профиле. ClickHouse и Elasticsearch —
// NoSQL-движки, и профиль их подтверждает: категория закрыта своими
// представителями.
func TestNoSQLCategoryWithEngineInProfile(t *testing.T) {
	letter := "• Микросервисы и БД: PostgreSQL, MySQL, Redis (кэш), Kafka; NoSQL и микросервисы: ClickHouse, Elasticsearch; проектирование event-driven архитектур."
	profile := "PostgreSQL, MySQL, Redis, Kafka, ClickHouse, Elasticsearch, event-driven"
	for _, w := range CheckProfile(letter, profile).Warnings {
		if strings.Contains(w, "NoSQL") {
			t.Errorf("NoSQL при ClickHouse/Elasticsearch в профиле — не фабрикация: %s", w)
		}
	}
}

// TestNoSQLWithoutEngineStillFlagged — обратная сторона: если в профиле нет
// ни одного NoSQL-движка, «NoSQL» остаётся подозрительным.
func TestNoSQLWithoutEngineStillFlagged(t *testing.T) {
	letter := "• Опыт с NoSQL: ClickHouse и Elasticsearch в production."
	profile := "PostgreSQL, Redis, Kafka"
	flagged := false
	for _, w := range CheckProfile(letter, profile).Warnings {
		if strings.Contains(w, "NoSQL") {
			flagged = true
		}
	}
	if !flagged {
		t.Error("NoSQL без движков в профиле должен оставаться под вопросом")
	}
}

// TestStdlibCallNotFabrication — регресс живого прогона АФЛТ (сентябрь
// 2026). «context.WithTimeout» — вызов стандартной библиотеки Go, а не
// инструмент кандидата; infraCamelRe вытаскивал «WithTimeout» и требовал
// найти его в профиле.
func TestStdlibCallNotFabrication(t *testing.T) {
	letter := "• Конкурентный код: ProcessManager (20+ воркеров, graceful shutdown), отмена через context.WithTimeout и time.After."
	profile := "ProcessManager, graceful shutdown, channels"
	for _, w := range CheckProfile(letter, profile).Warnings {
		if strings.Contains(w, "WithTimeout") || strings.Contains(w, "After") {
			t.Errorf("вызов стандартной библиотеки не должен считаться инструментом: %s", w)
		}
	}
}

// TestHonestGapWithDottedTokenNotFabrication — регресс живого кейса
// VoIP-вакансии (октябрь 2026): пункт «с WebRTC, sip.js и Asterisk не
// работал; … готов освоить SIP-стек» раньше помечался фабрикацией, потому
// что точка в «sip.js» рвала клаузу на две, отрицание «не работал» уезжало
// во вторую, и честный пробел выглядел как утвердительная заявка. Теперь
// точка считается концом предложения только при пробеле/конце после неё.
func TestHonestGapWithDottedTokenNotFabrication(t *testing.T) {
	letter := "Адаптация: SIP / WebRTC / FreeSWITCH — с WebRTC, sip.js и Asterisk не работал; реальный опыт WebSocket — Laravel + Ratchet, готов освоить SIP-стек колл-центра."
	profile := "Go: 3 года, PHP: 2005+, PostgreSQL. VoIP/SIP/WebRTC — нет."
	for _, w := range CheckProfile(letter, profile).Warnings {
		for _, bad := range []string{"SIP", "WebRTC", "FreeSWITCH", "фабрикац"} {
			if strings.Contains(w, bad) {
				t.Errorf("честное отрицание не должно быть фабрикацией: %s", w)
			}
		}
	}
}

// TestWordLimitFollowsPrompt — аудит обязан считать лимит по тому же
// правилу, что и промпт (v4 §2.3): до 250 слов, но при 6+ обязательных
// требованиях допустимо до 350. Живой баг (октябрь 2026): жёсткие 200
// ругали на письмо на 238/246 слов, которое промпт разрешает, автоправка
// его не могла исправить, и цикл «1 замечание» повторялся бесконечно.
func TestWordLimitFollowsPrompt(t *testing.T) {
	// Письмо на 320 слов тела: между 250 и 350, то есть легально при 6+
	// требованиях и нелегально при 5 и меньше.
	letter := makeLetterOfWords(320)
	if n := bodyWordCount(letter); n != 320 {
		t.Fatalf("тестовая фикстура должна быть на 320 слов, получили %d", n)
	}
	if ws := CheckWithMust(letter, "", 5).Warnings; !hasVolumeWarning(ws) {
		t.Error("при 5 требованиях письмо на 320 слов должно давать замечание по объёму")
	}
	if ws := CheckWithMust(letter, "", 6).Warnings; hasVolumeWarning(ws) {
		t.Errorf("при 6 требованиях 320 слов разрешены промптом, замечание недопустимо: %v", ws)
	}
	if ws := CheckWithMust(letter, "", 7).Warnings; hasVolumeWarning(ws) {
		t.Errorf("при 7 требованиях 320 слов разрешены промптом, замечание недопустимо: %v", ws)
	}
	// Проверка без числа требований (разбор вакансии не удался) — самое
	// строгое поведение, а не молчаливый пропуск проверки.
	if ws := CheckWithMust(letter, "", 0).Warnings; !hasVolumeWarning(ws) {
		t.Error("без числа требований действует базовый лимит 250")
	}
	// Обратная совместимость: Check — это то же, что nMust = 0.
	before, after := Check(letter, "").Warnings, CheckWithMust(letter, "", 0).Warnings
	if len(before) != len(after) {
		t.Errorf("Check и CheckWithMust(nMust=0) обязаны совпадать: %v vs %v", before, after)
	}
}

// TestGoPrimaryVolumeAllows400 — GO-PRIMARY вакансия: пять Go-сервисов с
// метриками — обязательная дословная строка, письмо физически длиннее 300.
// Лимит для go-primary — 400, и письмо на 380 слов не должно ругаться.
func TestGoPrimaryVolumeAllows400(t *testing.T) {
	vac := "Ищем Go-разработчика (Senior). Высоконагруженные сервисы."
	if ws := CheckWithMust(makeLetterOfWords(380), vac, 7).Warnings; hasVolumeWarning(ws) {
		t.Errorf("go-primary: 380 слов должны проходить при лимите 400: %v", ws)
	}
	if ws := CheckWithMust(makeLetterOfWords(420), vac, 7).Warnings; !hasVolumeWarning(ws) {
		t.Error("go-primary: 420 слов обязано давать замечание при лимите 400")
	}
	if ws := CheckWithMust(makeLetterOfWords(380), "", 7).Warnings; !hasVolumeWarning(ws) {
		t.Error("не-go-primary вакансия: 380 слов при 7 требованиях обязано ругаться при лимите 350")
	}
}

// TestVolumeWarningNamesWideLimit — при расширенном лимите сообщение
// обязано называть его явно, иначе пользователь видит «при лимите 200»
// рядом с вакансией, где шесть требований, и не понимает расхождения.
func TestVolumeWarningNamesWideLimit(t *testing.T) {
	letter := makeLetterOfWords(380)
	ws := CheckWithMust(letter, "", 7).Warnings
	var vol string
	for _, w := range ws {
		if hasVolumeWarning([]string{w}) {
			vol = w
		}
	}
	if vol == "" {
		t.Fatalf("ожидалось замечание по объёму при 380 словах и лимите 350: %v", ws)
	}
	if !strings.Contains(vol, "лимите 350") {
		t.Errorf("в сообщении должен быть назван расширенный лимит: %q", vol)
	}
	if !strings.Contains(vol, "7 обязательных требований") {
		t.Errorf("в сообщении должно быть указано число требований: %q", vol)
	}
	// Базовый лимит не упоминает 350.
	ws = CheckWithMust(letter, "", 2).Warnings
	for _, w := range ws {
		if hasVolumeWarning([]string{w}) && strings.Contains(w, "350") {
			t.Errorf("при базовом лимите 350 упоминаться не должно: %q", w)
		}
	}
}

// TestWordLimitFor — само правило выбора лимита, отдельно от писем.
func TestWordLimitFor(t *testing.T) {
	cases := []struct {
		nMust   int
		vacancy string
		want    int
	}{
		{0, "", 250}, {1, "", 250}, {5, "", 250}, // ниже порога — узкий лимит
		{6, "", 350}, {7, "", 350}, {12, "", 350},
		{-1, "", 250}, // отрицательное — как при неудачном разборе
		// GO-PRIMARY повышает потолок независимо от числа требований.
		{0, "Go developer", 400}, {3, "golang backend", 400}, {7, "Go-разработчик", 400},
		// PHP-primary не получает go-primary потолок.
		{7, "PHP-разработчик (Senior)", 350},
	}
	for _, c := range cases {
		if got := wordLimitFor(c.nMust, c.vacancy); got != c.want {
			t.Errorf("wordLimitFor(%d, %q) = %d, ожидали %d", c.nMust, c.vacancy, got, c.want)
		}
	}
}

// hasVolumeWarning — есть ли среди замечаний жалоба на объём письма.
func hasVolumeWarning(ws []string) bool {
	for _, w := range ws {
		if strings.Contains(w, "слов при лимите") {
			return true
		}
	}
	return false
}

// makeLetterOfWords — письмо ровно на n слов тела. Слова бессодержательные,
// чтобы другие правила аудита не срабатывали: тест проверяет только объём.
func makeLetterOfWords(n int) string {
	words := make([]string, 0, n)
	for i := 0; i < n; i++ {
		words = append(words, "факт")
	}
	return strings.Join(words, " ")
}

// TestContactLineNotMatchedInsideBullet — регресс живого кейса (октябрь 2026).
// contactLineRe искал маркеры контактов ГДЕ УГОДНО в строке, и «снижение
// стоимости API на +150%» матчилось как телефон по «+1». bodyWordCount
// обрезал письмо с шестой строки: аудит насчитал 36 слов вместо 303, и
// лимит объёма не срабатывал никогда. closingCount при этом считал
// призывы к диалогу начиная с середины письма.
func TestContactLineNotMatchedInsideBullet(t *testing.T) {
	bullet := "- Geo-mapping Service: успешность маппингов +150%, снижение стоимости API на 70%, 3.1M+ записей"
	if contactLineRe.MatchString(bullet) {
		t.Error("«+150%» внутри буллета не должно считаться строкой контактов")
	}
	// Настоящая строка контактов обязана распознаваться: в начале, после
	// маркера списка или с телефона/telegram/http. Контакты — плейсхолдеры:
	// реальные телефон и Telegram в репозиторий не попадают.
	for _, ok := range []string{
		"+7 (000) 000-00-00 | Telegram: @example | https://example.com/",
		"- Telegram: @example",
		"t.me/example",
		"@example",
		"**+7 (000) 000-00-00**",
	} {
		if !contactLineRe.MatchString(ok) {
			t.Errorf("строка контактов не распознана: %q", ok)
		}
	}
	// Метрика в буллете не должна обрезать тело письма.
	letter := strings.Join([]string{
		"Здравствуйте! Меня заинтересовала ваша вакансия.",
		bullet,
		"Стек: Go, Kafka",
		"+7 (000) 000-00-00 | Telegram: @example",
		"Буду рад обсудить ваши задачи. Спасибо за внимание!",
	}, "\n")
	if n := bodyWordCount(letter); n < 20 {
		t.Errorf("тело письма обрезано буллетом с «+150%%»: насчитано %d слов", n)
	}
	// Призыв к диалогу один и считается в хвосте, после контактов.
	if c := closingCount(letter); c != 1 {
		t.Errorf("призывов к диалогу в хвосте ожидался 1, получено %d", c)
	}
}

// TestWideLimitFitsDenseFactLetter — широкий лимит 350 (решение владельца) на
// живых данных: письмо с плотным перечнем фактов по 5 направлениям. При
// 300 слов такие письма ругались, и автоправка не могла исправить их, не
// выкинув требования. Граница проверяется с обеих сторон: 345 проходит,
// 355 — уже нет.
func TestWideLimitFitsDenseFactLetter(t *testing.T) {
	if ws := CheckWithMust(makeLetterOfWords(345), "", 7).Warnings; hasVolumeWarning(ws) {
		t.Errorf("345 слов при 7 требованиях должны проходить при лимите 350: %v", ws)
	}
	if ws := CheckWithMust(makeLetterOfWords(350), "", 7).Warnings; hasVolumeWarning(ws) {
		t.Errorf("ровно 350 слов должны проходить при лимите 350: %v", ws)
	}
	if ws := CheckWithMust(makeLetterOfWords(355), "", 7).Warnings; !hasVolumeWarning(ws) {
		t.Error("355 слов при 7 требованиях обязано давать замечание по объёму")
	}
	// Узкий лимит не сдвинулся вместе с широким.
	if ws := CheckWithMust(makeLetterOfWords(345), "", 5).Warnings; !hasVolumeWarning(ws) {
		t.Error("при 5 требованиях действует базовый лимит 250, 345 слов — превышение")
	}
}

// TestPluralWordsInVolumeMessage — числительные в сообщении об объёме.
// «письмо на 303 слов» бросается в глаза и подрывает доверие к проверке:
// пользователь думает, что счёт написан небрежно, и не доверяет самому
// замечанию. Русские правила не сводятся к последней цифре — 11–14 берут
// форму множественного, хотя 12 кончается на «двойку».
func TestPluralWordsInVolumeMessage(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 слов"}, {1, "слово"}, {2, "слова"}, {4, "слова"},
		{5, "слов"}, {11, "слов"}, {12, "слов"}, {13, "слов"},
		{14, "слов"}, {21, "слово"}, {22, "слова"}, {24, "слова"},
		{25, "слов"}, {101, "слово"}, {102, "слова"}, {111, "слов"},
		{303, "слова"}, {305, "слов"},
	}
	for _, c := range cases {
		if got := pluralWords(c.n); got != c.want {
			t.Errorf("pluralWords(%d) = %q, ожидали %q", c.n, got, c.want)
		}
	}
	// Форма требований в широком сообщении.
	if got := pluralNum(7, "обязательное требование", "обязательных требования", "обязательных требований"); got != "обязательных требований" {
		t.Errorf("pluralNum(7) = %q", got)
	}
	if got := pluralNum(1, "обязательное требование", "обязательных требования", "обязательных требований"); got != "обязательное требование" {
		t.Errorf("pluralNum(1) = %q", got)
	}
	// Склейка в сообщении: без дублей и без «355 слов».
	letter := makeLetterOfWords(355)
	for _, w := range CheckWithMust(letter, "", 7).Warnings {
		if !hasVolumeWarning([]string{w}) {
			continue
		}
		// 355 → «355 слов» (последняя цифра 5 → plural), НЕ «355 слова».
		if strings.Contains(w, "355 слова") {
			t.Errorf("неверная форма числительного: %q", w)
		}
		if !strings.Contains(w, "355 слов при лимите 350 (7 обязательных требований)") {
			t.Errorf("сообщение должно называть 355 слов и 7 требований: %q", w)
		}
		if strings.Contains(w, "требований обязательных") {
			t.Errorf("дубль слова «требований»: %q", w)
		}
	}
}

// setClaimableTerms — подменить список заявляемых терминов на время теста
// (после него generic-пустой список, как у пользователя без .audit-rules.md).
func setClaimableTerms(t *testing.T, terms ...claimableTerm) {
	t.Helper()
	rulesMu.Lock()
	claimableTerms = terms
	rulesMu.Unlock()
	t.Cleanup(func() {
		rulesMu.Lock()
		resetDefaults()
		rulesMu.Unlock()
	})
}
