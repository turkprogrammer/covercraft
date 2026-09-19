package fit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeMapLLM — модель, отвечающая заранее заданным JSON разметки.
func fakeMapLLM(raw string) LLMFunc {
	return func(ctx context.Context, system, user string) (string, error) {
		return raw, nil
	}
}

const liveLetter = "Stable ID (Kafka, at-least-once, идемпотентность через ClickHouse Upsert).\n" +
	"Честно о пробелах:\n" +
	"- OpenTelemetry: опыта нет, observability — SQL-Top, Prometheus + Grafana, готов освоить."

// Главный профит гибрида: требование без распознаваемых технологий
// («интеграции с платёжными процессингами») матчер уводит в unknown, а
// модель закрывает его письмом по смыслу — с дословной цитатой.
func TestMapCoverageSemanticRequirement(t *testing.T) {
	reqs := mustReqs([]string{"Реализация интеграций с платёжными процессингами"}, nil, "go-primary")
	letter := "Проект «Платежи»: интеграции с эквайрингом и платёжными процессингами, каждый со своим протоколом."
	raw := `{"items":[{"text":"Реализация интеграций с платёжными процессингами","source":"letter","quote":"интеграции с эквайрингом и платёжными процессингами","note":"есть прод-опыт"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("требование закрыто письмом по цитате: %+v", f.Covered)
	}
	if !strings.Contains(f.Covered[0].Note, "цитата") {
		t.Errorf("нота должна приводить цитату: %q", f.Covered[0].Note)
	}
	if f.Verdict != Apply {
		t.Errorf("verdict = %s, хочу %s (всё закрыто письмом)", f.Verdict, Apply)
	}
}

// Модель сказала «закрыто», но цитаты в письме нет — запись недоказана,
// берём детерминированный результат (unknown по концепт-ветке).
func TestMapCoverageUngroundedQuoteFallsBack(t *testing.T) {
	reqs := mustReqs([]string{"Реализация интеграций с платёжными процессингами"}, nil, "go-primary")
	letter := "Пишу про Kafka и Go, про платежи ничего."
	raw := `{"items":[{"text":"Реализация интеграций с платёжными процессингами","source":"letter","quote":"опыт платёжных систем в финтехе восемь лет","note":"выдумка"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Covered) != 0 || len(f.Missing) != 0 || len(f.Caveats) != 1 {
		t.Fatalf("недоказанная запись не должна закрывать требование: %+v", f)
	}
	if f.Caveats[0].Source != SrcUnknown {
		t.Errorf("ожидался unknown из матчера: %+v", f.Caveats[0])
	}
}

// Модель «закрыла» честный пробел, процитировав клаузу-отрицание — верификатор
// обязан превратить это в честный пробел (unknown), а не в покрытие.
func TestMapCoverageNegatedQuoteBecomesHonestGap(t *testing.T) {
	reqs := mustReqs([]string{"OpenTelemetry — трейсинг на всех уровнях"}, nil, "go-primary")
	raw := `{"items":[{"text":"OpenTelemetry — трейсинг на всех уровнях","source":"letter","quote":"OpenTelemetry: опыта нет","note":"упомянут"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", liveLetter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Covered) != 0 {
		t.Fatalf("отрицание не должно считаться покрытием: %+v", f.Covered)
	}
	if len(f.Caveats) != 1 || !isHonestGap(f.Caveats[0].Note) {
		t.Fatalf("ожидался честный пробел: %+v", f.Caveats)
	}
}

// Соседняя клауза с позитивом не должна попадать под отрицание: цитата про
// observability из той же строки-пробела остаётся покрытием письма.
func TestMapCoverageQuoteFromPositiveClause(t *testing.T) {
	reqs := mustReqs([]string{"Выстраивание observability"}, nil, "go-primary")
	raw := `{"items":[{"text":"Выстраивание observability","source":"letter","quote":"observability — SQL-Top, Prometheus + Grafana","note":"строил"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", liveLetter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("позитивная клауза должна закрывать требование письмом: %+v", f)
	}
}

// Ограничитель профиля перевешивает профиль-цитату модели: outbox не
// засчитывается профиль-фактом, требование уходит мостом/unknown.
func TestMapCoverageProfileLimiterRejected(t *testing.T) {
	reqs := mustReqs([]string{"Проектирование event-driven цепочек через transactional outbox на PostgreSQL"}, nil, "go-primary")
	profile := "ОБЩИЙ ПРОФИЛЬ:\n- Надёжная доставка событий (мост к outbox): буферизация, идемпотентный Upsert, at-least-once, event-driven паттерны.\nОГРАНИЧИТЕЛИ:\n- Transactional outbox на PostgreSQL: не использовал."
	raw := `{"items":[{"text":"Проектирование event-driven цепочек через transactional outbox на PostgreSQL","source":"profile","quote":"Надёжная доставка событий (мост к outbox): буферизация, идемпотентный Upsert, at-least-once","note":"мост"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, profile, "", "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	for _, c := range f.Covered {
		if c.Source == SrcProfile {
			t.Errorf("ограничитель профиля не даёт профиль-факта: %+v", f.Covered)
		}
	}
	for _, a := range f.Advice {
		if strings.Contains(a, "впиши в письмо") {
			t.Errorf("совет вписать неприменённый паттерн недопустим: %q", a)
		}
	}
}

// Модель не имеет права ухудшать: письмо реально закрывает требование, а
// модель назвала missing — вердикт считаем по детерминированному матчеру.
func TestMapCoverageModelCannotDowngrade(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Kafka и ClickHouse"}, nil, "go-primary")
	letter := "Стек: Go, Kafka, ClickHouse."
	raw := `{"items":[{"text":"Опыт с Kafka и ClickHouse","source":"missing","quote":"","note":"не увидел"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter || len(f.Missing) != 0 {
		t.Fatalf("закрытое письмом требование не должно уходить в missing: %+v", f)
	}
}

// Quota-цитата model, которая частично доказательства: «PostgreSQL» для
// «PostgreSQL + in-почтовый/транзакционный outbox» не должно закрывать.
// Вариант Б — цитата-доказательство проверяется в verifyItem.
func TestMapCoveragePartialQuoteRejected(t *testing.T) {
	reqs := mustReqs([]string{"PostgreSQL для состояния и transactional outbox"}, nil, "go-primary")
	letter := "PostgreSQL, MySQL, Redis. Честно о пробелах: transactional outbox — не использовал."
	raw := `{"items":[{"text":"PostgreSQL для состояния и transactional outbox","source":"letter","quote":"PostgreSQL, MySQL, Redis","note":"закрыто"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	// Модель вернула letter, но цитата не покрывает transactional outbox — откат
	if len(f.Covered) != 0 {
		t.Fatalf("partial quote не должно закрывать: %+v", f.Covered)
	}
	if len(f.Missing) != 0 {
		t.Errorf("ожидался откат матчера, а не missing: %+v", f.Missing)
	}
}

// Majority-правило без честного пробела — letter остаётся (Variant А).
func TestMapCoverageMajorityWithoutHonestGap(t *testing.T) {
	reqs := mustReqs([]string{"PostgreSQL для состояния и transactional outbox"}, nil, "go-primary")
	// outbox не отрицается — majority токенов (postgres, transactional, outbox) найден
	letter := "PostgreSQL, transactional outbox на уровне приложения (буферизация, идемпотентность)."
	raw := `{"items":[{"text":"PostgreSQL для состояния и transactional outbox","source":"letter","quote":"PostgreSQL, transactional outbox","note":"закрыто"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	// Модель вернула letter (partial quote), но матчер — закрыл majority
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("majority без честного пробела должен закрывать: %+v", f.Covered)
	}
}

// Модель увидела смысл там, где у матчера нет словарных зацепок: её unknown
// принимается (это мягче missing — «нет данных», а не «не закрыто ничем»).
func TestMapCoverageModelUnknownAccepted(t *testing.T) {
	reqs := mustReqs([]string{"Опыт в финтехе или платёжных системах"}, nil, "go-primary")
	letter := "Работал в банке Росгосстрах над внутренними сервисами."
	raw := `{"items":[{"text":"Опыт в финтехе или платёжных системах","source":"unknown","quote":"","note":"банк упомянут, но роль неясна"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Caveats) != 1 || f.Caveats[0].Source != SrcUnknown || len(f.Missing) != 0 {
		t.Fatalf("ожидался unknown без missing: %+v", f)
	}
	if !strings.Contains(f.Caveats[0].Note, "оценка модели") {
		t.Errorf("нота должна помечать оценку модели: %q", f.Caveats[0].Note)
	}
}

// Битый JSON / пустой ответ — ошибка: сервер откатывается на Evaluate.
func TestMapCoverageBadJSON(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Kafka"}, nil, "go-primary")
	for _, raw := range []string{"не JSON вовсе", `{"items":[]}`, `{"items": [{"text": "x"`} {
		if _, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", "letter", "v"); err == nil {
			t.Errorf("ожидалась ошибка разбора на %q", raw)
		}
	}
}

// Ошибка модели (таймаут и т.п.) пробрасывается наверх — вызывающий
// откатывается на детерминированный вердикт.
func TestMapCoverageLLMError(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Kafka"}, nil, "go-primary")
	fn := func(ctx context.Context, system, user string) (string, error) {
		return "", errors.New("таймаут")
	}
	if _, err := MapCoverage(context.Background(), fn, DefaultConcepts(), reqs, "", "letter", "v"); err == nil {
		t.Error("ошибка модели должна возвращаться наружу")
	}
}

// Живой кейс гибрида: модель закрыла требование цитатой из соседней клаузы,
// а письмо честно отрицает второй термин требования (inbox/outbox) — честный
// пробел сильнее цитаты.
func TestMapCoverageHonestGapBeatsModelQuote(t *testing.T) {
	reqs := mustReqs([]string{"PostgreSQL для хранения состояния и transactional inbox/outbox"}, nil, "go-primary")
	letter := "- PostgreSQL: транзакции, индексы B-tree.\nЧестно о пробелах:\n- Transactional outbox не использовал; близкий опыт — событийный журнал в БД."
	raw := `{"items":[{"text":"PostgreSQL для хранения состояния и transactional inbox/outbox","source":"letter","quote":"PostgreSQL: транзакции, индексы B-tree.","note":"PG есть"}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Covered) != 0 {
		t.Fatalf("честный пробел не должен закрываться цитатой из другой клаузы: %+v", f.Covered)
	}
	if len(f.Caveats) != 1 || !isHonestGap(f.Caveats[0].Note) {
		t.Fatalf("ожидался честный пробел: %+v", f.Caveats)
	}
}

// Промпт разметки несёт вакансию, профиль, письмо и нумерованный список
// требований; правила про отрицание и цитату — в системном промпте.
func TestCoveragePromptContent(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Kafka", "Будет плюсом: опыт с TDD"}, nil, "go-primary")
	system, user := CoveragePrompt(reqs, "ПРОФИЛЬ-МАРКЕР", "ПИСЬМО-МАРКЕР", "ВАКАНСИЯ-МАРКЕР")
	for _, want := range []string{"ВАКАНСИЯ-МАРКЕР", "ПРОФИЛЬ-МАРКЕР", "ПИСЬМО-МАРКЕР", "1. Опыт с Kafka"} {
		if !strings.Contains(user, want) {
			t.Errorf("в user-промпте нет %q", want)
		}
	}
	if strings.Contains(user, "TDD") {
		t.Error("«будет плюсом» не должно попадать в список требований")
	}
	for _, want := range []string{"quote", "unknown", "опыта нет"} {
		if !strings.Contains(system, want) {
			t.Errorf("в системном промпте нет %q", want)
		}
	}
}

// Позиционное сопоставление: модель вернула записи без точного текста
// Требования, но в том же порядке.
func TestMapCoveragePositionalMatch(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Kafka", "Опыт с ClickHouse"}, nil, "go-primary")
	letter := "Стек: Go, Kafka, ClickHouse."
	raw := `{"items":[{"text":"требование 1","source":"letter","quote":"Go, Kafka","note":""},{"text":"требование 2","source":"letter","quote":"Kafka, ClickHouse","note":""}]}`
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	if len(f.Covered) != 2 || f.Verdict != Apply {
		t.Fatalf("позиционное сопоставление должно закрыть оба: %+v", f)
	}
}

// Регресс пропуска модели (defect 2): модель обязана дать запись по каждому
// must-have. Если требование пропущено, MapCoverage fallback-ит на
// coverage() напрямую — иначе оно тихо уходило бы в src=unknown.
func TestMapCoverageMissingItemFallbackToCoverage(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Kafka", "Опыт с ClickHouse", "OpenTelemetry для трейсинга"}, nil, "go-primary")
	// Модель закрыла только первые два, третье — «сделала вид, что проигнорировала».
	raw := `{"items":[{"text":"Опыт с Kafka","source":"letter","quote":"Стек: Go, Kafka","note":""},{"text":"Опыт с ClickHouse","source":"letter","quote":"Go, Kafka, ClickHouse","note":""}]}`
	letter := "Стек: Go, Kafka, ClickHouse."
	f, err := MapCoverage(context.Background(), fakeMapLLM(raw), DefaultConcepts(), reqs, "", letter, "вакансия")
	if err != nil {
		t.Fatalf("MapCoverage: %v", err)
	}
	// Позиционный fallback отключён (2 записи != 3 prompted), поэтому
	// пропущенное требование должно быть пересчитано через coverage().
	// OpenTelemetry в письме отсутствует — покрытие пустое, требование уходит в missing.
	var foundMissing bool
	for _, m := range f.Missing {
		if strings.Contains(m.Text, "OpenTelemetry") {
			foundMissing = true
		}
	}
	// Требование не должно исчезнуть бесследно: fallback должен был его обработать.
	if !foundMissing {
		t.Fatalf("пропущенное требование должно fallback-иться на покрытие, а не исчезать; covered=%+v caveats=%+v missing=%+v", f.Covered, f.Caveats, f.Missing)
	}
}
