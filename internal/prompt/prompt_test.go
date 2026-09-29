package prompt

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// defaultBase — усечённая база для тестов: инварианты обязаны прийти из
// самой Compose, а не из base.
const defaultBase = "Базовый системный промпт."

var testSections = []Section{
	{File: "01.md", Heading: "## PHP"},
	{File: "03.md", Heading: "## ML"},
	{File: "02.md", Heading: "## ФАКТЫ-ОГРАНИЧИТЕЛИ ИЗ ДОКОВ (не выдумывать сверх)"},
}

func TestComposeCarriesInvariantsIntoSystem(t *testing.T) {
	system, _ := Compose(defaultBase, "Вакансия PHP", "php-primary",
		[]string{"опыт PHP 5+ лет"}, []Section{{File: "01.md", Heading: "## PHP"}})
	for _, want := range []string{
		"факты из профиля", "ничего не выдумывай", "Язык письма", "до 200 слов",
		"приветствие", "подпись: если в инструкции уже задана дословная подпись", "не излагай пробел как слабость",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("в system композера нет инварианта %q", want)
		}
	}
	if !strings.HasPrefix(system, defaultBase) {
		t.Error("base должна идти первым куском system — мета-инструкция поверх неё")
	}
}

func TestComposeUserCarriesVacancyMustsAndSections(t *testing.T) {
	_, user := Compose(defaultBase, "Вакансия PHP", "php-primary",
		[]string{"опыт PHP 5+ лет", "  "}, testSections)
	for _, want := range []string{
		"Вакансия PHP",
		"- опыт PHP 5+ лет", // каждый must попал в user
		"01.md :: ## PHP",   // квалифицированный заголовок, не голый "## PHP"
		"03.md :: ## ML",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("в user композера нет %q\n---\n%s", want, user)
		}
	}
	if strings.Contains(user, "\n-  \n") {
		t.Error("пустые musts обязаны отбрасываться")
	}
}

func TestComposeRoleDirectiveOnlyForKnownRole(t *testing.T) {
	systemML, _ := Compose(defaultBase, "Вакансия", "ml-research", nil, nil)
	if !strings.Contains(systemML, "ML-опыт") {
		t.Error("для ml-research в system должна попасть доменная директива")
	}
	systemUnknown, _ := Compose(defaultBase, "Вакансия", "самодельная-роль", nil, nil)
	if strings.Contains(systemUnknown, "ML-опыт") || strings.Contains(systemUnknown, "PHP-вакансии") {
		t.Error("неизвестная роль не должна получать доменные директивы")
	}
}

func TestComposeEmptyContractIsValid(t *testing.T) {
	system, user := Compose(defaultBase, "Вакансия X", "other", nil, nil)
	if strings.TrimSpace(system) == "" || !strings.Contains(user, "Вакансия X") {
		t.Error("пустые musts и sections не должны ронять Compose")
	}
}

func TestParseStripsFenceAndPreamble(t *testing.T) {
	raw := "Вот ответ:\n```json\n" +
		`{"systemPrompt":"промпт под вакансию","dropSections":[{"file":"03.md","heading":"## ML"}],"reason":"PHP — ML не релевантен"}` +
		"\n```"
	res, err := Parse(raw, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if res.SystemPrompt != "промпт под вакансию" {
		t.Errorf("systemPrompt = %q", res.SystemPrompt)
	}
	if len(res.Drop) != 1 || res.Drop[0].Heading != "## ML" {
		t.Errorf("дропы: %+v", res.Drop)
	}
	if res.Reason != "PHP — ML не релевантен" {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestParseDropsUnknownHeading(t *testing.T) {
	raw := `{"systemPrompt":"п","dropSections":[` +
		`{"file":"03.md","heading":"## ML"},` +
		`{"file":"xx.md","heading":"## Выдуманный раздел"}],"reason":"r"}`
	res, err := Parse(raw, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Drop) != 1 || res.Drop[0].Heading != "## ML" {
		t.Errorf("несуществующий заголовок обязан отброситься, дропы: %+v", res.Drop)
	}
}

func TestParseNeverDropsProtectedSection(t *testing.T) {
	// Хард-защита: заголовок реально существует в sections, но вырезать
	// «ФАКТЫ-ОГРАНИЧИТЕЛИ» нельзя — антигаллюцинация важнее выигрыша в токенах.
	raw := `{"systemPrompt":"п","dropSections":[` +
		`{"file":"02.md","heading":"## ФАКТЫ-ОГРАНИЧИТЕЛИ ИЗ ДОКОВ (не выдумывать сверх)"},` +
		`{"file":"03.md","heading":"## ML"}],"reason":"r"}`
	res, err := Parse(raw, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Drop) != 1 || res.Drop[0].Heading != "## ML" {
		t.Errorf("защищённый раздел обязан остаться, дропы: %+v", res.Drop)
	}
}

func TestParseEmptySystemPromptIsError(t *testing.T) {
	if _, err := Parse(`{"systemPrompt":"  ","dropSections":[]}`, testSections); err == nil {
		t.Error("пустой systemPrompt → ошибка, иначе UI затрёт поле")
	}
	if _, err := Parse("без мусора и JSON", testSections); err == nil {
		t.Error("ответ без JSON → ошибка")
	}
	if _, err := Parse("{битый JSON}", testSections); err == nil {
		t.Error("битый JSON → ошибка")
	}
}

func TestParseTruncatesAtLineBoundary(t *testing.T) {
	total := maxPromptLines + 15
	var b strings.Builder
	for i := 0; i < total; i++ {
		b.WriteString("строка номер ")
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString("\n")
	}
	long := b.String() // строк больше лимита → обрезка по строкам
	res, err := Parse(`{"systemPrompt":`+quoteJSON(long)+`}`, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.Truncated {
		t.Errorf("%d строк при лимите %d → Truncated=true", total, maxPromptLines)
	}
	if got := strings.Count(res.SystemPrompt, "\n") + 1; got > maxPromptLines {
		t.Errorf("строк в промпте = %d, лимит %d", got, maxPromptLines)
	}
	wantTail := "строка номер " + string(rune('a'+(maxPromptLines-1)%26))
	if !strings.HasSuffix(res.SystemPrompt, wantTail) {
		t.Errorf("обрезка должна идти по границе строки, ждали хвост %q, получили: %q", wantTail, res.SystemPrompt)
	}

	// Одна длинная строка без переводов строк → обрезка по байтам на границе.
	huge := strings.Repeat("x", maxPromptChars+600)
	res2, err := Parse(`{"systemPrompt":`+quoteJSON(huge)+`}`, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res2.Truncated || len(res2.SystemPrompt) > maxPromptChars {
		t.Errorf("truncated=%v len=%d", res2.Truncated, len(res2.SystemPrompt))
	}
}

// TestParseDropsMismatchedFile — заголовок существует, но имя файла чужое:
// такой дроп не вырежет ничего (cover фильтрует дропы по файлу), поэтому Parse
// обязан его отбросить, а не отчитаться в UI об успехе, которого нет.
func TestParseDropsMismatchedFile(t *testing.T) {
	raw := `{"systemPrompt":"п","dropSections":[` +
		`{"file":"03.md","heading":"## ML"},` +
		`{"file":"99.md","heading":"## ML"}],"reason":"r"}`
	res, err := Parse(raw, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Drop) != 1 || res.Drop[0].File != "03.md" {
		t.Errorf("дроп с чужим файлом обязан отброситься, дропы: %+v", res.Drop)
	}

	// Регистр имени файла не важен: это по-прежнему тот же файл.
	rawCase := `{"systemPrompt":"п","dropSections":[{"file":"03.MD","heading":"## ML"}]}`
	res2, err := Parse(rawCase, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res2.Drop) != 1 {
		t.Errorf("регистр имени файла не должен ломать дроп: %+v", res2.Drop)
	}
}

// TestParseTruncatesCyrillicWithoutBreakingRunes — байтовая обрезка при
// отсутствии перевода строки не должна рвать UTF-8-руну: иначе в запрос к
// провайдеру уйдёт невалидный UTF-8. Префикс в один байт ставит границу
// лимита ровно на продолжение кириллической руны — без гарда тест падает.
func TestParseTruncatesCyrillicWithoutBreakingRunes(t *testing.T) {
	long := "a" + strings.Repeat("ф", maxPromptChars) // 1 + 2*maxPromptChars байт, ни одного перевода строки
	res, err := Parse(`{"systemPrompt":`+quoteJSON(long)+`}`, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.Truncated {
		t.Errorf("%d байт при лимите %d → Truncated=true", len(long), maxPromptChars)
	}
	if len(res.SystemPrompt) > maxPromptChars {
		t.Errorf("длина = %d байт, лимит %d", len(res.SystemPrompt), maxPromptChars)
	}
	if !utf8.ValidString(res.SystemPrompt) {
		t.Errorf("обрезка разорвала руну, хвост: %q", res.SystemPrompt[len(res.SystemPrompt)-4:])
	}
}

// TestMissingInvariants — мягкая проверка безопасности playbook'а: имена
// потерянных инвариантов уходят в Result.Missing, а не роняют Parse.
func TestMissingInvariants(t *testing.T) {
	res, err := Parse(`{"systemPrompt":"пиши хорошо"}`, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Missing) == 0 {
		t.Error("промпт без инвариантов обязан дать непустой Missing")
	}

	full := `{"systemPrompt":"Только факты из профиля, ничего не выдумывай. Язык письма — по вакансии. Объём — до 200 слов. Завершай подписью именем. Пробел — не слабость."}`
	res2, err := Parse(full, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res2.Missing) != 0 {
		t.Errorf("все инварианты на месте, Missing = %v", res2.Missing)
	}

	// Прямой контракт хелпера: перефразирование тоже считается нахождением.
	if got := MissingInvariants("Язык — по вакансии, до 200 слов, подпись есть, не выдумывай, пробел как мост"); len(got) != 0 {
		t.Errorf("MissingInvariants = %v, хочу пусто", got)
	}
}

// TestParseAttributesLostInvariants — инвариант, срезанный лимитом, и
// инвариант, потерянный моделью, — это разные поломки: лечатся по-разному.
// Missing честно перечисляет оба (в письмо уходит промпт без них), а
// MissingCut отделяет вину clamp'а от вины модели. Это регресс на решение
// «считать инварианты до обрезки»: так предупреждение врало бы, что виновата
// модель, и потеря инварианта в финальном промпте проходила бы молча.
func TestParseAttributesLostInvariants(t *testing.T) {
	// Хвост с «пробел — не слабость» уходит за лимит: модель инвариант
	// написала, clamp его срезал.
	head := "Только факты из профиля, ничего не выдумывай. Язык письма — по вакансии. Объём — до 200 слов. Завершай подписью именем.\n"
	raw := `{"systemPrompt":` + quoteJSON(head+strings.Repeat("Подробности.\n", 400)+"Пробел — не слабость.") + `}`
	res, err := Parse(raw, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.Truncated {
		t.Fatalf("преmise: длинный промпт обязан обрезаться, len = %d", len(head))
	}
	if len(res.Missing) == 0 {
		t.Fatal("срезанный инвариант обязан попасть в Missing — он отсутствует в письме")
	}
	if len(res.MissingCut) == 0 {
		t.Errorf("Missing пуст при обрезке: виноват clamp, а не модель — Missing = %v", res.Missing)
	}
	for _, inv := range res.MissingCut {
		if !contains(res.Missing, inv) {
			t.Errorf("MissingCut %q не входит в Missing %v", inv, res.Missing)
		}
	}

	// Модель не написала инвариант вовсе и промпт не обрезан: виновата она.
	plain := `{"systemPrompt":"Только факты из профиля, ничего не выдумывай. Язык письма — по вакансии. Объём — до 200 слов. Завершай подписью именем."}`
	res2, err := Parse(plain, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res2.Missing) == 0 {
		t.Fatal("преmise: без «пробел — не слабость» инвариант обязан потеряться")
	}
	if len(res2.MissingCut) != 0 {
		t.Errorf("без обрезки MissingCut обязан быть пуст, получено %v", res2.MissingCut)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// TestMetaInstructionTellsBudget — модель не может уложиться в кап, которого
// не знает: лимиты и требование «инварианты первыми строками» обязаны быть
// в мете явно. Регресс на реальный прогон, где хвост с «пробел — не
// слабость» срезался именно потому, что мета про лимит молчала.
func TestMetaInstructionTellsBudget(t *testing.T) {
	sys, _ := Compose("базовая инструкция", "Fullstack-разработчик", "вакансия", nil, testSections)
	for _, want := range []string{
		"Уложись в лимиты итогового systemPrompt",
		"40",
		"5000",
		"первыми строками",
		"не выбрасывай раздел",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("в мета-инструкции нет %q — модель не уложится в лимит вслепую", want)
		}
	}
}
func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
