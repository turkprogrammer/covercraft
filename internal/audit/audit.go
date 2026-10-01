// Package audit — постпроверка сгенерированного письма против правил.
//
// Модель стабильно теряет факты, которые требует профиль (Symfony/Yii2 в
// PHP-письмах, highload-метрики, A/B при требовании вакансии), и иногда
// возвращает запрещённые паттерны (фреймворки в стеке, названия ИИ-редакторов,
// «опыт есть, но» в пробелах). Правила в тексте промпта это не лечат —
// проверка здесь детерминирована: violation = конкретное сообщение.
package audit

import (
	"fmt"
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
	{"блокиров", "Блокировки (Redis) без требования в вакансии — заменить на «Redis (кэш)»", func(v string) bool {
		vl := strings.ToLower(v)
		return strings.Contains(vl, "блокиров") || strings.Contains(vl, "rate limit") ||
			strings.Contains(vl, "rate-limit") || strings.Contains(vl, "локинг") ||
			strings.Contains(vl, "lock")
	}},
}

// Плейсхолдеры вида [Название вакансии], <роль>, {{...}} — письмо не готово.
var placeholderRe = regexp.MustCompile(`(?i)\[[А-ЯЁA-Z][^\]\n]{3,40}\]|<[А-ЯЁA-Z][^>\n]{3,40}>`)

// «опыт … есть, но» внутри секции пробелов — прячет достижения.
var butRe = regexp.MustCompile(`(?i)опыт[^.\n]{0,60}есть[^.\n]{0,10},\s*но`)

// gapButRe — ОБРАТНЫЙ порядок в живом письме: «Пробел: Нет прямого опыта с
// Tempo/OpenTelemetry, но есть навык построения трассировки через Kafka».
// Прежний butRe требовал «опыт … есть …, но» и такой ход пропускал, хотя
// достижение всё равно спрятано в буллете пробела. Ловим «пробел/нет …, но
// есть/навык/опыт» — вторая часть и есть спрятанное достижение.
var gapButRe = regexp.MustCompile("(?i)(пробел|нет|не\\s+(?:имею|работал|использовал))[^.\\n]{0,80},\\s*но\\s+(?:есть|имею|навык|опыт|знаком|владе|умею|работал)")

// stackLineRe — строка стека в любом виде: «Стек: …», «**Стек:** …», «Stack:».
var stackLineRe = regexp.MustCompile(`(?i)^\W*(стек|stack)\s*:?\W*`)

// bareStackRe — «голая» строка стека: перечень технологий через запятую,
// начинающийся с языка («Go, PHP, ML, Kafka, …»). Модель иногда теряет
// префикс «Стек:» — опознаём такую строку по форме, а не по слову.
var bareStackRe = regexp.MustCompile(`(?i)^\W*(?:go|golang|php|python|java|rust|kotlin|typescript|javascript|ml|sql|bash)\s*[,;]`)

// dupTechs — технологии, которые модель любит дублировать: заявить как опыт
// в буллетах и тут же повторить в «Честно о пробелах».
var dupTechs = []string{"horizon", "octane", "kafka", "clickhouse", "argo"}

// foreignGapTerms — термины, законно живущие в секции пробелов. Их нужно
// вырезать перед проверкой дублей: без этого «Debezium» содержит подстроку
// «clickhouse» (De-bez-ClickHouse-ium) и даёт ложное срабатывание детектора.
var foreignGapTerms = []string{
	"debezium", "elasticsearch", "opensearch", "prometheus",
	"grafana", "airflow", "rabbitmq",
}

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
		owners: []*regexp.Regexp{mustRE(`(?i)fraud( detection)? engine|go-проект|go projects`)},
		fact:   "155+ тестов — Go-проект Fraud Engine; не атрибутируй их PHPUnit/PHP-тестам",
	},
	{
		metric: mustRE(`(?i)92\s?% ?f1`),
		owners: []*regexp.Regexp{mustRE(`(?i)fraud( detection)? engine`)},
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
		owners: []*regexp.Regexp{mustRE(`(?i)fraud( detection)? engine`)},
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
		has:  mustRE(`(?i)fraud( detection)? engine`),
		fact: "Highload-требование: Fraud Engine (92% F1, P95 < 4.2ms) — доказательная база",
	},
	{
		need: mustRE(`(?i)highload|высоконагруж|нагрузк|latency|производительност|p95|p99|rps`),
		has:  mustRE(`(?i)(92\s?%|4\.2\s?ms|10\s?000|10 000)`),
		fact: "Highload-требование: метрики профиля (92% F1 / P95 < 4.2ms / 10 000 RPS) отсутствуют",
	},
	{
		// PHPUnit + TDD обязателен ТОЛЬКО для PHP-primary. Слово «тест» в
		// Go-вакансии не запускает это правило: для Go-primary PHPUnit в
		// письме запрещён (правило промпта), тесты закрываются «155+ тестов».
		need: mustRE(`(?i)(php.*(senior|middle)|laravel|symfony|bitrix|битрикс)`),
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
		if butRe.MatchString(gap) || gapButRe.MatchString(gap) {
			r.Warnings = append(r.Warnings, "«опыт … есть, но» в секции пробелов —achievement спрятан, переписать без этой технологии")
		}
	}

	// Дубли: технология заявлена и как опыт (буллеты), и как пробел.
	dupSeen := map[string]bool{}
	if gapIdx := gapSectionIndex(letter); gapIdx >= 0 {
		bullets := strings.ToLower(letter[:gapIdx])
		// Секция пробелов заканчивается строкой стека/контактами — в стек
		// «ClickHouse» и «Kafka» входят законно и не должны считаться
		// упоминанием в пробелах. Границу ищем построчно: модель иногда
		// теряет префикс «Стек:», поэтому строку стека опознаём и по
		// форме перечня (isStackLine), а не только по слову «Стек».
		var gapLines []string
		for _, line := range strings.Split(letter[gapIdx:], "\n") {
			lt := strings.ToLower(strings.TrimSpace(line))
			if isStackLine(line) || strings.HasPrefix(lt, "+7") ||
				strings.HasPrefix(lt, "telegram:") || strings.HasPrefix(lt, "github") {
				break
			}
			gapLines = append(gapLines, lt)
		}
		gap := strings.Join(gapLines, "\n")
		// Вырезаем имена чужих технологий из секции пробелов: «Debezium»
		// содержит подстроку «clickhouse» и ловился бы как ложный дубль.
		for _, term := range foreignGapTerms {
			gap = strings.ReplaceAll(gap, term, " ")
		}
		gapBlocks := gapBlocksOf(gap)
		for _, tech := range dupTechs {
			re := regexp.MustCompile(`(?i)\b` + tech + `\b`)
			if !re.MatchString(bullets) || !re.MatchString(gap) || dupSeen[tech] {
				continue
			}
			// Пробел закрывает технологию («не работал») — тогда это честный
			// пробел или якорь моста, а не дубль; дублированием считаем
			// утвердительный тон. Отрицание ищем в границах всего буллета,
			// а не только в хвосте после технологии: живое письмо называло
			// её якорем моста («Transactional outbox: не использовал; мост —
			// буферизация…, досылка при недоступности Kafka»), маркер стоит
			// ДО технологии — хвостовой поиск давал ложный warning, а auto-fix
			// по нему не мог ничего исправить и жёг генерации.
			if !gapBlockNegated(gapBlocks, tech) {
				dupSeen[tech] = true
				r.Warnings = append(r.Warnings, "«"+tech+"» одновременно в буллетах и в пробелах — оставь только одно (пробел не должен повторять закрытый факт)")
			}
		}
	}

	// Фреймворки/инструменты в строке стека — полный список запрещённых слов.
	for _, line := range strings.Split(letter, "\n") {
		if !isStackLine(line) {
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

	// --- Инварианты v4, проверяемые детерминированно ---
	// Секция адаптации и финальная строка обязательны всегда (v4 §2.4, §2.7).
	if !strings.Contains(low, "адаптация под ваш стек") {
		r.Warnings = append(r.Warnings, "нет обязательной секции «Адаптация под ваш стек» — v4 §2.4")
	}
	// Объём письма: лимит 200 слов из промпта («сокращай второстепенные факты,
	// а не требования»). Проверки не было вовсе, и живое письмо пришло на
	// 307 слов без последствий. Контакты и строка подписи не считаются телом.
	if n := bodyWordCount(letter); n > letterWordLimit {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"письмо на %d слов при лимите %d — сократи второстепенные факты, а не требования (v4 §2.3)",
			n, letterWordLimit))
	}
	// Оправдательный аргумент вместо факта: «стажа достаточно, экспертиза
	// компенсирует» — это не аргумент кандидата, а уговор работодателя.
	if m := persuasionRe.FindString(letter); m != "" {
		r.Warnings = append(r.Warnings, "«"+strings.TrimSpace(m)+
			"» — это оправдательный аргумент вместо факта: убеждать работодателя, "+
			"что требование можно не выполнять, нельзя. Назови факт честно или убери пункт.")
	}
	// Призыв к диалогу должен быть ОДИН. Живое письмо заканчивалось тремя
	// подряд: «Готов обсудить детали…», «Буду рад обсудить… Спасибо за
	// внимание!» и «С уважением,». Правило требует финальную строку, модель
	// её дала, но своё добавила тоже — повтор никто не замечал.
	if n := closingCount(letter); n > 1 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"призыв к диалогу встречается %d раза — оставь один, иначе письмо размывается", n))
	}
	if !strings.Contains(low, "буду рад обсудить") {
		r.Warnings = append(r.Warnings, "нет обязательной строки: финальная строка «Буду рад обсудить… Спасибо за внимание!» — v4 §2.7")
	}
	// Контакты одной строкой: признаки, а не конкретные значения — контакты
	// живут в приватном context/, и промпт вправе их поменять.
	if !contactLineRe.MatchString(letter) {
		r.Warnings = append(r.Warnings, "нет обязательной строки: контакты одной строкой (телефон/Telegram/сайт/репозиторий) — v4 §2.6")
	}
	// Строка стека обязана иметь префикс «Стек: »: по нему пост-проверка
	// отличает стек от буллета (v4 §2.5/§3).
	if stackSectionOf(letter) == "" {
		r.Warnings = append(r.Warnings, "нет строки стека с префиксом «Стек: » — v4 §2.5/§3")
	}
	// Имя, которого нет в профиле, — фабрикация: модель выводит его из
	// хендла/ники. Текстом промпта это не лечится (правило есть, но мягкое),
	// поэтому проверяем здесь.
	if m := nameIntroRe.FindString(letter); m != "" {
		r.Warnings = append(r.Warnings, "в письме «"+m+" …» — имени в профиле нет: выдуманное имя = фабрикация, убери строку или подпишись «С уважением,» без имени")
	}
	if name := signedNameAfter(letter); name != "" {
		r.Warnings = append(r.Warnings, "подпись «С уважением, "+name+"» — имени в профиле нет: подпись без имени либо имя из профиля")
	}

	// Пробел, начатый с самоуничижения («не работал/не эксплуатировал/не
	// применял»), — красный флаг Senior (v4 §4).
	for _, line := range strings.Split(letter, "\n") {
		l := strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "-*• \t")))
		for _, bad := range []string{"не работал", "не эксплуатировал", "не применял", "опыта нет"} {
			if strings.HasPrefix(l, bad) {
				r.Warnings = append(r.Warnings, "строка пробела начинается с «"+bad+"» — v4 §4 требует начать с того, что ЕСТЬ")
			}
		}
	}

	return r
}

// contactLineRe — признаки строки контактов (не конкретные значения: контакты
// живут в приватном context/, и промпт вправе их поменять).
var contactLineRe = regexp.MustCompile(`(?im)^.*(telegram|t\.me/|\+\d|@\w{4,}).*$`)

// stackSectionOf — содержимое строки стека, если префикс («Стек: », «**Стек:** »)
// есть: голая строка стека без префикса не считается (v4 §2.5/§10).
func stackSectionOf(letter string) string {
	for _, line := range strings.Split(letter, "\n") {
		if stackLineRe.FindStringIndex(line) != nil && strings.Contains(line, ":") {
			return line
		}
	}
	return ""
}

// nameIntroRe — «Меня зовут …». Имени кандидата в профиле нет, поэтому такая
// фраза — чистая фабрикация: живой случай — модель вывела «Турал» из
// Telegram-хендла «example».
var nameIntroRe = regexp.MustCompile(`(?i)меня зовут`)

// signatureRe — начало блока подписи.
var signatureRe = regexp.MustCompile(`(?i)^\W*с уважением`)

// nameWordRe — слово вида «Турал» или «Иван»: с заглавной буквы, из букв.
var nameWordRe = regexp.MustCompile(`^\p{Lu}[\p{L}'’-]{0,23}$`)

// signedNameAfter — имя строкой сразу после «С уважением». В профиле имени
// нет, поэтому такая подпись — выдумка модели. Проверка узкая: 1-2 слова, каждое
// с заглавной буквы; строка контактов (цифры, «|», «@», точки) и «Команда
// разработки» (второе слово со строчной) под условие не попадают.
func signedNameAfter(letter string) string {
	lines := strings.Split(letter, "\n")
	for i, l := range lines {
		if !signatureRe.MatchString(l) {
			continue
		}
		for _, next := range lines[i+1:] {
			next = strings.TrimSpace(next)
			if next == "" {
				continue
			}
			if looksLikeName(next) {
				return next
			}
			return ""
		}
	}
	return ""
}

// looksLikeName — 1-2 слова, каждое с заглавной буквы.
func looksLikeName(s string) bool {
	words := strings.Fields(s)
	if len(words) == 0 || len(words) > 2 {
		return false
	}
	for _, w := range words {
		if !nameWordRe.MatchString(w) {
			return false
		}
	}
	return true
}

// isStackLine — строка стека в любом виде: с префиксом («Стек: …»,
// «**Стек:** …») или «голым» перечнем технологий. Голая строка считается
// стеком только если это чистый перечень (≥2 запятых, без скобок и тире),
// чтобы не принять за стек обычный буллет или предложение.
func isStackLine(line string) bool {
	t := strings.TrimSpace(line)
	if stackLineRe.MatchString(t) {
		return true
	}
	return bareStackRe.MatchString(t) &&
		strings.Count(t, ",") >= 2 &&
		!strings.ContainsAny(t, "(—:[")
}

// gapSectionIndex — начало секции честных пробелов (несколько формулировок).
// gapBlocksOf разбивает секцию пробелов на буллеты: область действия
// отрицания — свой пункт («Transactional outbox: не использовал; мост —
// буферизация…, досылка при недоступности Kafka»), а не только хвост
// после технологии.
func gapBlocksOf(gap string) []string {
	var blocks []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, line := range strings.Split(gap, "\n") {
		if isGapBulletStart(line) {
			flush()
		}
		cur = append(cur, line)
	}
	flush()
	return blocks
}

// isGapBulletStart — строка начинает новый пункт списка («- …», «• …», «* …»).
func isGapBulletStart(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "-") || strings.HasPrefix(t, "•") || strings.HasPrefix(t, "*")
}

// gapBlockNegated — в буллете, упоминающем технологию, она названа честным
// пробелом (или якорем моста), а не дублем факта. Тон определяется по клаузе
// самой технологии: «Transactional outbox: не использовал; мост — …,
// досылка при недоступности Kafka» — маркер отрицания стоит ДО технологии,
// хвостовой поиск давал ложный warning, а auto-fix по нему не мог ничего
// исправить и жёг генерации. Но если в клаузе технологии есть утвердительная
// подача («с Horizon знаком, готов освоить»), это дубль/противоречие —
// отрицание другой технологии в том же буллете его не оправдывает.
func gapBlockNegated(blocks []string, tech string) bool {
	reTech := regexp.MustCompile(`(?i)\b` + tech + `\b`)
	neg := regexp.MustCompile(`(?i)не работал|не применял|не использовал|не настраивал|опыта нет|нет опыта|не приходилось`)
	for _, b := range blocks {
		if !reTech.MatchString(b) {
			continue
		}
		if gapTechClauseAffirmative(b, tech) {
			return false
		}
		return neg.MatchString(b)
	}
	return false
}

// gapTechClauseAffirmative — клауза, называющая технологию, подаёт её
// утвердительно («с Horizon знаком», «Kafka использовал»).
func gapTechClauseAffirmative(block, tech string) bool {
	reTech := regexp.MustCompile(`(?i)\b` + tech + `\b`)
	affirm := regexp.MustCompile(`(?i)знаком|использовал|работал|умею|готов освоить|практик|опыт`)
	for _, c := range strings.FieldsFunc(block, func(r rune) bool {
		return r == '.' || r == ',' || r == ';' || r == '\n' || r == '!' || r == '?'
	}) {
		if reTech.MatchString(c) && affirm.MatchString(c) {
			return true
		}
	}
	return false
}

func gapSectionIndex(letter string) int {
	for _, h := range []string{"Честно о пробелах", "честно о пробел", "Пробелы:"} {
		if i := strings.Index(strings.ToLower(letter), strings.ToLower(h)); i >= 0 {
			return i
		}
	}
	return -1
}

// claimableTerms — термины, которые письмо может заявить как СВОЙ опыт, а
// профиль обязан подтверждать. Узкий домен (веб-безопасность/аутентификация):
// именно там модель стабильно дорисовывает «правдоподобное» (XSSI, basic
// auth, CSRF-токены) из соседних технологий, и именно там это не проверял
// прежний blacklist-подход «слова, которые нельзя». Список расширяемый, но
// короткий: каждый элемент проверяется на реальном кейсе.
var claimableTerms = []struct {
	// pat — как термин выглядит в письме.
	pat string
	// profileAlt — что искать в профиле (пусто = pat).
	profileAlt string
	msg        string
}{
	{"XSSI", "", "«XSSI sanitization» — в профиле такого факта нет (есть только XSS-санитизация в CMS Blog); это другая технология, проверь"},
	{"basic auth", "", "«basic auth» заявлен как опыт, но в профиле его нет — убери или добавь факт в context/"},
	{"CSRF", "", "«CSRF» заявлен как опыт — в профиле этого факта нет; не выдумывай защиту, которой не было"},
	{"cookie", "", "«cookie» заявлены как опыт — в профиле этого факта нет"},
	{"сесси", "", "«управление сессиями» заявлено как опыт — в профиле этого факта нет"},
	{"Helmet", "", "«Helmet» заявлен как опыт — в профиле этого фреймворка нет"},
}

// infraTechRe — кандидаты в «инфраструктурном» классе: латинские слова с
// внутренней заглавной (Tempo, Pyroscope, CloudNative, OpenTelemetry) либо из
// заглавных сегментов (Cilium, Linkerd). Цель — ловить выдумку, названную
// СВОИМ ИМЕНЕМ: blacklist «запрещённых слов» бессилен, потому что выдумку
// зовут как угодно, а claimableTerms вручную перечислял только security-домен.
// Живой кейс SRE (сентябрь 2026): модель написала «интегрируюсь в ваши
// инструменты observability (Tempo, Pyroscope)» и «CNPG, Service Mesh» — в
// профиле их нет ни в одном context/*.md, и audit молчал.
var infraTechRe = regexp.MustCompile(`\b[A-Z]{2,6}[a-z]?[A-Za-z]*\b`)

// CamelCase-кандидаты обрабатываются отдельно и списком: универсальный
// CamelCase-guard давал ЛОЖНОЕ срабатывание на «Observability» — это реальный
// факт профиля (Prometheus/Grafana), просто слово не написано дословно.
// Доменные имена перечислены в infraDomainTerms — путь рабочий и покрыт тестом.
var infraCamelRe = regexp.MustCompile(`\b[A-Z][a-z]+[A-Z][A-Za-z]*\b`)

// infraDomainTerms — доменные имена инфраструктуры, которых в профиле нет.
// Модель стабильно дорисовывает их в блоке «Адаптация под ваш стек», подгоняя
// письмо под вакансию: живой кейс SRE (сентябрь 2026) — «Tempo, Pyroscope»,
// «CNPG, Service Mesh», ни одного из них в context/*.md (0 вхождений).
var infraDomainTerms = []string{
	// ИИ-инструменты разработки. Живой кейс Fullstack Backend (сентябрь
	// 2026): письмо писало «ИИ-инструменты (Cursor, Claude Code и аналоги) —
	// ежедневная практика», в профиле их 0 вхождений. Как и Tempo/Pyroscope —
	// модель дорисовывает «правдоподобное» из соседних технологий.
	"Cursor", "Claude Code", "Copilot", "Windsurf", "Codeium", "Aider",
	"ChatGPT", "Gemini", "Grok",
	"Service Mesh", "servicemesh", "Istio", "Cilium", "Linkerd",
	"Tempo", "Pyroscope", "Jaeger", "Zipkin",
	"CloudNative-PG", "CloudNative", "Patroni", "Helm",
	"VictoriaMetrics", "Thanos", "Mimir",
	// Фронтенд/медиа-стек вакансий, которого в профиле нет (октябрь 2026).
	// Convex/Remotion не ловятся CamelCase-guard'ом (нет второй заглавной),
	// E2B — цифрой в имени, Expo — тем, что «экспо» есть внутри «экспорт».
	"Convex", "Remotion", "E2B", "Expo", "React Native",
}

// infraCommonWords — обычные слова в именительном падеже, которые письмо
// начинает с заглавной (первое слово буллета, «Managed», «Высоконагруженные»).
// Само camelCase-совпадение их не спасает: «Managed K8s» — это про вакансию,
// а не про навык, и ругаться на него нельзя.
var infraCommonWords = map[string]bool{
	"managed": true, "self": true, "high": true, "python": true, "golang": true,
	"rust": true, "java": true,
	"written": true, "built": true, "done": true, "плюс": true,
	"bottleneck": true, "latency": true, "greeting": true, "hello": true,
}

// infraNeutralWords — имена, которые не являются фактами о навыке: роли,
// языки, сокращения. Всё прочее CamelCase сверяется с профилем.
var infraNeutralWords = map[string]bool{
	"sre": true, "go": true, "sql": true, "api": true, "http": true, "grpc": true,
	"ci": true, "cd": true, "db": true, "tui": true, "sdk": true, "ide": true,
	// Технологии, подтверждённые профилем и потому частые в письме. Guard и сам
	// сверяется с профилем, но список не даёт тратить сверку на заведомо
	// валидные слова. Сюда НЕЛЬЗЯ вносить сомнительные термины (Tempo,
	// Pyroscope, Cilium): именно их письмо и выдумывает, и в списке они
	// проходили бы молча — этот класс проверяется сравнением с профилем.
	"prometheus": true, "grafana": true, "kafka": true, "redis": true,
	"elasticsearch": true, "kibana": true, "docker": true, "caddy": true,
	"postgresql": true, "clickhouse": true, "gitlab": true, "linux": true,
	"nginx": true, "symfony": true, "php": true, "bash": true,
}

// infraDeclineRe — профиль называет термин, но отрицает работу с ним.
var infraDeclineRe = regexp.MustCompile(`(?i)не работал|не применял|не использовал|не настраивал|не интегрировал|опыта нет|нет опыта|не знаком`)

// profileDeclinesTerm — термин есть в профиле, но назван пробелом
// («Service Mesh / Cilium / Linkerd / Istio: НЕ работал»). Заявлять его в
// письме как свой инструмент — противоречие профилю, а не свежий факт.
func profileDeclinesTerm(term, profile string) bool {
	lower := strings.ToLower(profile)
	for _, c := range strings.FieldsFunc(lower, func(r rune) bool {
		return r == '.' || r == ',' || r == ';' || r == '\n' || r == '(' || r == ')'
	}) {
		if !strings.Contains(c, strings.ToLower(term)) {
			continue
		}
		// «Service Mesh / Cilium / Linkerd / Istio: НЕ работал» — список
		// терминов идёт ДО двоеточия, отрицание после. Разрыв по «:» рвал
		// клаузу и терял отрицание, а профиль при этом содержит термин —
		// и письмо проходило как подтверждённый факт. Двоеточие не разделитель.
		if infraDeclineRe.MatchString(c) {
			return true
		}
	}
	return false
}

// claimPositively — термин заявлен в письме утвердительно, а не в клаузе с
// отрицанием. «Нет прямого опыта с Tempo» — честно; «интегрируюсь в Tempo» —
// заявление о своём инструменте, даже если в соседнем пункте пробел назван.
func claimPositively(letter, term string) bool {
	reTerm := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(term) + `[\w-]*`)
	affirm := regexp.MustCompile(`(?i)интегриру|навык|опыт|работал|знаком|использ|уверен|владе|внедри|применя|эксплуатирую|понимани|` +
		// Живая практика ИИ-инструментов выражалась словами «ежедневная практика»,
		// «производственное применение», «автоматизирую с их помощью» — без слова
		// «опыт». Без них Cursor/Claude Code проходили как неупомянутые, хотя
		// письмо заявляло «ежедневная практика» (сентябрь 2026).
		`практик|ежедневн|применяю|автоматизиру|пишу.{0,15}(с|в) них|работаю.{0,15}(с|в) них|`)
	// Декларация понимания/готовности — не заявление о навыке, даже если
	// профиль разрешает именно эту формулировку («допустимо «понимаю принципы
	// WAL»»). Живой кейс: с расширением маркеров практики «понимаю» попало в
	// affirm, и разрешённое мостом «понимаю принципы WAL» стало warning'ом
	// о выдумке. Здесь важно отличать «заявляю опыт» от «делюсь мостом».
	neg := regexp.MustCompile(`(?i)пробел|нет прямого|не име|не работал|не использ|отсутствует|не знаком|ограничен|нет опыта|нет практического|не вносил|` +
		`понима[юе]|понимать|разбира[юесь]|готовност|готовост|способност|основан.{0,15}на |мост|принципы? .{0,10}(поним|работ)|` +
		// «готова оперативно освоить ваш стек» — признание пробела, а не заявка
		// опыта. Живой кейс Fullstack (октябрь 2026): без этой формы блок
		// адаптации с перечислением чужого стека давал пачку ложных warning'ов.
		`готов[а-яё]*\s+(?:оперативно\s+|быстро\s+|легко\s+)?(?:освоить|осваивать|перенести|переносить|изучать|изучить|разобраться|углубиться)`)
	positive := false
	for _, c := range strings.FieldsFunc(letter, func(r rune) bool {
		return r == '.' || r == '\n' || r == '!' || r == '?'
	}) {
		if !reTerm.MatchString(c) {
			continue
		}
		if neg.MatchString(c) {
			continue
		}
		if affirm.MatchString(c) {
			positive = true
		}
	}
	return positive
}

// CheckInfraClaims — универсальный guard против выдуманных инструментов.
// Работает для ЛЮБОГО термина в CamelCase, а не по ручному списку: термин
// заявлен в письме утвердительно, но либо отсутствует в профиле, либо там
// прямо отрицается. Слова из стоплиста и термины, реально подтверждённые
// профилем, проверку проходят.
func CheckInfraClaims(letter, profile string) Result {
	var r Result
	low := strings.ToLower(profile)
	seen := map[string]bool{}
	check := func(term string) {
		key := strings.ToLower(term)
		if len(term) < 3 || infraNeutralWords[key] || infraCommonWords[key] || seen[key] {
			return
		}
		seen[key] = true
		if !claimPositively(letter, term) {
			return
		}
		if strings.Contains(low, key) && !profileDeclinesTerm(term, profile) {
			return
		}
		r.Warnings = append(r.Warnings, "«"+term+"» заявлен как свой инструмент/навык, "+
			"но в профиле его нет или он там отрицан — это фабрикация, убери или добавь факт в context/")
	}
	// all-caps аббревиатуры (CNPG, OTel) — без ложных срабатываний.
	for _, m := range infraTechRe.FindAllString(letter, -1) {
		check(strings.TrimSpace(m))
	}
	// Доменные имена: двусловные (Service Mesh) и CamelCase (CloudNative).
	for _, t := range infraDomainTerms {
		if strings.Contains(strings.ToLower(letter), strings.ToLower(t)) {
			check(t)
		}
	}
	for _, m := range infraCamelRe.FindAllString(letter, -1) {
		check(strings.TrimSpace(m))
	}
	return r
}

// letterWordLimit — лимит объёма из промпта (v4 §2.3).
const letterWordLimit = 200

// bodyWordCount — слова тела письма: без строки контактов и хвоста подписи.
// Считаем только содержательные слова, потому что разметка (**жирный**, *курсив*)
// и маркеры списков — это не объём текста, который читает человек.
func bodyWordCount(letter string) int {
	body := letter
	if contactLineRe.MatchString(body) {
		if idx := contactLineIdx(body); idx >= 0 {
			body = body[:idx]
		}
	}
	n := 0
	for _, w := range strings.Fields(body) {
		w = strings.Trim(w, "*_`#-•")
		if w == "" {
			continue
		}
		n++
	}
	return n
}

// contactLineIdx — начало строки контактов (телефон/Telegram/сайт/репозиторий).
func contactLineIdx(letter string) int {
	lines := strings.Split(letter, "\n")
	off := 0
	for _, l := range lines {
		if contactLineRe.MatchString(l) {
			return off
		}
		off += len(l) + 1
	}
	return -1
}

// closingRe — формы призыва к диалогу в конце письма.
var closingRe = regexp.MustCompile(`(?i)готов.{0,20}обсудить|буду.{0,20}рад.{0,10}обсудить|рад.{0,10}пообщ|хотел.{0,10}бы.{0,10}обсудить|буду.{0,20}связаться|предлагаю.{0,20}обсудить`)

// closingCount — сколько раз письмо призывает к диалогу. Считаем только в
// хвосте (после последней строки стека/контактов): призыв в начале письма —
// это не закрытие, а вводная часть.
func closingCount(letter string) int {
	tail := letter
	if idx := contactLineIdx(letter); idx >= 0 {
		tail = letter[idx:]
	}
	// «Буду рад обсудить… Спасибо за внимание!» — один призыв, две фразы.
	return len(closingRe.FindAllString(tail, -1))
}

// persuasionRe — оправдательный аргумент вместо факта: «Go-опыта от 3 лет
// достаточно», «глубина экспертизы компенсирует стаж». Кандидат не может
// решать за работодателя, что для него приемлемо; такое в письме читается как
// попытка продавить своё требование. Живой кейс Fullstack Backend
// (сентябрь 2026) — в блоке «Адаптация под ваш стек».
//
// Сознательно НЕ ловим обычные утверждения о соответствии («3 года коммерческого
// опыта», «стаж соответствует требованию»): это факт о стаже, а не уговор.
var persuasionRe = regexp.MustCompile(`(?i)(достаточно|хватает|компенсир|заменяет|не проблема|не критично|закрыва[ет|ю] требование|` +
	`покрыва[ет|ю] потребност|не догоняет критично|стоит вопроса|` +
	`(опыт|стаж|знания|навыки).{0,40}(не помеша|не важн|второстепенн))`)

// CheckProfile — дополнение к Check: сверяет заявленные в письме факты с
// профилем. Черный список «запрещённых слов» тут не годится — выдумка
// может называться как угодно, — поэтому проверка идёт по списку технических
// терминов, которые обязаны быть подтверждены профилем (claimableTerms).
//
// Живой кейс (вакансия IAM, сентябрь 2026): письмо заявило «XSSI
// sanitization» и «JWT и basic auth-механизмами», которых нет ни в одном
// context/*.md. Check этого не видел (он профиль не читает), письмо уходило
// с ложью, а требование «основы веб-безопасности» оставалось незакрытым.
//
// Пустой профиль — не ошибка проверки, а отсутствие основания судить: у
// пользователей без context/*.md каждое письмо иначе получило бы десятки
// ложных замечаний.
func CheckProfile(letter, profile string) Result {
	var r Result
	if strings.TrimSpace(profile) == "" {
		return r
	}
	low := strings.ToLower(letter)
	prof := strings.ToLower(profile)
	for _, t := range claimableTerms {
		needle := strings.ToLower(t.pat)
		if !strings.Contains(low, needle) {
			continue
		}
		// В самом профиле термин есть — значит, факт подтверждён, а письмо
		// его лишь цитирует. Ищем и альтернативную форму записи.
		search := needle
		if t.profileAlt != "" {
			search = strings.ToLower(t.profileAlt)
		}
		if strings.Contains(prof, search) {
			continue
		}
		r.Warnings = append(r.Warnings, t.msg)
	}
	// Универсальный guard против выдуманных инструментов в CamelCase.
	r.Warnings = append(r.Warnings, CheckInfraClaims(letter, profile).Warnings...)
	// Заявка ИИ-инструментов без факта: именами не ловится (письмо их не
	// называет), а в профиле они есть только внутри запретной строки.
	r.Warnings = append(r.Warnings, checkAIClaim(letter, profile)...)
	return r
}

// --- Заявка ИИ-инструментов без факта в профиле ---
//
// Живой кейс Fullstack Backend (октябрь 2026): письмо писало «ИИ-инструменты —
// ежедневная практика (постановка задач, генерация, ревью, отладка)» без единого
// названия инструмента, а в профиле Cursor/Claude Code/Copilot — 0 вхождений
// (context/01:336). Именами это не ловится: письмо их не называет. Хуже того —
// в профиле имена ЕСТЬ, но только внутри самой запретной строки («0 вхождений,
// модель дописывает… ловится на интервью»), и Contains(prof, «cursor») дал бы
// ложное «подтверждено».
var (
	// aiToolClaimRe — заявка класса «ИИ-инструменты в работе» в письме.
	aiToolClaimRe = regexp.MustCompile(`(?i)(ии[-\s]?инструмент|ии[-\s]?код|искусственн\w+ интеллект)`)
	// aiToolDailyRe — признак ежедневного применения, а не разовой задачи.
	aiToolDailyRe = regexp.MustCompile(`(?i)(ежедневн[а-яё]*[ \t]+практик[а-яё]*|ежедневно|кажд[а-яё]+ день)`)
	// aiToolNames — подтверждение факта: конкретное имя инструмента в профиле
	// (вне запретных строк).
	aiToolNames = regexp.MustCompile(`(?i)\b(cursor|claude|copilot|windsurf|aider|codeium|cline|continue|chatgpt|gemini|grok)\b`)
	// profileForbiddenMarkers — маркеры строки-запрета. Такой блок профиля
	// запрещает заявку, а не подтверждает её: его надо вырезать перед сверкой.
	profileForbiddenMarkers = regexp.MustCompile(`0 вхождений|НЕ заявлять|дописывает|ловится на интервью`)
)

// stripForbiddenProfile — профиль без запретных блоков. Блок = непрерывная
// группа строк буллета: начинается с «- »/«* »/«#» либо является продолжением
// (ведущие пробелы). Запрет context/01:336-340 занимает 5 строк, и маркеры
// разбросаны по ним — построчный сплит его бы не вырезал.
func stripForbiddenProfile(profile string) string {
	var kept []string
	var block []string
	flush := func() {
		if len(block) == 0 {
			return
		}
		if !profileForbiddenMarkers.MatchString(strings.Join(block, "\n")) {
			kept = append(kept, block...)
		}
		block = nil
	}
	for _, line := range strings.Split(profile, "\n") {
		trim := strings.TrimSpace(line)
		isBulletStart := trim == "" ||
			strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") ||
			strings.HasPrefix(trim, "#")
		if isBulletStart {
			flush()
		}
		block = append(block, line)
	}
	flush()
	return strings.Join(kept, "\n")
}

// checkAIClaim — заявка «ИИ-инструменты — ежедневная практика» без факта
// в профиле (после вырезания запретных блоков).
func checkAIClaim(letter, profile string) []string {
	if !aiToolClaimRe.MatchString(letter) || !aiToolDailyRe.MatchString(letter) {
		return nil
	}
	if aiToolNames.MatchString(stripForbiddenProfile(profile)) {
		return nil
	}
	return []string{"«ИИ-инструменты — ежедневная практика» заявлены без факта в профиле (ни одного названия инструмента там нет) — либо зафиксируй факт в профиле, либо убери утверждение"}
}
