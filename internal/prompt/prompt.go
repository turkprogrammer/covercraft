// Package prompt собирает системный промпт под вакансию и выбирает разделы
// профиля, которые в неё не попадают. Чистая обёртка без I/O: всё входное —
// аргументами, выходное — возвращаемыми значениями (стиль internal/cover).
package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Section — квалифицированный заголовок профиля, предъявляемый композеру.
type Section struct {
	File    string // например "01-профиль-карта-фактов.md"
	Heading string // например "## 3. ML / AI — ИНТЕГРАЦИЯ"
}

// Drop — раздел профиля, который модель решила вырезать из user-промпта.
type Drop struct {
	File    string `json:"file"`
	Heading string `json:"heading"`
}

// Result — разобранный ответ композера.
type Result struct {
	SystemPrompt string // собранный системный промпт (playbook) под вакансию
	Drop         []Drop // какие разделы профиля вырезать из user-промпта
	Reason       string // одна строка: почему вырезаны
	Truncated    bool   // системный промпт упёрся в лимит и обрезан
	// Missing — инварианты безопасности, которых не нашлось в SystemPrompt.
	// Считается по ИТОГОВОМУ (обрезанному) промпту — тому, что реально уходит
	// в письмо: потеря инварианта в финальном тексте бьёт по качеству,
	// даже если модель его изначально написала.
	// (Мягкая проверка, см. MissingInvariants: playbook заменяет дефолт
	// целиком, поэтому потерю «не выдумывай» нужно хотя бы подсветить.)
	Missing []string
	// MissingCut — подмножество Missing, которые ПРИСУТСТВОВАЛИ в исходном
	// ответе модели, но срезаны clamp'ом: причина потери — лимит, а не
	// модель. Позволяет UI различать «срезано лимитом» и «модель не
	// сохранила» — атрибуция, а не успокоение: финальный промпт в любом
	// случае без этих инвариантов.
	MissingCut []string
}

// Лимиты против избыточности: playbook не должен разрастись больше дефолта
// на порядок. Обрезка — по границе строки, без обрыва слова посередине.
//
// maxPromptChars измеряется в байтах (len), а не в символах: для кириллицы
// это ~2500 символов. Лимиты подняты по факту реального прогона (2026-09-28):
// при 25 строках / 2500 байт большой playbook под вакансию упирался в кап и
// хвост (инварианты «пробел — не слабость») срезался. Значения не в настройках
// намеренно: internal/prompt остаётся чистым и I/O-free.
const (
	maxPromptLines = 40
	maxPromptChars = 5000
)

// protectedHeading — подстрока заголовка, который вырезать нельзя ни при
// каких условиях: это антигаллюцинационный раздел «ФАКТЫ-ОГРАНИЧИТЕЛИ»,
// на который опирается проверка письма. Дроп-лист защищает от случайности,
// но не от выбора самой модели, поэтому фильтр дублируется в Parse.
const protectedHeading = "ФАКТЫ-ОГРАНИЧИТЕЛИ"

// roleDirectives — доменные директивы для закрытого списка ролей из
// fit.ExtractPrompt (go-primary|php-primary|ml-research|fullstack|other).
// Неизвестная роль директиву не получает — паники нет.
var roleDirectives = map[string]string{
	"go-primary":  "Для Go-вакансии подчеркни production-Go: антифрод, Stable ID/Bundle ID/Domain ID сервисы, RPS и нагрузку.",
	"php-primary": "Для PHP-вакансии подчеркни 17 лет PHP production: Symfony, legacy-миграции, производственную нагрузку и стабильность.",
	"ml-research": "Для ML-исследовательской вакансии подчеркни ML-опыт в production: LLM-интеграцию, маппинг/классификацию, качество и метрики моделей без Python-стека.",
	"fullstack":   "Для fullstack-вакансии покажи и бэкенд (Go/PHP), и фронтенд-смежку, делая упор на сквозное владение продуктом.",
}

// invariant — инвариант безопасности базового промпта: имя для UI и группы
// альтернативных формулировок, потому что модель вправе перефразировать.
type invariant struct {
	Name string
	Alts []string
}

// safetyInvariants — что обязано остаться в готовом промпте. Проверка мягкая:
// отсутствие маркера не блокирует промпт (иначе перефразирование роняло бы
// фичу), а подсвечивается в UI — playbook заменяет дефолт целиком (D2), и
// потеря «не выдумывай» бьёт по качеству письма.
var safetyInvariants = []invariant{
	{Name: "только факты профиля", Alts: []string{"не выдумывай", "не придумывай", "не додумывай", "только факты", "анти-фабрикация", "факты из профиля"}},
	{Name: "язык письма", Alts: []string{"язык"}},
	{Name: "объём до 200 слов", Alts: []string{"200", "слов", "объём", "объем"}},
	{Name: "подпись именем", Alts: []string{"подпис"}},
	// Маркер намеренно узкий: голое слово «пробел» встречается почти в любом
	// playbook-е, и проверка стала бы мёртвой.
	{Name: "пробел — не слабость", Alts: []string{"не слабость", "как мост"}},
}

// MissingInvariants возвращает имена инвариантов безопасности, не найденных в
// готовом промпте (сравнение нормализовано: регистр, пробелы). Пустой список —
// все на месте. Решение «предупредить или отклонить» — за вызывающим: Parse
// только сообщает, ничего не блокирует.
func MissingInvariants(systemPrompt string) []string {
	low := strings.ToLower(normalize(systemPrompt))
	var missing []string
	for _, inv := range safetyInvariants {
		found := false
		for _, alt := range inv.Alts {
			if strings.Contains(low, strings.ToLower(alt)) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, inv.Name)
		}
	}
	return missing
}

// Compose собирает system- и user-части вызова композера.
//
// system = base (settings.DefaultSystemPrompt) + мета-инструкция: сохранить
// инварианты базового промпта и переадресовать их на вакансию. user = текст
// вакансии + must-have требования + список квалифицированных заголовков
// профиля (File + Heading), а не весь профиль: композеру нужны только
// заголовки, чтобы выбрать, что вырезать.
func Compose(base, vacancy, role string, musts []string, sections []Section) (system, user string) {
	system = base + "\n\n" + metaInstruction(role)

	var u strings.Builder
	u.WriteString("## Вакансия\n\n")
	u.WriteString(vacancy)
	u.WriteString("\n")

	if len(musts) > 0 {
		u.WriteString("\n## Обязательные требования (must-have)\n\n")
		for _, m := range musts {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			u.WriteString("- ")
			u.WriteString(m)
			u.WriteString("\n")
		}
	}

	u.WriteString("\n## Разделы профиля (только заголовки; полный текст не даётся)\n\n")
	for _, s := range sections {
		u.WriteString(s.File)
		u.WriteString(" :: ")
		u.WriteString(s.Heading)
		u.WriteString("\n")
	}

	return system, u.String()
}

// metaInstruction — мета-инструкция композера поверх base: сохранить
// инварианты и заменить их на вакансия-специфичные директивы.
func metaInstruction(role string) string {
	var b strings.Builder
	b.WriteString("Ты превращаешь инструкцию выше в самостоятельный системный промпт (playbook) под конкретную вакансию.\n")
	b.WriteString("Сохрани инварианты исходной инструкции и перечисли их ПЕРВЫМИ СТРОКАМИ итогового промпта, чтобы они не потерялись при усечении:\n")
	b.WriteString("- опирайся только на факты из профиля и описания вакансии, ничего не выдумывай;\n")
	b.WriteString("- Язык письма — по языку вакансии;\n")
	b.WriteString("- объём — до 200 слов, но must-have вакансии приоритетнее объёма: не влезает — сокращай второстепенные факты, а не требования;\n")
	b.WriteString("- структура: приветствие → маркированный список доводов (каждый must-have — отдельный пункт с фактом) → закрытие;\n")
	b.WriteString("- подпись: если в инструкции уже задана дословная подпись — используй её; если имени нет в профиле — без подписи, не выдумывай; выдуманное имя = фабрикация;\n")
	b.WriteString("- не излагай пробел как слабость, а как мост на ближайший факт.\n")
	b.WriteString("\nПереадресуй эти правила на вакансию: язык, объём, структуру, тон, что подчеркнуть из must-have, в каких разделах профиля искать факты.\n")
	if d, ok := roleDirectives[role]; ok {
		b.WriteString(d)
		b.WriteString("\n")
	}
	// Лимиты сообщаются модели явно (см. F7-план): она не может уложиться в
	// кап, которого не знает, а срез хвоста в реальном прогоне терял
	// последний инвариант «пробел — не слабость». Инварианты-первыми — вторая
	// страховка: clamp режет хвост, а не голову.
	fmt.Fprintf(&b,
		"\nУложись в лимиты итогового systemPrompt: не больше %d строк и %d байт (≈%d символов кириллицы). Инварианты безопасности — первыми строками, их срез при усечении недопустим.\n",
		maxPromptLines, maxPromptChars, maxPromptChars/2)
	b.WriteString("\nВерни ТОЛЬКО JSON {\"systemPrompt\":\"…\",\"dropSections\":[{\"file\":\"…\",\"heading\":\"## …\"}],\"reason\":\"…\"} без markdown-обёртки и пояснений.\n")
	b.WriteString("- systemPrompt: текст будущего системного промпта, по-русски;\n")
	b.WriteString("- dropSections: только заголовки из предъявленного списка, кроме разделов, содержащих «")
	b.WriteString(protectedHeading)
	b.WriteString("» — их не выбрасывать; не выбрасывай раздел, способный подтвердить must-have или сильные стороны по роли: «для краткости письма» — не основание для вырезания. Режь только то, что всерьёз нерелевантно стеку и роли вакансии;\n")
	b.WriteString("- reason: одна строка, почему разделы вырезаны.")
	return b.String()
}

// Parse разбирает ответ композера. Терпим к markdown-обёртке и мусору вокруг
// JSON (приём ParseExtraction). Отбрасывает дроп с заголовком, которого нет в
// предъявленном списке секций, любой дроп с «ФАКТЫ-ОГРАНИЧИТЕЛИ» в заголовке
// (хард-защита антигаллюцинации) и дроп, у которого заголовок есть, а файл —
// чужой: cover вырезает по паре (file, heading), поэтому «принятый» дроп без
// своего файла был бы ложью в UI. Пустой systemPrompt — ошибка, чтобы UI не
// затирал поле. Потеря инвариантов безопасности ошибкой не считается: их имена
// уходят в Result.Missing (мягкое предупреждение в UI).
func Parse(raw string, sections []Section) (Result, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return Result{}, errors.New("в ответе композера нет JSON")
	}

	var payload struct {
		SystemPrompt string `json:"systemPrompt"`
		DropSections []Drop `json:"dropSections"`
		Reason       string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &payload); err != nil {
		return Result{}, errors.New("битый JSON композера")
	}

	system, truncated := clampPrompt(payload.SystemPrompt)
	if strings.TrimSpace(system) == "" {
		return Result{}, errors.New("композер вернул пустой systemPrompt")
	}

	res := Result{
		SystemPrompt: system,
		Reason:       strings.TrimSpace(payload.Reason),
		Truncated:    truncated,
		// Missing — по ФИНАЛЬНОМУ (обрезанному) тексту: это то, что реально
		// уходит в письмо. Проверять исходный ответ модели было бы самообманом —
		// потерянный при усечении инвариант в письме отсутствует так же.
		Missing: MissingInvariants(system),
	}
	// MissingCut — атрибуция: инвариант, который был в ответе модели, но срезан
	// clamp'ом, отличается от инварианта, которого модель не написала вовсе.
	// Предупреждение в UI остаётся в обоих случаях (финальный промпт без него
	// в обоих), но называет настоящую причину, а не сваливает всё в «модель».
	if truncated {
		presentBefore := make(map[string]bool)
		for _, inv := range MissingInvariants(payload.SystemPrompt) {
			presentBefore[inv] = true
		}
		for _, inv := range MissingInvariants(system) {
			if !presentBefore[inv] {
				res.MissingCut = append(res.MissingCut, inv)
			}
		}
	}
	for _, d := range payload.DropSections {
		if dropAllowed(d, sections) {
			res.Drop = append(res.Drop, d)
		}
	}
	return res, nil
}

// dropAllowed — дроп валиден, если пара (файл, заголовок) реально существует
// в предъявленном списке и это не защищённый «ФАКТЫ-ОГРАНИЧИТЕЛИ». Файл
// сверяется наравне с заголовком: cover ищет дропы по имени файла
// (cover.dropsFor), поэтому заголовок с чужим файлом не вырежет ничего —
// принять его значило бы отчитаться об успехе, которого нет. Регистр имени
// файла не важен (EqualFold): модель вправе вернуть другое его написание.
func dropAllowed(d Drop, sections []Section) bool {
	if strings.Contains(strings.ToUpper(normalize(d.Heading)), strings.ToUpper(protectedHeading)) {
		return false
	}
	for _, s := range sections {
		if normalize(s.Heading) != normalize(d.Heading) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(s.File), strings.TrimSpace(d.File)) {
			return true
		}
	}
	return false
}

// clampPrompt режет промпт по границе строки в пределах лимитов и сообщает,
// была ли обрезка.
func clampPrompt(s string) (string, bool) {
	trimmed := strings.TrimRight(s, "\n \t")
	if trimmed == "" {
		return "", false
	}
	truncated := false

	// Обрезка по символам — только по границе строки, чтобы не рвать слово.
	if len(trimmed) > maxPromptChars {
		cut := strings.LastIndexByte(trimmed[:maxPromptChars], '\n')
		if cut <= 0 {
			cut = maxPromptChars
			// Границы строки рядом нет — режем по байтам, но не рвём
			// UTF-8-руну: иначе в теле запроса к провайдеру оказался бы
			// невалидный UTF-8.
			for cut > 0 && !utf8.RuneStart(trimmed[cut]) {
				cut--
			}
		}
		trimmed = strings.TrimRight(trimmed[:cut], "\n \t")
		truncated = true
	}

	// Обрезка по строкам.
	if lines := strings.Split(trimmed, "\n"); len(lines) > maxPromptLines {
		trimmed = strings.Join(lines[:maxPromptLines], "\n")
		truncated = true
	}

	return trimmed, truncated
}

// normalize приводит заголовок к сравнимому виду: регистр не важен,
// повторные пробелы схлопываются, \r и внешние пробелы отбрасываются.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}
