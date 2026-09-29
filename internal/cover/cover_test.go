package cover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/turkprogrammer/covercraft/internal/prompt"
)

func writeContext(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBuildUserPromptIncludesContextAndVacancy(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-profile.md":  "# Профиль\nGo-разработчик, 5 лет опыта.",
		"02-projects.md": "# Проекты\nvpnctl — менеджер VPN на Go.",
		"notes.txt":      "не .md — игнорируется",
		"README.md":      "# Резюме-заметки\nУмею в GTK.",
	})

	got := BuildUserPrompt(dir, "Вакансия: senior Go developer в банке.", nil, nil)

	for _, want := range []string{
		"Go-разработчик, 5 лет опыта.",
		"vpnctl — менеджер VPN на Go.",
		"Умею в GTK.",
		"Вакансия: senior Go developer в банке.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("промпт не содержит %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "не .md") {
		t.Error("файлы не .md не должны попадать в промпт")
	}
	// Контекст идёт раньше вакансии: модель сперва видит профиль.
	if strings.Index(got, "Go-разработчик") > strings.Index(got, "Вакансия") {
		t.Error("контекст должен идти раньше описания вакансии")
	}
}

func TestBuildUserPromptWithoutContextDir(t *testing.T) {
	got := BuildUserPrompt(filepath.Join(t.TempDir(), "нет-такой-папки"), "Вакансия X.", nil, nil)
	if !strings.Contains(got, "Вакансия X.") {
		t.Errorf("без папки контекста промпт должен содержать вакансию: %q", got)
	}
}

func TestBuildUserPromptIsStable(t *testing.T) {
	// Файлы сортируются по имени — промпт детерминирован.
	dir := writeContext(t, map[string]string{
		"b.md": "B-контент",
		"a.md": "A-контент",
	})
	got1 := BuildUserPrompt(dir, "V", nil, nil)
	got2 := BuildUserPrompt(dir, "V", nil, nil)
	if got1 != got2 {
		t.Error("повторный вызов должен давать тот же промпт")
	}
	if strings.Index(got1, "A-контент") > strings.Index(got1, "B-контент") {
		t.Error("файлы должны идти в порядке имён (a.md раньше b.md)")
	}
}

// TestBuildUserPromptChecklist — непустой список must-have идёт отдельной
// секцией-чек-листом перед вакансией; пустой — секции нет вовсе.
func TestBuildUserPromptChecklist(t *testing.T) {
	dir := t.TempDir()

	withChecklist := BuildUserPrompt(dir, "V", []string{"Go 3+ лет", "PostgreSQL", "  "}, nil)
	if !strings.Contains(withChecklist, "### Обязательный чек-лист") {
		t.Errorf("с непустым musts секции чек-листа нет:\n%s", withChecklist)
	}
	for _, want := range []string{"- Go 3+ лет\n", "- PostgreSQL\n"} {
		if !strings.Contains(withChecklist, want) {
			t.Errorf("чек-лист не содержит %q:\n%s", want, withChecklist)
		}
	}
	if strings.Contains(withChecklist, "- \n") || strings.Contains(withChecklist, "- \n\n") {
		t.Errorf("пустые элементы не должны попадать в чек-лист:\n%s", withChecklist)
	}
	// Чек-лист между контекстом и вакансией, с запретом на выдумки.
	iHead := strings.Index(withChecklist, "### Обязательный чек-лист")
	iVac := strings.Index(withChecklist, "### Вакансия")
	if !(iHead >= 0 && iVac > iHead) {
		t.Errorf("чек-лист должен идти перед вакансией:\n%s", withChecklist)
	}
	if !strings.Contains(withChecklist, "не выдумывай") {
		t.Error("чек-лист должен запрещать выдумывать факты")
	}
	// Требование конкретики: «имею опыт» не считается закрытием.
	if !strings.Contains(withChecklist, "конкретный проект и факт") {
		t.Errorf("чек-лист должен требовать конкретный проект/факт, а не абстракцию:\n%s", withChecklist)
	}

	without := BuildUserPrompt(dir, "V", nil, nil)
	if strings.Contains(without, "чек-лист") {
		t.Errorf("пустой musts — секции быть не должно:\n%s", without)
	}
}

func TestDropSectionsRemovesExactBlock(t *testing.T) {
	raw := "### преамбула\n\n## Go-проекты\n\nпо факту\n\n### вложенный\n\n## PHP\n\nphp-факт\n"
	got := DropSections(raw, []prompt.Drop{{Heading: "## Go-проекты"}})
	for _, want := range []string{"### преамбула", "## PHP", "php-факт"} {
		if !strings.Contains(got, want) {
			t.Errorf("после вырезания нет %q в:\n%s", want, got)
		}
	}
	if strings.Contains(got, "по факту") || strings.Contains(got, "### вложенный") {
		t.Error("вложенный ### обязан уйти вместе с родительским ##")
	}
}

func TestDropSectionsKeepsNeighbourHeading(t *testing.T) {
	raw := "## A\n\na\n\n## B\n\nb\n"
	got := DropSections(raw, []prompt.Drop{{Heading: "## A"}})
	if !strings.Contains(got, "## B") || !strings.Contains(got, "\nb\n") {
		t.Errorf("соседний ##B не должен съедаться:\n%s", got)
	}
}

func TestDropSectionsNotFoundIsByteIdentical(t *testing.T) {
	raw := "## ФАКТЫ-ОГРАНИЧИТЕЛИ (не выдумывать сверх)\n\n- не выдумывай\n"
	if got := DropSections(raw, []prompt.Drop{{Heading: "## Нет такого"}}); got != raw {
		t.Errorf("ненайденный заголовок → побайтово вход:\n%q\n%q", raw, got)
	}
	if got := DropSections(raw, nil); got != raw {
		t.Error("пустые drops → побайтово вход")
	}
}

func TestDropSectionsMultipleBlocksOneFile(t *testing.T) {
	raw := "## A\n\na\n\n## B\n\nb\n\n## C\n\nc\n"
	got := DropSections(raw, []prompt.Drop{{Heading: "## A"}, {Heading: "## C"}})
	if strings.Contains(got, "a\n") || strings.Contains(got, "c\n") {
		t.Errorf("оба блока должны уйти:\n%s", got)
	}
	if !strings.Contains(got, "## B") || !strings.Contains(got, "b\n") {
		t.Errorf("##B обязан остаться:\n%s", got)
	}
}

func TestProfileSectionsListsHeadingsQualified(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"02-b.md":   "## B1\nтекст\n### B1.1\n\n## B2\n",
		"01-a.md":   "## A1\nтекст\n",
		"notes.txt": "## Не считается\n",
	})
	got := ProfileSections(dir)
	want := []string{
		"01-a.md :: ## A1",
		"02-b.md :: ## B1",
		"02-b.md :: ## B2",
	}
	if len(got) != len(want) {
		t.Fatalf("секций = %d, хочу %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("секция[%d] = %q, хочу %q", i, got[i], want[i])
		}
	}
	// Уровень ## отбирается, ### внутри секции — нет (это не ##-заголовок).
	for _, s := range got {
		if strings.Contains(s, "B1.1") {
			t.Errorf("### не должен попадать в список секций: %q", s)
		}
	}
}

// TestBuildUserPromptDropsScopedToFile — дропы последнего ##-блока файла не
// задевают синтетический разделитель «### имя» и содержимое соседнего файла.
func TestBuildUserPromptDropsScopedToFile(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-a.md": "# A\n\n## Хвостовый\n\nхвост-а\n",
		"02-b.md": "# B\n\n## Нужно\n\nб-контент\n",
	})
	got := BuildUserPrompt(dir, "V", nil, []prompt.Drop{{File: "01-a.md", Heading: "## Хвостовый"}})
	if strings.Contains(got, "хвост-а") {
		t.Errorf("хвост 01-a.md должен вырезаться:\n%s", got)
	}
	for _, want := range []string{"### 01-a", "### 02-b", "## Нужно", "б-контент", "### Вакансия"} {
		if !strings.Contains(got, want) {
			t.Errorf("после дропа нет %q:\n%s", want, got)
		}
	}
	// Дроп на файл, которого нет в списке, ничего не делает.
	noOp := BuildUserPrompt(dir, "V", nil, []prompt.Drop{{File: "03-нет.md", Heading: "## Нужно"}})
	if !strings.Contains(noOp, "б-контент") {
		t.Errorf("дроп чужого файла не должен трогать чужой контент:\n%s", noOp)
	}
}
