// Package prompt собирает запрос композера: по тексту вакансии и заголовкам
// профиля (с краткой выжимкой фактов) выбирает разделы профиля, которые в неё
// не попадают; их вырезает internal/cover.DropSections.
//
// Системный промпт композер НЕ пишет: промпт — рукописный актив пользователя
// (settings.DefaultSystemPrompt). Переписывание его моделью давало регресс —
// правила терялись при обрезке по лимиту (5000 байт / 40 строк), а проверка
// инвариантов ловила потерю лишь у пяти общих маркеров. Здесь осталась
// единственная часть, которую модель делает лучше кода: отбор разделов.
//
// Пакет чистый, без I/O: вход аргументами, выход возвращаемыми значениями
// (стиль internal/cover).
package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Section — квалифицированный заголовок профиля, предъявляемый композеру.
type Section struct {
	File    string // например "01-профиль-карта-фактов.md"
	Heading string // например "## 3. ML / AI — ИНТЕГРАЦИЯ"
	// Excerpt — краткая выжимка фактов раздела (первые непустые строки текста
	// без строк-заголовков). Даёт композеру судить о содержимом по факту, а не
	// по широкому заголовку. Защищённые разделы (ФАКТЫ-ОГРАНИЧИТЕЛИ, КОНТАКТЫ)
	// excerpt не получают — см. IsProtected.
	Excerpt string
}

// Drop — раздел профиля, который модель решила вырезать из user-промпта.
type Drop struct {
	File    string `json:"file"`
	Heading string `json:"heading"`
}

// Result — разобранный ответ композера: только отбор разделов.
type Result struct {
	Drop   []Drop // какие разделы профиля вырезать из user-промпта
	Reason string // одна строка: почему вырезаны
}

// protectedHeadings — подстроки заголовков, которые вырезать нельзя ни при
// каких условиях: антигаллюцинационный «ФАКТЫ-ОГРАНИЧИТЕЛИ», на который
// опирается проверка письма, и «КОНТАКТЫ» — строка контактов живёт в приватном
// context/00-контакты.md, и её вырезание лишило бы письмо контактов.
// Инструкция защищает от случайности, но не от выбора самой модели, поэтому
// фильтр дублируется в Parse.
var protectedHeadings = []string{"ФАКТЫ-ОГРАНИЧИТЕЛИ", "КОНТАКТЫ"}

// roleHints — зачем композеру роль: подсказка, какие области профиля способны
// подтвердить требования вакансии. Роль приходит из fit.ExtractRequirements
// (закрытый список go-primary|php-primary|ml-research|fullstack|other);
// неизвестная роль подсказки не получает — паники нет.
var roleHints = map[string]string{
	"go-primary":  "вакансия Go: Go-сервисы, метрики нагрузки и конкурентность нужны; PHP-экосистема и legacy-миграции обычно нет",
	"php-primary": "вакансия PHP: PHP/Symfony/Yii2/Laravel и БД нужны; Go-сервисы и ML-интеграции обычно нет",
	"ml-research": "ML-вакансия: ML-проекты, LLM-интеграция и метрики качества моделей нужны; детали PHP-фреймворков обычно нет",
	"fullstack":   "fullstack-вакансия: нужны и бэкенд (Go/PHP), и фронтенд-смежные разделы; резать только заведомо нерелевантное",
}

// Compose собирает system- и user-части вызова композера.
//
// system = инструкция отбора: вернуть JSON с dropSections и reason.
// user = текст вакансии + must-have + список квалифицированных заголовков
// профиля (File + Heading) с краткой выжимкой фактов (Excerpt), а не весь
// профиль: заголовок может быть широким («## Go и highload»), а must-have
// узким («ISO-8583») — excerpt даёт модели судить о содержимом по факту.
// Системный промпт письма сюда не попадает — он больше не переписывается
// моделью.
func Compose(vacancy, role string, musts []string, sections []Section) (system, user string) {
	system = dropInstruction(role)

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

	u.WriteString("\n## Разделы профиля (заголовки + выжимка фактов)\n\n")
	for _, s := range sections {
		u.WriteString(s.File)
		u.WriteString(" :: ")
		u.WriteString(s.Heading)
		u.WriteString("\n")
		if s.Excerpt != "" {
			u.WriteString("   Excerpt: ")
			u.WriteString(s.Excerpt)
			u.WriteString("\n")
		}
	}

	return system, u.String()
}

// dropInstruction — инструкция отбора: что резать нельзя и как отвечать.
func dropInstruction(role string) string {
	var b strings.Builder
	b.WriteString("Ты отбираешь разделы профиля кандидата, нерелевантные конкретной вакансии.\n")
	b.WriteString("Профиль уходит в промпт целиком и создаёт шум, поэтому лишние разделы вырезаются.\n\n")
	b.WriteString("Правила отбора:\n")
	b.WriteString("- режь только то, что всерьёз нерелевантно стеку и роли вакансии: «для краткости письма» — не основание;\n")
	fmt.Fprintf(&b, "- НИКОГДА не вырезай разделы, содержащие %s: на них опирается письмо;\n", quoteList(protectedHeadings))
	b.WriteString("- не вырезай раздел, способный подтвердить must-have или сильные стороны по роли;\n")
	b.WriteString("- сомневаешься — не режь: лишний раздел стоит токенов, вырезанный факт стоит качества письма.\n")
	if h, ok := roleHints[role]; ok {
		b.WriteString("\nОриентир по роли: ")
		b.WriteString(h)
		b.WriteString(".\n")
	}
	b.WriteString("\nВерни ТОЛЬКО JSON {\"dropSections\":[{\"file\":\"…\",\"heading\":\"## …\"}],\"reason\":\"…\"} без markdown-обёртки и пояснений.\n")
	b.WriteString("- dropSections: только пары (file, heading) из предъявленного списка, дословно; пустой список — если резать нечего;\n")
	b.WriteString("- строки «File :: Heading» — элементы списка для dropSections; строки «Excerpt:» — только контекст для суждения о содержимом раздела, в ответ их дословно копировать не нужно;\n")
	b.WriteString("- reason: одна строка, почему разделы вырезаны.")
	return b.String()
}

// quoteList — «a», «b» — чтобы защищённые заголовки читались в инструкции как
// перечисление, а не как одно длинное слово.
func quoteList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, s := range items {
		quoted = append(quoted, "«"+s+"»")
	}
	return strings.Join(quoted, ", ")
}

// Parse разбирает ответ композера. Терпим к markdown-обёртке и мусору вокруг
// JSON (приём fit.ParseExtraction). Отбрасывает дроп с заголовком, которого нет
// в предъявленном списке секций, любой дроп с защищённой подстрокой в заголовке
// (хард-защита антигаллюцинации и контактов) и дроп, у которого заголовок есть,
// а файл — чужой: cover вырезает по паре (file, heading), поэтому «принятый»
// дроп без своего файла был бы ложью в UI.
func Parse(raw string, sections []Section) (Result, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return Result{}, errors.New("в ответе композера нет JSON")
	}

	var payload struct {
		DropSections []Drop `json:"dropSections"`
		Reason       string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &payload); err != nil {
		return Result{}, errors.New("битый JSON композера")
	}

	res := Result{Reason: strings.TrimSpace(payload.Reason)}
	for _, d := range payload.DropSections {
		if dropAllowed(d, sections) {
			res.Drop = append(res.Drop, d)
		}
	}
	return res, nil
}

// dropAllowed — дроп валиден, если пара (файл, заголовок) реально существует
// в предъявленном списке и заголовок не защищён. Файл сверяется наравне с
// заголовком: cover ищет дропы по имени файла (cover.dropsFor), поэтому
// заголовок с чужим файлом не вырежет ничего. Регистр имени файла не важен
// (EqualFold): модель вправе вернуть другое его написание.
func dropAllowed(d Drop, sections []Section) bool {
	if IsProtected(d.Heading) {
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

// IsProtected — заголовок содержит защищённую подстроку (регистр не важен):
// «ФАКТЫ-ОГРАНИЧИТЕЛИ» или «КОНТАКТЫ». Такие разделы нельзя вырезать ни при
// каких условиях (хард-защита в Parse), поэтому и excerpt для них не нарезается
// — отбора по ним нет, а контакты не должны утекать в вызов композера.
func IsProtected(heading string) bool {
	up := strings.ToUpper(normalize(heading))
	for _, p := range protectedHeadings {
		if strings.Contains(up, strings.ToUpper(p)) {
			return true
		}
	}
	return false
}

// normalize приводит заголовок к сравнимому виду: регистр не важен,
// повторные пробелы схлопываются, \r и внешние пробелы отбрасываются.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}
