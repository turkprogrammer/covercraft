package fit

import (
	"regexp"
	"strings"
	"testing"
)

// --- АФЛТ-Системс, Go Backend Developer (октябрь 2026) ---------------------
//
// Один и тот же факт — опыт REST API — в двух прогонах был записан по-разному
// («REST API» через пробел и «REST-API» через дефис) и дал противоположные
// вердикты. Ниже — регрессии на каждое расхождение.

const aeroflotProfile = "PostgreSQL: индексы B-tree/GIN, execution plans, Safe EXPLAIN; " +
	"Stable ID — Kafka consumer groups (Train, Export, Stream), at-least-once, 10 000 RPS; " +
	"Bundle ID — классификация iOS/Android; Geo-mapping Service — 3.1M+ записей; " +
	"Prometheus + Grafana (3 дашборда, 23 панели); gRPC (банк Росгосстрах); " +
	"155+ тестов (unit, integration, E2E); ADR (10), Hexagonal, DDD, SOLID"

// TestRestAPISpellingDoesNotMatter — «REST API» и «REST-API» — одно и то же
// название. До фикса токен требования «rest-api» не находился в письме с
// «REST API», и обязательное требование уходило в «не закрыто», роняя вердикт.
func TestRestAPISpellingDoesNotMatter(t *testing.T) {
	for _, letter := range []string{
		"Микросервисы: REST API — laravel-api, URL-Shortener; gRPC (банк Росгосстрах), HTTP.",
		"Микросервисы: REST-API (laravel-api, URL-Shortener); gRPC (банк Росгосстрах), HTTP.",
	} {
		reqs := Requirements{MustHave: []Requirement{
			{Text: "Опыт работы с REST-API", Kind: "must"},
		}}
		f := Evaluate(DefaultConcepts(), reqs, aeroflotProfile, letter, "")
		if len(f.Missing) != 0 {
			t.Errorf("письмо %q: требование REST-API должно закрываться, получено %+v", letter, f.Missing)
		}
	}
}

// TestJSONRPCNegationInTailClause — честный пробел в конце буллета адаптации.
// sentences() режет по «;», поэтому «не применял» оказывалось в соседней
// клаузе, а требование видело «JSON-RPC» без отрицания и закрывалось
// «закрыто в письме». Письмо говорило ровно обратное.
func TestJSONRPCNegationInTailClause(t *testing.T) {
	const letter = "Адаптация под ваш стек:\n" +
		"- JSON-RPC: REST API — laravel-api, URL-Shortener; JSON-RPC — не применял, готов оперативно освоить."
	reqs := Requirements{MustHave: []Requirement{
		{Text: "Опыт работы с JSON-RPC", Kind: "must"},
	}}
	f := Evaluate(DefaultConcepts(), reqs, aeroflotProfile, letter, "")
	for _, c := range f.Covered {
		if strings.Contains(c.Text, "JSON-RPC") {
			t.Fatalf("честно названный пробел не может быть «закрыто в письме»: %+v", f.Covered)
		}
	}
	if len(f.Missing) != 0 {
		t.Errorf("пробел, названный письмом, — это не «не закрыто ничем»: %+v", f.Missing)
	}
}

// TestProtobufListedInAdaptationHead — технология в ГОЛОВЕ буллета-адаптации
// («Protobuf/JSON-RPC: …») называет стек работодателя. Хвост того же буллета
// («Protobuf и JSON-RPC — естественное расширение») говорит о том же, и опытом
// не является. При этом опыт в той же строке остаётся опытом.
func TestProtobufListedInAdaptationHead(t *testing.T) {
	const letter = "Микросервисы: gRPC (банк Росгосстрах), event-driven (Kafka), REST-API (laravel-api), HTTP.\n" +
		"Адаптация под ваш стек:\n" +
		"  - Protobuf/JSON-RPC: имею опыт работы с gRPC (банк Росгосстрах) и REST-API (laravel-api, URL-Shortener); " +
		"Protobuf и JSON-RPC — естественное расширение протокольного стека, готов оперативно освоить спецификации вашего проекта."

	reqs := Requirements{MustHave: []Requirement{{Text: "Опыт работы с Protobuf", Kind: "must"}}}
	f := Evaluate(DefaultConcepts(), reqs, aeroflotProfile, letter, "")
	for _, c := range f.Covered {
		if strings.Contains(c.Text, "Protobuf") {
			t.Fatalf("Protobuf назван пробелом, а отчёт говорит «закрыто в письме»: %+v", f.Covered)
		}
	}

	reqs = Requirements{MustHave: []Requirement{{Text: "Опыт работы с gRPC", Kind: "must"}}}
	f = Evaluate(DefaultConcepts(), reqs, aeroflotProfile, letter, "")
	if len(f.Missing) != 0 {
		t.Errorf("gRPC в этой же строке — реальный факт: %+v", f.Missing)
	}
}

// TestEventDrivenRussianWording — «потоковая и событийная архитектура» это
// event-driven. Триггер знал только английское написание, и русская
// формулировка вакансии уходила в «нет данных» при живом event-driven в письме.
func TestEventDrivenRussianWording(t *testing.T) {
	const letter = "System Design: ADR (10), Hexagonal, DDD, SOLID; отказоустойчивость: retries " +
		"(exponential backoff), deadlines, идемпотентность (ClickHouse Upsert), at-least-once (Kafka consumer groups); " +
		"проектирование event-driven архитектур."
	reqs := Requirements{MustHave: []Requirement{
		{Text: "Применение потоковой и событийной архитектуры в разработке", Kind: "must"},
	}}
	f := Evaluate(DefaultConcepts(), reqs, aeroflotProfile, letter, "")
	if len(f.Missing) != 0 {
		t.Errorf("русская формулировка event-driven должна узнаваться: %+v", f.Missing)
	}
}

// TestConceptSignalsRejectHomonyms — латинские термины без \b ловились внутри
// других слов: stream ⊂ downstream, player ⊂ multiplayer, race ⊂ graceful,
// layer ⊂ player, span ⊂ spanned, exact ⊂ exactly.
func TestConceptSignalsRejectHomonyms(t *testing.T) {
	cases := []struct{ label, text string }{
		{"видео/медиа-пайплайн", "downstream-синхронизация опрашивает журнал (polling-потребитель)"},
		{"видео/медиа-пайплайн", "Kafka consumer groups (Train, Export, Stream)"},
		{"видео/медиа-пайплайн", "автоматический экспорт моделей каждые 30 минут"},
		{"тестовая инфраструктура", "graceful shutdown — ProcessManager, 20+ воркеров"},
		{"паттерны/архитектурные стили", "multiproplayer lobby на 200 игроков"},
		{"трейсинг/телеметрия", "операции spanning несколько минут"},
		{"precision/точность", "Kafka at-least-once не exactly-once"},
	}
	signals := map[string]*regexp.Regexp{}
	for _, c := range DefaultConcepts() {
		for _, s := range c.Signals {
			if _, ok := signals[s.Label]; !ok {
				signals[s.Label] = s.Re
			}
		}
	}
	for _, tc := range cases {
		re, ok := signals[tc.label]
		if !ok {
			t.Fatalf("сигнал %q не найден", tc.label)
		}
		if re.MatchString(tc.text) {
			t.Errorf("сигнал %q ловит омоним в %q", tc.label, tc.text)
		}
	}
}

// TestProfileGapLinesGiveNoFacts — ограничители профиля не должны выдавать
// факты. Каждая строка ниже называет технологию, чтобы её НЕ заявлять.
func TestProfileGapLinesGiveNoFacts(t *testing.T) {
	cases := []struct{ profile, token string }{
		{"- НЕ писать про Canvas/WebGL-рендер: фронтенд — Twig + CSS", "render"},
		{"- Состояние игры — Redis, а не СУБД: НЕ писать про PostgreSQL/ORM", "postgres"},
		{"- Kubernetes — ВСЕГДА честный пробел: опыта администрирования нет", "k8s"},
		{"- MinIO / S3: в профиле НЕТ — только честный пробел", "s3"},
		{"- OpenTelemetry: опыта интеграции НЕТ — честный пробел", "opentelemetry"},
	}
	for _, tc := range cases {
		if findFact(tc.token, tc.profile, tc.profile) {
			t.Errorf("ограничитель %q даёт ложный факт по %q", tc.profile, tc.token)
		}
	}
}

// TestVideoNotClosedByObservability — требование про видеорендеринг не имеет
// в профиле ни одного медиа-сигнала (омонимы «downstream»/«Stream»/«экспорт
// моделей» раньше давали их), поэтому закрываться соседним, более общим
// концептом observability — дашбордами Prometheus — оно не должно.
func TestVideoNotClosedByObservability(t *testing.T) {
	reqs := Requirements{MustHave: []Requirement{
		{Text: "Обеспечение быстрого и отказоустойчивого рендеринга видео", Kind: "must"},
	}}
	f := Evaluate(DefaultConcepts(), reqs, aeroflotProfile, "Письмо о бэкенде на Go и Kafka.", "")
	for _, c := range f.Covered {
		if strings.Contains(c.Note, "observability") {
			t.Fatalf("видео закрыто observability: %+v", f.Covered)
		}
	}
}

// TestVideoRequirementStaysHonest — медиа-концепт не должен набирать сигналы
// из посторонних слов профиля.
func TestVideoRequirementStaysHonest(t *testing.T) {
	name, gap := conceptPrimaryGap(DefaultConcepts(),
		"Обеспечение быстрого и отказоустойчивого рендеринга видео", "Письмо о Go.", aeroflotProfile)
	if !gap {
		t.Fatalf("медиа-пробел не распознан, вернётся ложное закрытие (первый концепт %q)", name)
	}
}

// TestConjunctiveListSplitIsDeterministic — «Опыт работы с gRPC, Protobuf,
// REST-API, JSON-RPC, HTTP» — конъюнкция, и разбивать её должен код, а не
// модель: два прогона на одних данных давали «не откликаться» и «с оговоркой».
func TestConjunctiveListSplitIsDeterministic(t *testing.T) {
	const whole = `{"mustHave":[{"text":"Опыт работы с gRPC, Protobuf, REST-API, JSON-RPC, HTTP","kind":"must"}]}`
	reqs, err := ParseExtraction(whole)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(reqs.MustHave) != 5 {
		t.Fatalf("ожидалось 5 отдельных требований, получено %d: %+v", len(reqs.MustHave), reqs.MustHave)
	}
	for _, want := range []string{"gRPC", "Protobuf", "REST-API", "JSON-RPC", "HTTP"} {
		found := false
		for _, r := range reqs.MustHave {
			if strings.Contains(r.Text, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("технология %q потерялась при разбиении: %+v", want, reqs.MustHave)
		}
	}
}

// TestShortListsAreNotSplit — перечисление из двух технологий не трогаем:
// в требованиях это часто описание роли, а разбиение добавит шум.
func TestShortListsAreNotSplit(t *testing.T) {
	const raw = `{"mustHave":[{"text":"Kafka, PostgreSQL","kind":"must"}]}`
	reqs, err := ParseExtraction(raw)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(reqs.MustHave) != 1 {
		t.Errorf("короткий список разбит: %+v", reqs.MustHave)
	}
}

// TestSameInputsGiveSameVerdict — главная проверка D10: одно требование, честно
// названный пробел по Protobuf и JSON-RPC. Ни один из них не должен
// превращаться в «не закрыто ничем», и вердикт не должен быть skip.
func TestSameInputsGiveSameVerdict(t *testing.T) {
	const letter = "Микросервисы: gRPC (банк Росгосстрах), REST API (laravel-api, URL-Shortener), HTTP.\n" +
		"Адаптация: Protobuf/JSON-RPC — естественное расширение, готов оперативно освоить."
	reqs := Requirements{MustHave: []Requirement{
		{Text: "Опыт работы с gRPC", Kind: "must"},
		{Text: "Опыт работы с Protobuf", Kind: "must"},
		{Text: "Опыт работы с REST-API", Kind: "must"},
		{Text: "Опыт работы с JSON-RPC", Kind: "must"},
		{Text: "Опыт работы с HTTP", Kind: "must"},
	}}
	f := Evaluate(DefaultConcepts(), reqs, aeroflotProfile, letter, "")
	if f.Verdict == Skip {
		t.Errorf("честно названные пробелы не должны давать skip: %+v", f.Advice)
	}
	for _, c := range f.Missing {
		t.Errorf("пробел назван письмом — это не «не закрыто ничем»: %q", c.Text)
	}
}

// TestVerdictIsIndependentOfModelSplitting — причина расхождения вердиктов:
// модель решала, разбивать ли «Опыт работы с gRPC, Protobuf, REST-API,
// JSON-RPC, HTTP». Один прогон давал «не откликаться», другой — «с оговоркой».
// Теперь разбиение выполняет код, поэтому оба разбора обязаны давать один
// вердикт на одном письме.
func TestVerdictIsIndependentOfModelSplitting(t *testing.T) {
	const letter = "Микросервисы: gRPC (банк Росгосстрах), REST API (laravel-api, URL-Shortener), HTTP.\n" +
		"Адаптация: Protobuf/JSON-RPC — естественное расширение, готов оперативно освоить."
	const combined = `{"mustHave":[{"text":"Опыт работы с gRPC, Protobuf, REST-API, JSON-RPC, HTTP","kind":"must"}]}`
	const split = `{"mustHave":[
		{"text":"Опыт работы с gRPC","kind":"must"},
		{"text":"Опыт работы с Protobuf","kind":"must"},
		{"text":"Опыт работы с REST-API","kind":"must"},
		{"text":"Опыт работы с JSON-RPC","kind":"must"},
		{"text":"Опыт работы с HTTP","kind":"must"}]}`

	a, err := ParseExtraction(combined)
	if err != nil {
		t.Fatalf("разбор 1: %v", err)
	}
	b, err := ParseExtraction(split)
	if err != nil {
		t.Fatalf("разбор 2: %v", err)
	}
	fa := Evaluate(DefaultConcepts(), a, aeroflotProfile, letter, "")
	fb := Evaluate(DefaultConcepts(), b, aeroflotProfile, letter, "")
	if fa.Verdict != fb.Verdict || fa.Score != fb.Score {
		t.Errorf("вердикт зависит от разбора модели: %q/%d против %q/%d\n%v\n%v",
			fa.Verdict, fa.Score, fb.Verdict, fb.Score, fa.Advice, fb.Advice)
	}
	if fa.Verdict == Skip {
		t.Errorf("честно названные пробелы дали skip: %v", fa.Advice)
	}
}
