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
	letter := `Здравствуйте! Стек: Go, PHP.` // голый текст без обязательств
	r := Check(fullLetter(letter), "")
	if !r.OK() {
		t.Errorf("без вакансии обязательства проверять нельзя: %v", r.Warnings)
	}
}

func TestStackLineVariants(t *testing.T) {
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
	vac := "Ищем Go-разработчика. Требуется Kafka, PostgreSQL, Kubernetes."
	clean := "Go: Fraud Engine (multi-tenancy), Stable ID (Kafka, 10 000 RPS).\n" +
		"Адаптация под ваш стек: Kubernetes не эксплуатировал — опыт Docker Compose переносится.\n" +
		"Стек: Go, PHP, ML, Kafka, PostgreSQL, Docker, Linux\n" +
		"+7 (000) 000-00-00 | Telegram: @handle | https://example.org/ | github.com/example\n" +
		"Буду рад обсудить ваши задачи. Спасибо за внимание!"
	if w := Check(clean, vac).Warnings; len(w) != 0 {
		t.Errorf("полное письмо не должно давать замечаний: %v", w)
	}

	missing := Check("Go: Fraud Engine. Kubernetes не эксплуатировал.", vac).Warnings
	for _, want := range []string{"финальная строка", "контакты", "Стек:", "Адаптация"} {
		if !strings.Contains(strings.Join(missing, " "), want) {
			t.Errorf("ожидал замечание про %q, получено: %v", want, missing)
		}
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

// TestWordLimitFollowsPrompt — аудит обязан считать лимит по тому же
// правилу, что и промпт (v4 §2.3): до 200 слов, но при 6+ обязательных
// требованиях допустимо до 250. Живой баг (октябрь 2026): жёсткие 200
// ругали на письмо на 238/246 слов, которое промпт разрешает, автоправка
// его не могла исправить, и цикл «1 замечание» повторялся бесконечно.
func TestWordLimitFollowsPrompt(t *testing.T) {
	// Письмо на 246 слов тела: между 200 и 250, то есть легально при 6+
	// требованиях и нелегально при 5 и меньше.
	letter := makeLetterOfWords(246)
	if n := bodyWordCount(letter); n != 246 {
		t.Fatalf("тестовая фикстура должна быть на 246 слов, получили %d", n)
	}
	if ws := CheckWithMust(letter, "", 5).Warnings; !hasVolumeWarning(ws) {
		t.Error("при 5 требованиях письмо на 246 слов должно давать замечание по объёму")
	}
	if ws := CheckWithMust(letter, "", 6).Warnings; hasVolumeWarning(ws) {
		t.Errorf("при 6 требованиях 246 слов разрешены промптом, замечание недопустимо: %v", ws)
	}
	if ws := CheckWithMust(letter, "", 7).Warnings; hasVolumeWarning(ws) {
		t.Errorf("при 7 требованиях 246 слов разрешены промптом, замечание недопустимо: %v", ws)
	}
	// Проверка без числа требований (разбор вакансии не удался) — самое
	// строгое поведение, а не молчаливый пропуск проверки.
	if ws := CheckWithMust(letter, "", 0).Warnings; !hasVolumeWarning(ws) {
		t.Error("без числа требований действует базовый лимит 200")
	}
	// Обратная совместимость: Check — это то же, что nMust = 0.
	before, after := Check(letter, "").Warnings, CheckWithMust(letter, "", 0).Warnings
	if len(before) != len(after) {
		t.Errorf("Check и CheckWithMust(nMust=0) обязаны совпадать: %v vs %v", before, after)
	}
}

// TestVolumeWarningNamesWideLimit — при расширенном лимите сообщение
// обязано называть его явно, иначе пользователь видит «при лимите 200»
// рядом с вакансией, где шесть требований, и не понимает расхождения.
func TestVolumeWarningNamesWideLimit(t *testing.T) {
	letter := makeLetterOfWords(320)
	ws := CheckWithMust(letter, "", 7).Warnings
	var vol string
	for _, w := range ws {
		if hasVolumeWarning([]string{w}) {
			vol = w
		}
	}
	if vol == "" {
		t.Fatalf("ожидалось замечание по объёму при 320 словах и лимите 300: %v", ws)
	}
	if !strings.Contains(vol, "лимите 300") {
		t.Errorf("в сообщении должен быть назван расширенный лимит: %q", vol)
	}
	if !strings.Contains(vol, "7 обязательных требований") {
		t.Errorf("в сообщении должно быть указано число требований: %q", vol)
	}
	// Базовый лимит не упоминает 300.
	ws = CheckWithMust(letter, "", 2).Warnings
	for _, w := range ws {
		if hasVolumeWarning([]string{w}) && strings.Contains(w, "300") {
			t.Errorf("при базовом лимите 300 упоминаться не должно: %q", w)
		}
	}
}

// TestWordLimitFor — само правило выбора лимита, отдельно от писем.
func TestWordLimitFor(t *testing.T) {
	cases := []struct {
		nMust int
		want  int
	}{
		{0, 200}, {1, 200}, {5, 200}, // ниже порога — узкий лимит
		{6, 300}, {7, 300}, {12, 300},
		{-1, 200}, // отрицательное — как при неудачном разборе
	}
	for _, c := range cases {
		if got := wordLimitFor(c.nMust); got != c.want {
			t.Errorf("wordLimitFor(%d) = %d, ожидали %d", c.nMust, got, c.want)
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

// TestWideLimitFitsDenseFactLetter — широкий лимит 300 (решение владельца) на
// живых данных: письмо с плотным перечнем фактов по 5 направлениям. При
// 250 слов такие письма ругались, и автоправка не могла исправить их, не
// выкинув требования. Граница проверяется с обеих сторон: 295 проходит,
// 305 — уже нет.
func TestWideLimitFitsDenseFactLetter(t *testing.T) {
	if ws := CheckWithMust(makeLetterOfWords(295), "", 7).Warnings; hasVolumeWarning(ws) {
		t.Errorf("295 слов при 7 требованиях должны проходить при лимите 300: %v", ws)
	}
	if ws := CheckWithMust(makeLetterOfWords(300), "", 7).Warnings; hasVolumeWarning(ws) {
		t.Errorf("ровно 300 слов должны проходить при лимите 300: %v", ws)
	}
	if ws := CheckWithMust(makeLetterOfWords(305), "", 7).Warnings; !hasVolumeWarning(ws) {
		t.Error("305 слов при 7 требованиях обязано давать замечание по объёму")
	}
	// Узкий лимит не сдвинулся вместе с широким.
	if ws := CheckWithMust(makeLetterOfWords(295), "", 5).Warnings; !hasVolumeWarning(ws) {
		t.Error("при 5 требованиях действует базовый лимит 200, 295 слов — превышение")
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
	// Склейка в сообщении: без дублей и без «303 слов».
	letter := makeLetterOfWords(305)
	for _, w := range CheckWithMust(letter, "", 7).Warnings {
		if !hasVolumeWarning([]string{w}) {
			continue
		}
		// 305 → «305 слов» (последняя цифра 5 → plural), НЕ «305 слова».
		if strings.Contains(w, "305 слова") {
			t.Errorf("неверная форма числительного: %q", w)
		}
		if !strings.Contains(w, "305 слов при лимите 300 (7 обязательных требований)") {
			t.Errorf("сообщение должно называть 305 слов и 7 требований: %q", w)
		}
		if strings.Contains(w, "требований обязательных") {
			t.Errorf("дубль слова «требований»: %q", w)
		}
	}
}
