// Package audit — постпроверка сгенерированного письма против правил.
//
// Модель стабильно теряет факты, которые требует профиль (Symfony/Yii2 в
// PHP-письмах, highload-метрики, A/B при требовании вакансии), и иногда
// возвращает запрещённые паттерны (фреймворки в стеке, названия ИИ-редакторов,
// «опыт есть, но» в пробелах). Правила в тексте промпта это не лечат —
// проверка здесь детерминирована: violation = конкретное сообщение.
package audit

import (
	"regexp"
	"strings"
)

// Result — итог проверки одного письма.
type Result struct {
	Warnings []string // найденные нарушения
}

// OK — нарушений нет.
func (r Result) OK() bool { return len(r.Warnings) == 0 }

// forbidden — статические запрещённые подстроки (регистронезависимо).
var forbidden = []struct {
	pat   string
	msg   string
	skipV func(vacancy string) bool // nil = всегда проверять
}{
	{"Copilot", "Название ИИ-редактора кода в письме — по правилам только обезличенное «ИИ-инструменты»", nil},
	{"ChatGPT", "Название ИИ-сервиса в письме — обезличить до «ИИ-инструменты»", nil},
	{"Cursor", "Возможное название ИИ-редактора (Cursor) — обезличить", func(v string) bool { return !strings.Contains(strings.ToLower(v), "cursor") }},
	{"основной стек — Go", "Занижающая вставка в Python-пробеле — убрать", nil},
	{"основной язык — go/php", "Оправдание вместо закрытия требования", nil},
	{"поиск ближайших соседей", "k-NN/ANN как опыт — эмбеддингов и векторного поиска в профиле нет", nil},
	{"кэширования эмбеддингов", "Эмбеддинги как опыт — в профиле их нет (кэш = текстовые ключи)", nil},
	{"эмбеддинг", "Упоминание эмбеддингов — проверить: в профиле эмбеддингов нет", nil},
}

// Плейсхолдеры вида [Название вакансии], <роль>, {{...}} — письмо не готово.
var placeholderRe = regexp.MustCompile(`(?i)\[[А-ЯЁA-Z][^\]\n]{3,40}\]|<[А-ЯЁA-Z][^>\n]{3,40}>`)

// «опыт … есть, но» внутри секции пробелов — прячет достижения.
var butRe = regexp.MustCompile(`(?i)опыт[^.\n]{0,60}есть[^.\n]{0,10},\s*но`)

// frameWords — слова фреймворков; рядом должен быть маркер строки стека.
var frameWords = []string{"laravel", "symfony", "yii2", "lumen"}

// stackLineRe — строка стека в любом виде: «Стек: …», «**Стек:** …», «Stack:».
var stackLineRe = regexp.MustCompile(`(?i)^\W*(стек|stack)\s*:?\W*`)

// dupTechs — технологии, которые модель любит дублировать: заявить как опыт
// в буллетах и тут же повторить в «Честно о пробелах».
var dupTechs = []string{"horizon", "octane", "kafka", "clickhouse", "argo"}

// stackForbidden — всё, что запрещено в строке стека (правила промпта v4.1):
// фреймворки, инструменты тестирования/анализа, ADR, non-tech термины.
var stackForbidden = []string{
	"laravel", "symfony", "yii2", "lumen",
	"phpunit", "phpstan", "xdebug", "rector",
	"adr", "rest", "graphql", "grpc",
}

// metricRules — метрики профиля и их проекты-владельцы. Если метрика
// появилась в письме рядом с чужим проектом (и без своего) — предупреждение
// о проверке атрибуции: модель при автоправке склеивает соседние факты
// (был случай: «PHPUnit + TDD (155+ тестов)», где 155+ — тесты Go-проекта).
type metricRule struct {
	metric *regexp.Regexp
	owners []*regexp.Regexp // при ком хотя бы одном метрика легитимна
	fact   string
}

var metricRules = []metricRule{
	{
		metric: mustRE(`(?i)155\+`),
		owners: []*regexp.Regexp{mustRE(`(?i)fraud engine|go-проект|go projects`)},
		fact:   "155+ тестов — Go-проект Fraud Engine; не атрибутируй их PHPUnit/PHP-тестам",
	},
	{
		metric: mustRE(`(?i)92\s?% ?f1`),
		owners: []*regexp.Regexp{mustRE(`(?i)fraud engine`)},
		fact:   "92% F1 — только Fraud Engine",
	},
	{
		metric: mustRE(`(?i)329[, ]?847`),
		owners: []*regexp.Regexp{mustRE(`(?i)domain id`)},
		fact:   "329 847 — только Domain ID",
	},
	{
		metric: mustRE(`(?i)416k\+`),
		owners: []*regexp.Regexp{mustRE(`(?i)domain id`)},
		fact:   "416K+ доменов — только Domain ID",
	},
	{
		metric: mustRE(`(?i)760[–-]850`),
		owners: []*regexp.Regexp{mustRE(`(?i)bundle id`)},
		fact:   "760–850 эл/с — только Bundle ID",
	},
	{
		metric: mustRE(`(?i)(p95[^.\n]{0,12}4\.2|4\.2 ?ms)`),
		owners: []*regexp.Regexp{mustRE(`(?i)fraud engine`)},
		fact:   "P95 < 4.2ms — только Fraud Engine",
	},
	{
		metric: mustRE(`(?i)(10 ?000 rps|10k rps)`),
		owners: []*regexp.Regexp{mustRE(`(?i)stable id`)},
		fact:   "10 000 RPS — только Stable ID",
	},
}

// checkMetricAttribution — линейная проверка: метрика должна стоять в той же
// строке, что и её проект-владелец. Строки писем короткие и одно-темные,
// поэтому построчный анализ точен и не даёт ложных срабатываний на стек-линии.
func checkMetricAttribution(letter string) []string {
	var out []string
	for _, ma := range metricRules {
		if !ma.metric.MatchString(letter) {
			continue
		}
		ok := false
		for _, line := range strings.Split(letter, "\n") {
			if !ma.metric.MatchString(line) {
				continue
			}
			for _, o := range ma.owners {
				if o.MatchString(line) {
					ok = true
					break
				}
			}
			if ok {
				break
			}
		}
		if !ok {
			out = append(out, "Проверь атрибуцию: "+ma.fact+" — метрика стоит не рядом со своим проектом")
		}
	}
	return out
}

// requiredPhrases — факт-обязательства: фраза → условие появления (по вакансии).
type obligation struct {
	need *regexp.Regexp // матчит вакансию → факт обязателен
	has  *regexp.Regexp // должна найтись в письме
	fact string         // человекочитаемое имя факта
}

var obligations = []obligation{
	{
		need: mustRE(`(?i)(a/b|ab[- ]тест|эксперимент|оценк\w* эффект|go/no-go)`),
		has:  mustRE(`(?i)a/?b`),
		fact: "A/B тестирование (Stable ID: traffic split 50/50, метрики P/R/F1) — вакансия требует оценки эффекта",
	},
	{
		need: mustRE(`(?i)php.*(senior|middle)|laravel|symfony|bitrix|битрикс`),
		has:  mustRE(`(?i)symfony`),
		fact: "PHP-primary: Symfony (E-commerce-Lite 7.2, URL-Shortener 8.0) обязан быть в буллетах",
	},
	{
		need: mustRE(`(?i)php.*(senior|middle)|laravel|symfony|bitrix|битрикс`),
		has:  mustRE(`(?i)\byii2\b|yii2[a-z]`), // «yii2» или проект «yii2filmcatalog»
		fact: "PHP-primary: Yii2 (коммерческий production) обязан быть в буллетах",
	},
	{
		need: mustRE(`(?i)highload|высоконагруж|нагрузк|latency|производительност|p95|p99|rps`),
		has:  mustRE(`(?i)fraud engine`),
		fact: "Highload-требование: Fraud Engine (92% F1, P95 < 4.2ms) — доказательная база",
	},
	{
		need: mustRE(`(?i)highload|высоконагруж|нагрузк|latency|производительност|p95|p99|rps`),
		has:  mustRE(`(?i)(92\s?%|4\.2\s?ms|10\s?000|10 000)`),
		fact: "Highload-требование: метрики профиля (92% F1 / P95 < 4.2ms / 10 000 RPS) отсутствуют",
	},
	{
		need: mustRE(`(?i)php.*(senior|middle)|laravel|symfony|тест`),
		has:  mustRE(`(?i)phpunit`),
		fact: "PHP-primary: PHPUnit + TDD обязателен в буллете",
	},
	{
		need: mustRE(`(?i)php.*(senior|middle)|laravel|symfony`),
		has:  mustRE(`(?i)lumen`),
		fact: "Laravel/PHP-вакансия: Lumen обязателен в буллете",
	},
}

func mustRE(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// Check проверяет письмо при известном тексте вакансии.
// vacancy пустой — обязательства по вакансии пропускаются (нельзя судить).
func Check(letter, vacancy string) Result {
	var r Result
	low := strings.ToLower(letter)

	for _, f := range forbidden {
		if f.skipV != nil && f.skipV(vacancy) {
			continue
		}
		if strings.Contains(low, strings.ToLower(f.pat)) {
			r.Warnings = append(r.Warnings, f.msg)
		}
	}

	if m := placeholderRe.FindString(letter); m != "" {
		r.Warnings = append(r.Warnings, "Плейсхолдер «"+m+"» — вакансия не получена или роль не скопирована 1 в 1")
	}

	if gapIdx := gapSectionIndex(letter); gapIdx >= 0 {
		gap := letter[gapIdx:]
		if butRe.MatchString(gap) {
			r.Warnings = append(r.Warnings, "«опыт … есть, но» в секции пробелов —achievement спрятан, переписать без этой технологии")
		}
	}

	// Дубли: технология заявлена и как опыт (буллеты), и как пробел.
	dupSeen := map[string]bool{}
	if gapIdx := gapSectionIndex(letter); gapIdx >= 0 {
		bullets := strings.ToLower(letter[:gapIdx])
		gap := strings.ToLower(letter[gapIdx:])
		for _, tech := range dupTechs {
			re := regexp.MustCompile(`(?i)\b` + tech + `\b`)
			if !re.MatchString(bullets) || !re.MatchString(gap) || dupSeen[tech] {
				continue
			}
			// Пробел закрывает технологию («не работал») — тогда это честный
			// пробел, а не дубль; дублированием считаем утвердительный тон.
			closed := regexp.MustCompile(`(?i)[^\n]{0,60}\b` + tech + `\b[^\n]{0,80}(не работал|не применял|не использовал|не настраивал|опыта нет)`).MatchString(gap)
			if !closed {
				dupSeen[tech] = true
				r.Warnings = append(r.Warnings, "«"+tech+"» одновременно в буллетах и в пробелах — оставь только одно (пробел не должен повторять закрытый факт)")
			}
		}
	}

	// Фреймворки/инструменты в строке стека — полный список запрещённых слов.
	for _, line := range strings.Split(letter, "\n") {
		if !stackLineRe.MatchString(strings.TrimSpace(line)) {
			continue
		}
		ll := strings.ToLower(line)
		for _, fw := range stackForbidden {
			if regexp.MustCompile(`(?i)\b`+fw+`\b`).MatchString(ll) && !dupSeen["stack:"+fw] {
				dupSeen["stack:"+fw] = true
				r.Warnings = append(r.Warnings, "«"+fw+"» в строке стека — перенести в буллеты (в стеке запрещён)")
			}
		}
	}

	// Обязательства по вакансии.
	if strings.TrimSpace(vacancy) != "" {
		for _, o := range obligations {
			if o.need.MatchString(vacancy) && !o.has.MatchString(letter) {
				r.Warnings = append(r.Warnings, "Потерян факт: "+o.fact)
			}
		}
	}

	// Атрибуция метрик профиля (155+ / 92% F1 / 329 847 / …).
	r.Warnings = append(r.Warnings, checkMetricAttribution(letter)...)

	return r
}

// gapSectionIndex — начало секции честных пробелов (несколько формулировок).
func gapSectionIndex(letter string) int {
	for _, h := range []string{"Честно о пробелах", "честно о пробел", "Пробелы:"} {
		if i := strings.Index(strings.ToLower(letter), strings.ToLower(h)); i >= 0 {
			return i
		}
	}
	return -1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
