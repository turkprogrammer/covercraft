// Тесты для concepts.go: BuildConcept, buildPattern, LoadConcepts,
// ProfileHash, DefaultConcepts, roundtrip Save → Load, GenerateConcepts.
package fit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileHashCoversUppercaseExt — хэш профиля обязан меняться от файла с
// заглавным расширением, иначе правка такого файла не перегенерирует кэш
// концептов и движок продолжит считать устаревшие концепты актуальными.
func TestProfileHashCoversUppercaseExt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "01.md"), []byte("база"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := ProfileHash(dir)
	if err := os.WriteFile(filepath.Join(dir, "02.MD"), []byte("факт"), 0o600); err != nil {
		t.Fatal(err)
	}
	if after := ProfileHash(dir); after == before {
		t.Errorf("хэш не изменился после добавления 02.MD: %q", after)
	}
}

func TestBuildConceptValid(t *testing.T) {
	raw := RawConcept{
		Name:         "тест",
		TriggerTerms: []string{"платёжн", "процессинг"},
		Signals: []RawSignal{
			{Label: "финтех", Terms: []string{"финтех", "банкинг"}},
			{Label: "платежи", Terms: []string{"платёж", "биллинг"}},
		},
	}
	c, err := BuildConcept(raw)
	if err != nil {
		t.Fatalf("BuildConcept: %v", err)
	}
	if c.Name != "тест" {
		t.Errorf("Name = %q, хочу тест", c.Name)
	}
	if c.Trigger == nil || c.Trigger.String() == "" {
		t.Error("Trigger пустой")
	}
	if len(c.Signals) != 2 {
		t.Errorf("Signals = %d, хочу 2", len(c.Signals))
	}
}

func TestBuildConceptShortTermRejected(t *testing.T) {
	raw := RawConcept{
		Name:         "тест",
		TriggerTerms: []string{"платёжн", "go"}, // "go" — 2 руны
		Signals: []RawSignal{
			{Label: "a", Terms: []string{"финтех", "банкинг"}},
			{Label: "b", Terms: []string{"платёж", "биллинг"}},
		},
	}
	if _, err := BuildConcept(raw); err == nil {
		t.Error("короткий термин должен отвергаться")
	}
}

func TestBuildConceptEmptyName(t *testing.T) {
	raw := RawConcept{
		Name:         "  ",
		TriggerTerms: []string{"платёжн", "процессинг"},
		Signals: []RawSignal{
			{Label: "a", Terms: []string{"финтех", "банкинг"}},
			{Label: "b", Terms: []string{"платёж", "биллинг"}},
		},
	}
	if _, err := BuildConcept(raw); err == nil {
		t.Error("пустое имя должно отвергаться")
	}
}

func TestBuildConceptTooFewSignals(t *testing.T) {
	raw := RawConcept{
		Name:         "тест",
		TriggerTerms: []string{"платёжн", "процессинг"},
		Signals: []RawSignal{
			{Label: "только одна", Terms: []string{"финтех", "банкинг"}},
		},
	}
	if _, err := BuildConcept(raw); err == nil {
		t.Error("< 2 signal-групп должно отвергаться")
	}
}

func TestBuildConceptEmptyTerms(t *testing.T) {
	raw := RawConcept{
		Name:         "тест",
		TriggerTerms: []string{"платёжн", ""},
		Signals: []RawSignal{
			{Label: "a", Terms: []string{"финтех", "банкинг"}},
			{Label: "b", Terms: []string{"платёж", "биллинг"}},
		},
	}
	if _, err := BuildConcept(raw); err == nil {
		t.Error("пустой термин должен отвергаться")
	}
}

func TestBuildConceptSpecialCharsEscaped(t *testing.T) {
	// Спецсимволы в терминах должны экранироваться QuoteMeta,
	// а не интерпретироваться как regex.
	raw := RawConcept{
		Name:         "тест",
		TriggerTerms: []string{"foo.bar", "baz+plus"},
		Signals: []RawSignal{
			{Label: "a", Terms: []string{"alpha.beta", "gamma+delta"}},
			{Label: "b", Terms: []string{"epsilon|zeta", "eta.theta"}},
		},
	}
	c, err := BuildConcept(raw)
	if err != nil {
		t.Fatalf("BuildConcept: %v", err)
	}
	// foo.bar должен матчиться как литерал, а не как "любой символ"
	if !c.Trigger.MatchString("foo.bar") {
		t.Error("foo.bar не найден в Trigger")
	}
	if c.Trigger.MatchString("fooxbar") {
		t.Error("foo.bar матчит fooxbar — экранирование не сработало")
	}
}

func TestLoadConceptsValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concepts.json")
	body := `{
		"profile_hash": "sha256:abc",
		"concepts": [
			{
				"name": "тест",
				"trigger_terms": ["платёжн", "процессинг"],
				"signals": [
					{"label": "a", "terms": ["финтех", "банкинг"]},
					{"label": "b", "terms": ["платёж", "биллинг"]}
				]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	concepts, hash, err := LoadConcepts(path)
	if err != nil {
		t.Fatalf("LoadConcepts: %v", err)
	}
	if hash != "sha256:abc" {
		t.Errorf("hash = %q", hash)
	}
	if len(concepts) != 1 || concepts[0].Name != "тест" {
		t.Errorf("concepts = %+v", concepts)
	}
}

func TestLoadConceptsMissing(t *testing.T) {
	_, _, err := LoadConcepts("/nonexistent/path/concepts.json")
	if err == nil {
		t.Error("отсутствующий файл должен давать ошибку")
	}
}

func TestLoadConceptsBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concepts.json")
	if err := os.WriteFile(path, []byte("{битый json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadConcepts(path)
	if err == nil {
		t.Error("битый JSON должен давать ошибку")
	}
}

func TestLoadConceptsAllInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concepts.json")
	// Все концепты короче терминов — должны быть отброшены.
	body := `{
		"profile_hash": "sha256:abc",
		"concepts": [
			{
				"name": "битый",
				"trigger_terms": ["go", "ml"],
				"signals": [
					{"label": "a", "terms": ["a", "b"]},
					{"label": "b", "terms": ["c", "d"]}
				]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadConcepts(path)
	if err == nil {
		t.Error("файл без валидных концептов должен давать ошибку")
	}
}

func TestProfileHash(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	os.WriteFile(a, []byte("Профиль Go-разработчика."), 0o600)
	os.WriteFile(b, []byte("Опыт: Kafka, PostgreSQL."), 0o600)
	h1 := ProfileHash(dir)
	if h1 == "" {
		t.Fatal("ProfileHash вернул пустую строку при наличии файлов")
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("хэш без префикса: %q", h1)
	}
	// Изменение одного символа должно менять хэш.
	os.WriteFile(b, []byte("Опыт: Kafka, PostgreSQL!"), 0o600)
	h2 := ProfileHash(dir)
	if h2 == h1 {
		t.Error("изменение файла не изменило хэш")
	}
}

func TestProfileHashEmpty(t *testing.T) {
	// Пустой каталог — пустой хэш, без паники.
	if h := ProfileHash(t.TempDir()); h != "" {
		t.Errorf("пустой каталог: %q, хочу \"\"", h)
	}
	// Несуществующий путь — пустой хэш, без паники.
	if h := ProfileHash("/nonexistent/path/that/does/not/exist"); h != "" {
		t.Errorf("несуществующий путь: %q, хочу \"\"", h)
	}
	// Пустая строка — пустой хэш, без паники.
	if h := ProfileHash(""); h != "" {
		t.Errorf("пустая строка: %q, хочу \"\"", h)
	}
}

func TestDefaultConceptsNotEmpty(t *testing.T) {
	concepts := DefaultConcepts()
	if len(concepts) == 0 {
		t.Fatal("DefaultConcepts() вернул пустой список")
	}
	// Все концепты должны иметь непустые имя, триггер и сигналы.
	for i, c := range concepts {
		if c.Name == "" {
			t.Errorf("концепт[%d]: пустое имя", i)
		}
		if c.Trigger == nil {
			t.Errorf("концепт[%d] %q: пустой Trigger", i, c.Name)
		}
		if len(c.Signals) < 2 {
			t.Errorf("концепт[%d] %q: < 2 сигналов", i, c.Name)
		}
	}
}

func TestDefaultConceptsKnownNames(t *testing.T) {
	// Должны сохраниться все 11 концептов из старого var concepts.
	concepts := DefaultConcepts()
	want := map[string]bool{
		"распределённые системы":                     false,
		"высоконагруженные системы":                  false,
		"эксплуатация и observability":               false,
		"архитектурное управление":                   false,
		"модернизация legacy":                        false,
		"многопоточность и жизненный цикл":           false,
		"ООП/SOLID/паттерны":                         false,
		"алгоритмы и структуры данных":               false,
		"backend-разработка":                         false,
		"гарантии консистентности и идемпотентности": false,
		"интеграция с платёжными процессингами":      false,
	}
	for _, c := range concepts {
		if _, ok := want[c.Name]; ok {
			want[c.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("DefaultConcepts потерял концепт %q", name)
		}
	}
}

// TestSaveLoadRoundtrip — P0: roundtrip Save → Load должен возвращать
// концепты с теми же триггерами и сигналами. До фикса SaveConceptsFile
// заполнял только Name, и LoadConcepts отбраковывал всё при чтении.
func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concepts.json")
	raw := &rawConceptsFile{
		Concepts: []RawConcept{
			{
				Name:         "интеграция с платёжными процессингами",
				TriggerTerms: []string{"платёжн", "процессинг", "эквайринг"},
				Signals: []RawSignal{
					{Label: "финтех/банкинг", Terms: []string{"финтех", "банкинг", "эквайринг"}},
					{Label: "платежи/payment", Terms: []string{"платёж", "биллинг", "payment"}},
				},
			},
		},
	}
	hash := "sha256:abc123"
	if err := SaveConceptsFile(path, raw, hash); err != nil {
		t.Fatalf("SaveConceptsFile: %v", err)
	}
	concepts, savedHash, err := LoadConcepts(path)
	if err != nil {
		t.Fatalf("LoadConcepts: %v", err)
	}
	if savedHash != hash {
		t.Errorf("hash = %q, хочу %q", savedHash, hash)
	}
	if len(concepts) != 1 {
		t.Fatalf("len(concepts) = %d, хочу 1", len(concepts))
	}
	c := concepts[0]
	if c.Name != "интеграция с платёжными процессингами" {
		t.Errorf("Name = %q", c.Name)
	}
	// Триггер должен ловить свои термины и не ловить чужие.
	if !c.Trigger.MatchString("платёжные системы") {
		t.Error("Trigger не ловит «платёжные»")
	}
	if !c.Trigger.MatchString("эквайринг") {
		t.Error("Trigger не ловит «эквайринг»")
	}
	// Сигналы должны быть доступны.
	if len(c.Signals) != 2 {
		t.Errorf("Signals = %d, хочу 2", len(c.Signals))
	}
	// Проверяем, что сигналы реально работают (а не просто сохранены).
	if name, hits := conceptHit(concepts, "опыт процессингов", "финтех и банкинг, платёж и биллинг", 2); name == "" {
		t.Error("conceptHit не закрыл требование — сигналы не восстановились")
	} else {
		t.Logf("conceptHit: %s, hits=%v", name, hits)
	}
}

// TestGenerateConceptsMock — мок LLM возвращает JSON с терминами,
// GenerateConcepts парсит и компилирует regex. После фикса также
// возвращает raw для roundtrip-сохранения.
func TestGenerateConceptsMock(t *testing.T) {
	raw := `{"concepts":[
		{
			"name":"тест",
			"trigger_terms":["платёжн","процессинг"],
			"signals":[
				{"label":"a","terms":["финтех","банкинг"]},
				{"label":"b","terms":["платёж","биллинг"]}
			]
		}
	]}`
	fn := func(ctx context.Context, system, user string) (string, error) {
		return raw, nil
	}
	concepts, r, err := GenerateConcepts(context.Background(), fn, "профиль кандидата")
	if err != nil {
		t.Fatalf("GenerateConcepts: %v", err)
	}
	if len(concepts) != 1 {
		t.Fatalf("len = %d, хочу 1", len(concepts))
	}
	if r == nil {
		t.Fatal("raw == nil — roundtrip невозможен")
	}
	if len(r.Concepts) != 1 {
		t.Errorf("raw.Concepts = %d, хочу 1", len(r.Concepts))
	}
	// Roundtrip: сохраняем и читаем обратно.
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	if err := SaveConceptsFile(path, r, "sha256:xyz"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, hash, err := LoadConcepts(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if hash != "sha256:xyz" {
		t.Errorf("hash = %q", hash)
	}
	if len(loaded) != 1 || loaded[0].Name != "тест" {
		t.Errorf("loaded = %+v", loaded)
	}
}

// TestGenerateConceptsEmpty — пустой массив concepts и мусор должны давать ошибку.
func TestGenerateConceptsEmpty(t *testing.T) {
	fn := func(ctx context.Context, system, user string) (string, error) {
		return `{"concepts":[]}`, nil
	}
	if _, _, err := GenerateConcepts(context.Background(), fn, "p"); err == nil {
		t.Error("пустой массив должен давать ошибку")
	}
	fn2 := func(ctx context.Context, system, user string) (string, error) {
		return "мусор без json", nil
	}
	if _, _, err := GenerateConcepts(context.Background(), fn2, "p"); err == nil {
		t.Error("мусор должен давать ошибку")
	}
}

// TestConceptHitDynamic — регресс-покрытие: с DefaultConcepts() conceptHit
// должен закрывать известные концепты. Если кто-то случайно сломает
// DefaultConcepts (например, удалит концепт «распределённые системы»),
// этот тест упадёт — и мы заметим регресс поведения.
func TestConceptHitDynamic(t *testing.T) {
	concepts := DefaultConcepts()
	tests := []struct {
		req    string
		letter string
		name   string
	}{
		{
			req:    "опыт проектирования распределённых систем",
			letter: "опыт с Kafka, очереди и брокеры, микросервисы на gRPC",
			name:   "распределённые системы",
		},
		{
			req:    "интеграция с платёжными процессингами",
			letter: "опыт в финтехе и банкинге, платёжные системы, процессинг и шлюзы",
			name:   "интеграция с платёжными процессингами",
		},
		{
			req:    "эксплуатация и observability",
			letter: "Prometheus + Grafana, алертинг по SLO, дашборды по метрикам",
			name:   "эксплуатация и observability",
		},
	}
	for _, tt := range tests {
		name, _ := conceptHit(concepts, tt.req, tt.letter, 2)
		if name != tt.name {
			t.Errorf("conceptHit(%q, %q) = %q, хочу %q", tt.req, tt.letter, name, tt.name)
		}
	}
}

// TestBuildConceptTooFewTriggers — minTriggerTerms из плана.
func TestBuildConceptTooFewTriggers(t *testing.T) {
	raw := RawConcept{
		Name:         "тест",
		TriggerTerms: []string{"только-один-длинный-термин"}, // 1 штука
		Signals: []RawSignal{
			{Label: "a", Terms: []string{"финтех", "банкинг"}},
			{Label: "b", Terms: []string{"платёж", "биллинг"}},
		},
	}
	if _, err := BuildConcept(raw); err == nil {
		t.Error("< 2 trigger_terms должен отвергаться")
	}
}

// TestConceptHitGaming — требование «Опыт в игровой индустрии» не содержит ни
// одного латинского токена, поэтому закрывается только концептом: без него
// вердикт по игровому требованию всегда «нет данных», сколько бы фактов ни
// лежало в профиле.
func TestConceptHitGaming(t *testing.T) {
	concepts := DefaultConcepts()
	const req = "Опыт в игровой индустрии — Опыт работы в индустрии видеоигр, " +
		"разработки или эксплуатации платформ для разработчиков игр, издателей или игроков"
	const letter = "Игра Pathfinder Chronicles: sci-fi выживач на Symfony 6.1 + Redis, " +
		"hexagonal, 61 YAML-сцена и 7 концовок, публичный open-source (MIT)."

	if name, _ := conceptHit(concepts, req, letter, 2); name != "игровая индустрия и геймдев" {
		t.Errorf("conceptHit по игровому требованию = %q, хочу «игровая индустрия и геймдев»", name)
	}
	// Тот же факт в профиле (письмо могло не назвать проект) — тоже закрывает.
	const profile = "## 12. ИГРОВАЯ ИНДУСТРИЯ — Pathfinder Chronicles\n" +
		"- 4 главы, 61 YAML-сцена, 7 концовок, survival на Symfony + Redis"
	if name, _ := conceptHit(concepts, req, profile, 2); name != "игровая индустрия и геймдев" {
		t.Errorf("conceptHit по профилю = %q, хочу «игровая индустрия и геймдев»", name)
	}
}

// TestConceptHitGamingIgnoresMigrations — триггер игрового концепта не должен
// ловить «миграции»: в RE2 нет границ слов для кириллицы, и наивное `игр`
// матчится внутри «миграция», а такое требование не про игры.
func TestConceptHitGamingIgnoresMigrations(t *testing.T) {
	for _, c := range DefaultConcepts() {
		if c.Name != "игровая индустрия и геймдев" {
			continue
		}
		for _, req := range []string{"опыт с миграциями без даунтайма", "миграция легаси на PostgreSQL"} {
			if c.Trigger.MatchString(req) {
				t.Errorf("триггер игрового концепта сработал на %q", req)
			}
			if _, ok := conceptHonestGap([]Concept{c}, req, "С миграциями не работал"); ok {
				t.Errorf("требование %q не должно уходить в игровой честный пробел", req)
			}
		}
		return
	}
	t.Error("концепт «игровая индустрия и геймдев» отсутствует в DefaultConcepts")
}

// TestGamingConceptHonestGapNotCovered — письмо с честным пробелом по теме
// концепта не должно закрывать требование: раньше «Игровая индустрия — пробел:
// нет опыта» попадала в «закрыто» по упоминанию темы.
func TestGamingConceptHonestGapNotCovered(t *testing.T) {
	reqs := mustReqs([]string{"Опыт в игровой индустрии"}, nil, "other")
	letter := "Go/PHP-бэкенд, Kafka, PostgreSQL.\n\nИгровая индустрия — пробел: нет опыта."
	profile := "ИГРОВАЯ ИНДУСТРИЯ — Pathfinder Chronicles: 61 сцена, 7 концовок, Symfony + Redis."

	res := Evaluate(DefaultConcepts(), reqs, profile, letter, "Требуется опыт в игровой индустрии")
	if len(res.Covered) != 0 {
		t.Errorf("честно названный пробел не должен закрывать требование: %+v", res.Covered)
	}
}
