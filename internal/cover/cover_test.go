package cover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	got := BuildUserPrompt(dir, "Вакансия: senior Go developer в банке.")

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
	got := BuildUserPrompt(filepath.Join(t.TempDir(), "нет-такой-папки"), "Вакансия X.")
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
	got1 := BuildUserPrompt(dir, "V")
	got2 := BuildUserPrompt(dir, "V")
	if got1 != got2 {
		t.Error("повторный вызов должен давать тот же промпт")
	}
	if strings.Index(got1, "A-контент") > strings.Index(got1, "B-контент") {
		t.Error("файлы должны идти в порядке имён (a.md раньше b.md)")
	}
}
