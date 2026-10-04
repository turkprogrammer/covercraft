package prompt

import (
	"strings"
	"testing"
)

var testSections = []Section{
	{File: "00-контакты.md", Heading: "## КОНТАКТЫ"},
	{File: "01.md", Heading: "## PHP"},
	{File: "03.md", Heading: "## ML"},
	{File: "02.md", Heading: "## ФАКТЫ-ОГРАНИЧИТЕЛИ ИЗ ДОКОВ (не выдумывать сверх)"},
}

func TestComposeUserCarriesVacancyMustsAndSections(t *testing.T) {
	_, user := Compose("Вакансия PHP", "php-primary", []string{"опыт PHP 5+ лет", "  "}, testSections)
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

// TestComposeSystemForbidsProtectedSections — хард-защита должна быть и в
// инструкции, а не только в Parse: модель не может нарушить правило, которого
// не знает.
func TestComposeSystemForbidsProtectedSections(t *testing.T) {
	system, _ := Compose("Вакансия", "php-primary", nil, testSections)
	for _, want := range []string{"ФАКТЫ-ОГРАНИЧИТЕЛИ", "КОНТАКТЫ", "dropSections", "reason"} {
		if !strings.Contains(system, want) {
			t.Errorf("в инструкции отбора нет %q:\n%s", want, system)
		}
	}
	// Системный промпт письма в инструкцию больше не подмешивается.
	if strings.Contains(system, "до 200 слов") || strings.Contains(system, "ничего не выдумывай") {
		t.Error("инструкция отбора не должна содержать правил промпта письма — их владелец settings.DefaultSystemPrompt")
	}
}

// TestComposeSectionExcerptInUserPart — композер видит выжимку фактов раздела
// рядом с заголовком: между широким «## Go» и узким must-have он судит по
// содержимому, а не по формулировке заголовка. Пустой excerpt строки не
// рендерит.
func TestComposeSectionExcerptInUserPart(t *testing.T) {
	sections := []Section{
		{File: "01.md", Heading: "## Go", Excerpt: "Stable ID 10 000 RPS, Kafka"},
		{File: "02.md", Heading: "## Пустой"},
	}
	_, user := Compose("Go-вакансия, highload", "", []string{"highload"}, sections)
	if !strings.Contains(user, "01.md :: ## Go\n   Excerpt: Stable ID 10 000 RPS") {
		t.Errorf("excerpt не попал в user-часть сразу после заголовка:\n%s", user)
	}
	// Пустой excerpt не рендерит строку Excerpt:.
	if strings.Contains(user, "02.md :: ## Пустой\n   Excerpt:") {
		t.Errorf("пустой excerpt не должен рендерить строку Excerpt:\n%s", user)
	}
}

func TestComposeRoleHintOnlyForKnownRole(t *testing.T) {
	ml, _ := Compose("Вакансия", "ml-research", nil, nil)
	if !strings.Contains(ml, "ML-вакансия") {
		t.Error("для ml-research в инструкции должна быть подсказка по роли")
	}
	unknown, _ := Compose("Вакансия", "самодельная-роль", nil, nil)
	if strings.Contains(unknown, "ML-вакансия") || strings.Contains(unknown, "вакансия Go:") {
		t.Error("неизвестная роль не должна получать подсказки по роли")
	}
}

func TestComposeEmptyContractIsValid(t *testing.T) {
	system, user := Compose("Вакансия X", "other", nil, nil)
	if strings.TrimSpace(system) == "" || !strings.Contains(user, "Вакансия X") {
		t.Error("пустые musts и sections не должны ронять Compose")
	}
}

func TestParseStripsFenceAndPreamble(t *testing.T) {
	raw := "Вот ответ:\n```json\n" +
		`{"dropSections":[{"file":"03.md","heading":"## ML"}],"reason":"PHP — ML не релевантен"}` +
		"\n```"
	res, err := Parse(raw, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Drop) != 1 || res.Drop[0].Heading != "## ML" || res.Drop[0].File != "03.md" {
		t.Errorf("дропы: %+v", res.Drop)
	}
	if res.Reason != "PHP — ML не релевантен" {
		t.Errorf("reason = %q", res.Reason)
	}
}

// TestParseEmptyDropListIsValid — «резать нечего» — валидный ответ, а не
// ошибка: иначе compose падал бы 502 на честном решении модели ничего не
// вырезать.
func TestParseEmptyDropListIsValid(t *testing.T) {
	res, err := Parse(`{"dropSections":[],"reason":"все разделы релевантны"}`, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Drop) != 0 || res.Reason != "все разделы релевантны" {
		t.Errorf("res = %+v", res)
	}
}

func TestParseDropsUnknownHeading(t *testing.T) {
	raw := `{"dropSections":[` +
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

// TestParseNeverDropsProtectedSections — контакты (вариант A: приватный
// context/00-контакты.md) защищены наравне с ФАКТЫ-ОГРАНИЧИТЕЛИ: их вырезание
// лишило бы письмо контактов.
func TestParseNeverDropsProtectedSections(t *testing.T) {
	raw := `{"dropSections":[` +
		`{"file":"02.md","heading":"## ФАКТЫ-ОГРАНИЧИТЕЛИ ИЗ ДОКОВ (не выдумывать сверх)"},` +
		`{"file":"00-контакты.md","heading":"## КОНТАКТЫ"},` +
		`{"file":"03.md","heading":"## ML"}],"reason":"r"}`
	res, err := Parse(raw, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Drop) != 1 || res.Drop[0].Heading != "## ML" {
		t.Errorf("защищённые разделы обязаны остаться, дропы: %+v", res.Drop)
	}
}

// TestParseDropsMismatchedFile — заголовок существует, но имя файла чужое:
// такой дроп не вырежет ничего (cover фильтрует дропы по файлу), поэтому Parse
// обязан его отбросить, а не отчитаться в UI об успехе, которого нет.
func TestParseDropsMismatchedFile(t *testing.T) {
	res, err := Parse(`{"dropSections":[{"file":"03.md","heading":"## ML"},{"file":"99.md","heading":"## ML"}]}`, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Drop) != 1 || res.Drop[0].File != "03.md" {
		t.Errorf("дроп с чужим файлом обязан отброситься, дропы: %+v", res.Drop)
	}
	res2, err := Parse(`{"dropSections":[{"file":"03.MD","heading":"## ML"}]}`, testSections)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res2.Drop) != 1 {
		t.Errorf("регистр имени файла не должен ломать дроп: %+v", res2.Drop)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"без мусора и JSON", "{битый JSON}", "```json\n"} {
		if _, err := Parse(raw, testSections); err == nil {
			t.Errorf("ответ %q → ожидал ошибку", raw)
		}
	}
}
