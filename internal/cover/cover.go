// Package cover собирает user-промпт для LLM: контекст из context/*.md
// (профиль, проекты) + описание вакансии. Системный промпт задаётся
// пользователем отдельно (internal/settings) и сюда не входит.
package cover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildUserPrompt читает все *.md из contextDir (в порядке имён файлов) и
// собирает единый user-промпт: сначала контекст о кандидате, затем вакансия.
// Отсутствующая папка не ошибка — промпт состоит из одной вакансии.
func BuildUserPrompt(contextDir, vacancy string) string {
	var b strings.Builder
	if entries, err := os.ReadDir(contextDir); err == nil {
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			raw, err := os.ReadFile(filepath.Join(contextDir, name))
			if err != nil {
				continue // гонка с пользователем, редактирующим файлы
			}
			b.WriteString("### ")
			b.WriteString(strings.TrimSuffix(name, filepath.Ext(name)))
			b.WriteString("\n\n")
			b.Write(raw)
			b.WriteString("\n\n")
		}
	}
	b.WriteString("### Вакансия\n\n")
	b.WriteString(vacancy)
	return b.String()
}
