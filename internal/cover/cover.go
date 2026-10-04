// Package cover собирает user-промпт для LLM: контекст из context/*.md
// (профиль, проекты) + описание вакансии. Системный промпт задаётся
// пользователем отдельно (internal/settings) и сюда не входит.
package cover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

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
//
// Профиль без ограничения объёма даёт только базовую генерацию. Оба режима
// автоправки идут через buildFixUserPrompt — с отбором релевантных секций.
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
		b.WriteString("Закрой в письме каждое обязательное требование вакансии фактом из контекста выше — отдельным пунктом. Это приоритет письма: если не хватает объёма, сокращай второстепенные факты, а не пункты чек-листа.\n\n")
		for _, m := range musts {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			b.WriteString("- " + m + "\n")
		}
		b.WriteString("\nКак закрывать требование и что делать с пробелом — в системной инструкции: строка пробела из требований вакансии, мост на ближайший факт из контекста, без выдуманных технологий и цифр.\n\n")
	}
	b.WriteString("### Вакансия\n\n")
	b.WriteString(vacancy)
	return b.String()
}

// fitFixProfileMaxBytes — потолок объёма профиля в user-промпте автоправки.
// Живой баг (октябрь 2026): fit-fix гнал модели 85.7 КБ context/*.md вместе с
// письмом на КАЖДУЮ итерацию; модель тонет в объёме и отвечает эхом промпта.
// 24 КБ — достаточно для отбора релевантных секций под caveat-список
// (полное письмо ~3 КБ, секции профиля ~1–3 КБ каждая).
const fitFixProfileMaxBytes = 24 << 10

// BuildFitFixUserPrompt — user-промпт режима fit-fix. Отличие от
// BuildUserPrompt: в модель уходит НЕ весь профиль, а только секции,
// релевантные строкам caveat (плюс вакансия и must-have чек-лист).
//
// Почему отбор: на полном профиле (85+ КБ) модель возвращала эхо входа
// вместо правки письма, после чего правки откатывались — «автофикс не может
// исправить даже через несколько попыток».
//
// Фоллбэк: если ни одна секция не совпала с caveat (формулировки вакансии
// бывают совсем не похожи на текст профиля), отдаём полный профиль — лучше
// старый объём, чем промпт, из которого модель выдумает факты.
//
// Секция = блок от ##-заголовка до следующего ##-заголовка того же уровня
// (вложенные ### уходят с родителем); текст до первого ## — преамбула файла,
// она включается только вместе с отобранной секцией (контекст заголовка).
func BuildFitFixUserPrompt(contextDir, vacancy string, musts []string, caveats []string, drops []prompt.Drop) string {
	return buildFixUserPrompt(contextDir, vacancy, musts, caveatKeywords(caveats), drops)
}

// BuildAuditFixUserPrompt — user-промпт режима автоправки по замечаниям
// аудита. Тот же отбор секций, что и в fit-fix: полный профиль модель не
// переваривает. Живой баг (октябрь 2026): audit-fix шёл через BuildUserPrompt
// и отправлял все 85 КБ context/*.md на каждую итерацию — модель тонула и
// отвечала эхом, поэтому «повтори автоправку» не помогало.
//
// Ключевые слова берутся из названий фактов в «ёлочках»: аудит пишет
// «Потерян факт: Symfony 6.4» и «Redis » одновременно в буллетах и в
// пробелах» — имя технологии там единственное, что указывает на нужную
// секцию профиля. Структурные замечания («нет обязательной секции…») имени
// не содержат и ключей не дают: для них отбор пуст и срабатывает фоллбэк
// на полный профиль — так же, как раньше.
func BuildAuditFixUserPrompt(contextDir, vacancy string, musts []string, warnings []string, drops []prompt.Drop) string {
	return buildFixUserPrompt(contextDir, vacancy, musts, auditWarningKeywords(warnings), drops)
}

// buildFixUserPrompt — общий путь обоих режимов автоправки: keywords —
// уже нормализованные ключевые слова отбора секций.
func buildFixUserPrompt(contextDir, vacancy string, musts, keywords []string, drops []prompt.Drop) string {
	profile := collectProfile(contextDir, drops)
	if len(profile) == 0 {
		return BuildUserPrompt(contextDir, vacancy, musts, drops)
	}
	selected, _ := selectSections(profile, keywords)
	if len(selected) == 0 {
		// Отбор пуст — фоллбэк на полный профиль (с прежним потолком дропов).
		return BuildUserPrompt(contextDir, vacancy, musts, drops)
	}
	var b strings.Builder
	b.WriteString("### Профиль (только секции, релевантные автоправке)\n\n")
	for _, sec := range selected {
		b.WriteString(sec.text)
		b.WriteString("\n\n")
	}
	if len(musts) > 0 {
		b.WriteString("### Обязательный чек-лист\n\n")
		for _, m := range musts {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			b.WriteString("- " + m + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("### Вакансия\n\n")
	b.WriteString(vacancy)
	return b.String()
}

// fileSection — одна ##-секция файла вместе с его преамбулой (для контекста).
type fileSection struct {
	preamble string // текст файла до первого ## (пусто, если секция — первая)
	text     string // сама секция, включая вложенные ###
}

// collectProfile — все ##-секции всех *.md с учётом дропов композера.
func collectProfile(contextDir string, drops []prompt.Drop) []fileSection {
	var out []fileSection
	for _, name := range mdNames(contextDir) {
		raw, err := os.ReadFile(filepath.Join(contextDir, name))
		if err != nil {
			continue // гонка с пользователем, редактирующим файлы
		}
		cleaned := DropSections(string(raw), dropsFor(name, drops))
		preamble, sections := splitSections(cleaned)
		for i, sec := range sections {
			fs := fileSection{text: sec}
			if i == 0 {
				fs.preamble = preamble
			}
			out = append(out, fs)
		}
	}
	return out
}

// splitSections — преамбула (до первого ##) и ##-секции файла. Уровень
// секции именно два: вложенные ### и #### входят в текст родителя.
func splitSections(raw string) (preamble string, sections []string) {
	lines := strings.Split(raw, "\n")
	var cur, pre []string
	inSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		level := headingLevel(trimmed)
		if level == 2 {
			if inSection {
				sections = append(sections, strings.Join(cur, "\n"))
				cur = nil
			}
			inSection = true
		}
		if inSection {
			cur = append(cur, line)
		} else {
			pre = append(pre, line)
		}
	}
	if inSection && len(cur) > 0 {
		sections = append(sections, strings.Join(cur, "\n"))
	}
	return strings.TrimRight(strings.Join(pre, "\n"), "\n"), sections
}

// minKeywordRunes — минимальная длина ключевого слова в СИМВОЛАХ (не в
// байтах). Порог считается по рунам намеренно: len() на кириллице даёт
// вдвое больше символов, и слово «стек» (4 буквы, 8 байт) проходило бы
// фильтр как «достаточно длинное», хотя для отбора секции это шум.
// Латинские имена (Symfony, RabbitMQ) от правила не меняются.
const minKeywordRunes = 5

// longEnough — достаточно ли символов в слове для ключа отбора секций.
func longEnough(w string) bool { return utf8.RuneCountInString(w) >= minKeywordRunes }

// caveatKeywords — содержательные слова из строк caveat: нормализация
// (регистр, пунктуация) и отсечение служебных слов фита («впиши», «письмо»,
// «профиль»), которые есть в каждой строке и не отбирают ничего.
func caveatKeywords(caveats []string) []string {
	stop := map[string]bool{
		"впиши": true, "пишет": true, "письмо": true, "письме": true,
		"профиле": true, "профиля": true, "профиль": true, "требование": true,
		"требования": true, "требованиях": true, "вакансии": true, "вакансия": true,
		"закроется": true, "полностью": true, "упомянут": true, "не": true,
		"попал": true, "попала": true, "факт": true, "факты": true, "фактов": true,
		"секции": true, "нужно": true, "есть": true, "опыт": true, "работа": true,
		"разработки": true, "разработчик": true,
	}
	seen := map[string]bool{}
	var words []string
	for _, c := range caveats {
		for _, w := range strings.FieldsFunc(strings.ToLower(c), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if !longEnough(w) || stop[w] || seen[w] {
				continue
			}
			seen[w] = true
			words = append(words, w)
		}
	}
	return words
}

// auditWarningKeywords — ключевые слова отбора для замечаний аудита.
// Берём содержимое «ёлочек»: аудит ставит в кавычки ровно то, о чём
// замечание — «Потерян факт: Symfony 6.4», «Redis » одновременно в буллетах
// и в пробелах», «нет обязательной секции «Адаптация под ваш стек»».
//
// Почему только кавычки, а не все слова строки: служебные слова замечания
// («потерян», «обязательной», «пробелах») встречаются в каждом замечании и
// отбирали бы случайные секции. Кавычки — единственный надёжный признак
// имени факта. Порог 5 символов общий с caveatKeywords: короткие «PHP»,
// «CDN», «REST» отсекаются, «Redis» и «Symfony» проходят.
//
// Замечание без кавычек («потерян факт») ключей не даёт — отбор пуст,
// срабатывает фоллбэк на полный профиль.
func auditWarningKeywords(warnings []string) []string {
	var words []string
	seen := map[string]bool{}
	add := func(s string) {
		for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if !longEnough(w) || seen[w] {
				continue
			}
			seen[w] = true
			words = append(words, w)
		}
	}
	for _, w := range warnings {
		inside := false
		var buf strings.Builder
		for _, r := range w {
			if r == '«' {
				inside, buf = true, strings.Builder{}
				continue
			}
			if r == '»' {
				if inside {
					add(buf.String())
				}
				inside = false
				continue
			}
			if inside {
				buf.WriteRune(r)
			}
		}
		// Незакрытая кавычка (сбой разбора замечания) — берём остаток строки.
		if inside {
			add(buf.String())
		}
	}
	return words
}

// selectSections — отбор секций: каждая содержит ≥1 ключевое слово caveat.
// Порядок файлов сохраняется; бюджет — общий потолок байт.
// Возвращает отобранные секции и остаток бюджета; пустой срез — ничего
// не подошло, вызывающий обязан сделать фоллбэк на полный профиль.
func selectSections(profile []fileSection, keywords []string) ([]fileSection, int) {
	budget := fitFixProfileMaxBytes
	var out []fileSection
	if len(keywords) == 0 {
		return nil, 0 // caveat-строк нет — отбор бессмыслен, будет фоллбэк
	}
	for _, sec := range profile {
		low := strings.ToLower(sec.text)
		hit := false
		for _, w := range keywords {
			if strings.Contains(low, w) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		cost := len(sec.preamble) + len(sec.text)
		if cost > budget {
			continue // не влезает целиком — пропускаем, бюджет оставим меньшим
		}
		budget -= cost
		if sec.preamble != "" {
			sec.text = sec.preamble + "\n\n" + sec.text
		}
		out = append(out, sec)
	}
	return out, budget
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

// AppliedDrops — подмножество drops, реально вырезанных из профиля: файл
// существует и заголовок в нём есть.
//
// Зачем: compose возвращает дропы по снимку секций, а профиль к моменту
// генерации мог измениться (пользователь правил context/*.md). Дроп по
// заголовку, которого больше нет, молча ничего не вырезает — UI обязан
// показать расхождение, а не рапортовать «вырезано N».
//
// Сопоставление то же, что в DropSections: имя файла без учёта регистра
// (dropsFor) + нормализованный заголовок как заголовок (headingLevel).
func AppliedDrops(contextDir string, drops []prompt.Drop) []prompt.Drop {
	if len(drops) == 0 {
		return nil
	}
	var out []prompt.Drop
	for _, name := range mdNames(contextDir) {
		for _, d := range dropsFor(name, drops) {
			h := normalizeHeading(d.Heading)
			if h == "" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(contextDir, name))
			if err != nil {
				continue // гонка с пользователем, редактирующим файлы
			}
			for _, line := range strings.Split(string(raw), "\n") {
				trimmed := strings.TrimSpace(line)
				if headingLevel(trimmed) > 0 && normalizeHeading(trimmed) == h {
					out = append(out, d)
					break
				}
			}
		}
	}
	return out
}

// ProfileSections — квалифицированные разделы всех *.md из contextDir:
// {File, Heading, Excerpt}. Композер видит заголовки и краткую выжимку фактов
// вместо 67 КБ профиля. Excerpt нарезается из фактического текста секции
// (sectionExcerpt); защищённые разделы (prompt.IsProtected) его не получают.
// Сортировка по именам файлов — как в BuildUserPrompt.
func ProfileSections(contextDir string) []prompt.Section {
	var out []prompt.Section
	for _, name := range mdNames(contextDir) {
		raw, err := os.ReadFile(filepath.Join(contextDir, name))
		if err != nil {
			continue // гонка с пользователем, редактирующим файлы
		}
		_, sections := splitSections(string(raw))
		for _, sec := range sections {
			heading := strings.TrimSpace(strings.Split(sec, "\n")[0])
			if !strings.HasPrefix(heading, "## ") {
				continue
			}
			out = append(out, prompt.Section{
				File:    name,
				Heading: heading,
				Excerpt: sectionExcerpt(sec, heading),
			})
		}
	}
	return out
}

// Границы выжимки фактов: первые N непустых строк текста, не длиннее L байт.
const (
	sectionExcerptMaxLines = 3
	sectionExcerptMaxBytes = 240
)

// sectionExcerpt — краткая выжимка фактов секции для композера: первые N
// непустых строк фактического текста (пустые строки и строки-заголовки
// пропускаются — иначе первые строки окажутся одними ### без фактов),
// конкатенированные через "; ", усечённые до L байт по границе руны с
// маркером «…». Защищённые разделы (prompt.IsProtected) выжимки не получают:
// они невырезаемы, отбор по ним не нужен, а контакты не должны утекать в
// дополнительный вызов композера.
func sectionExcerpt(section, heading string) string {
	if prompt.IsProtected(heading) {
		return ""
	}
	var facts []string
	for _, line := range strings.Split(section, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue // пустая строка или заголовок (сам ## и вложенные ###)
		}
		facts = append(facts, t)
		if len(facts) >= sectionExcerptMaxLines {
			break
		}
	}
	if len(facts) == 0 {
		return ""
	}
	return truncateRunes(strings.Join(facts, "; "), sectionExcerptMaxBytes)
}

// truncateRunes усекает s до maxBytes байт, не разрезая руну: остаток режется
// до ближайшей границы UTF-8, при обрезке добавляется маркер «…». Усечение по
// байтам без отката дало бы битую руну (профиль на русском).
func truncateRunes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	for len(cut) > 0 {
		r, size := utf8.DecodeLastRuneInString(cut)
		if r != utf8.RuneError || size > 1 {
			break
		}
		cut = cut[:len(cut)-1]
	}
	return cut + "…"
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
