// Package cover собирает user-промпт для LLM: контекст из context/*.md
// (профиль, проекты) + описание вакансии. Системный промпт задаётся
// пользователем отдельно (internal/settings) и сюда не входит.
package cover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/turkprogrammer/covercraft/internal/prompt"
)

// BuildUserPrompt читает все *.md из contextDir (в порядке имён файлов) и
// собирает единый user-промпт: сначала контекст о кандидате, затем вакансия.
// musts — тексты must-have требований вакансии (извёл fit.ExtractRequirements):
// они идут отдельной секцией-чек-листом, чтобы модель не молча пропускала
// неяркие факты профиля. Пустой musts — секции нет (промпт как раньше).
// drops — разделы, вырезаемые по решению композера: применяются к каждому
// файлу отдельно до записи в буфер, поэтому синтетический разделитель
// «### имя-файла» и преамбулы соседних файлов недостижимы для вырезания.
// Отсутствующая папка не ошибка — промпт состоит из одной вакансии.
func BuildUserPrompt(contextDir, vacancy string, musts []string, drops []prompt.Drop) string {
	var b strings.Builder
	for _, name := range mdNames(contextDir) {
		raw, err := os.ReadFile(filepath.Join(contextDir, name))
		if err != nil {
			continue // гонка с пользователем, редактирующим файлы
		}
		b.WriteString("### ")
		b.WriteString(strings.TrimSuffix(name, filepath.Ext(name)))
		b.WriteString("\n\n")
		b.WriteString(DropSections(string(raw), dropsFor(name, drops)))
		b.WriteString("\n\n")
	}
	if len(musts) > 0 {
		b.WriteString("### Обязательный чек-лист\n\n")
		b.WriteString("Закрой в письме каждое обязательное требование вакансии фактом из контекста выше — отдельным пунктом. Это приоритет письма: если не хватает объёма, сокращай второстепенные факты, а не пункты чек-листа.\n")
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
			"а честно признай пробел и сразу покажи, как твой реальный опыт решает эту задачу " +
			"(«X не применял — есть Y, готов освоить»; Y — ближайший факт из контекста). " +
			"Не извиняйся и не излагай пробел как слабость. " +
			"При уместности — добавь мост на ближайший факт, но БЕЗ названия технологий из фактов " +
			"(пиши «есть опыт буферизации и идемпотентности», НЕ «в ClickHouse»).\n\n")
	}
	b.WriteString("### Вакансия\n\n")
	b.WriteString(vacancy)
	return b.String()
}

// dropsFor — только дропы, относящиеся к данному файлу (регистр имён не
// важен: модель могла вернуть имя в другом регистре расширения).
func dropsFor(name string, drops []prompt.Drop) []prompt.Drop {
	var out []prompt.Drop
	for _, d := range drops {
		if strings.EqualFold(d.File, name) {
			out = append(out, d)
		}
	}
	return out
}

// ProfileSections — квалифицированные заголовки всех *.md из contextDir:
// "file.md :: ## Заголовок". Композер видит только эти строки вместо 67 КБ
// профиля. Сортировка по именам файлов — как в BuildUserPrompt.
func ProfileSections(contextDir string) []string {
	var out []string
	for _, name := range mdNames(contextDir) {
		raw, err := os.ReadFile(filepath.Join(contextDir, name))
		if err != nil {
			continue // гонка с пользователем, редактирующим файлы
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "## ") {
				out = append(out, name+" :: "+strings.TrimSpace(line))
			}
		}
	}
	return out
}

// DropSections вырезает из raw блоки по заголовкам drops, не трогая остальное.
// Семантика: сравнение нормализовано (регистр, повторные пробелы, \r); конец
// блока — следующий заголовок того же или меньшего уровня (вложенные ###
// уходят с родителем, соседний ## не съедается); заголовок не найден → вход
// побайтово без изменений; преамбула до первого заголовка неприкосновенна.
func DropSections(raw string, drops []prompt.Drop) string {
	if len(drops) == 0 {
		return raw
	}
	want := make(map[string]bool, len(drops))
	for _, d := range drops {
		if h := normalizeHeading(d.Heading); h != "" {
			want[h] = true
		}
	}
	if len(want) == 0 {
		return raw
	}

	lines := strings.Split(raw, "\n")
	var out strings.Builder
	skipping := false
	skipLevel := 0
	dropped := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		level := headingLevel(trimmed)
		if level > 0 {
			if skipping && level <= skipLevel {
				skipping = false // блок закончился — соседний заголовок остаётся
			}
			if !skipping && want[normalizeHeading(trimmed)] {
				skipping = true
				skipLevel = level
				dropped = true
				continue
			}
		}
		if skipping {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}

	if !dropped {
		return raw // ни один заголовок не найден — побайтово вход
	}
	return out.String()
}

// headingLevel — длина решётки заголовка (0, если это не заголовок).
func headingLevel(trimmed string) int {
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	if n == 0 || n >= len(trimmed) || trimmed[n] != ' ' {
		return 0
	}
	return n
}

// normalizeHeading — та же нормализация, что в prompt: регистр/пробелы/\r.
func normalizeHeading(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}

// mdNames — отсортированные *.md-файлы каталога (общий путь и для сборки,
// и для списка секций).
func mdNames(contextDir string) []string {
	entries, err := os.ReadDir(contextDir)
	if err != nil {
		return nil
	}
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
	return names
}
