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
// musts — тексты must-have требований вакансии (извёл fit.ExtractRequirements):
// они идут отдельной секцией-чек-листом, чтобы модель не молча пропускала
// неяркие факты профиля. Пустой musts — секции нет (промпт как раньше).
// Отсутствующая папка не ошибка — промпт состоит из одной вакансии.
func BuildUserPrompt(contextDir, vacancy string, musts []string) string {
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
	if len(musts) > 0 {
		b.WriteString("### Обязательный чек-лист\n\n")
		b.WriteString("Закрой в письме каждое обязательное требование вакансии фактом из контекста выше:\n")
		for _, m := range musts {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			b.WriteString("- " + m + "\n")
		}
		b.WriteString("\nДля каждого пункта назови в письме конкретный проект и факт из контекста " +
			"(название, цифры, технологии) — абстрактное «имею опыт» требование не закрывает. " +
			"Если подходящих фактов несколько, выбери самые сильные и релевантные именно этому требованию.\n")
		b.WriteString("\nЕсли факта для какого-то требования в контексте нет — не выдумывай его, " +
			"а честно признай пробел по шаблону: «С <X> не работал, готов освоить». " +
			"При уместности — добавь мост на ближайший факт, но БЕЗ названия технологий из фактов " +
			"(пиши «есть опыт буферизации и идемпотентности», НЕ «в ClickHouse»).\n\n")
	}
	b.WriteString("### Вакансия\n\n")
	b.WriteString(vacancy)
	return b.String()
}
