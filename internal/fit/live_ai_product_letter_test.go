package fit

import (
	"strings"
	"testing"
)

// Живая вакансия AI-продукта (прогон, сентябрь 2026). Из 7 обязательных
// требований в разбор попали только 2, и оба закрылись, поэтому вердикт вышел
// «~100%» при фактическом пробеле по Media/Video, платёжным шлюзам,
// подпискам и веб-воронкам.
const aiProductVacancyLive = `Обязанности:
    Разработка полного цикла: релиз продуктовых фичей на стыке фронтенда и бэкенда (API, интеграции, платежные шлюзы, подписки и веб-воронки);
    Агентские AI-системы: улучшение логики агентов (распознавание намерений пользователя, подбор футажей, сборка монтажа, контекстное управление диалогом);
    Оптимизация ИИ: повышение качества генерации, снижение задержки (latency) и стоимости запросов, оркестрация моделей и бенчмаркинг;
    Работа с медиа: обеспечение быстрого и отказоустойчивого рендеринга, кодирования, стриминга, загрузки и экспорта видео;
    Продуктовая полировка: развитие веб- и мобильного интерфейса, создание гладкого онбординга и работа с продуктовой аналитикой.
Требования:
    Практический опыт fullstack-разработки в реальных B2C/B2B-продуктах с полным циклом доведения фичей до продакшна;
    Экспертиза в одном из двух ключевых направлений:
        Media / Video: опыт в видео/аудио приложениях, видеоредакторах, пайплайнах рендеринга, кодеках, плеере или медиа-инфраструктуре;
        AI Agents: опыт построения агентских систем, Tool Use, управления контекстом и многошаговых цепочек рассуждений моделей;
    Готовность гибко переключаться между UI, серверной частью и инфраструктурой моделей;
    Развитое продуктовое чутье, внимание к деталям UX/UI и скорости отклика интерфейса;
    Владение английским языком на уровне B2+ для регулярного взаимодействия в международной распределенной команде.`

// TestExtractPromptSplitsAlternativeDirections — «Экспертиза в одном из двух
// направлений: Media/Video … или AI Agents …» нельзя отдавать одним
// требованием: закрытие по AI Agents прячет пробел по Media/Video, и вердикт
// показывает «все требования закрыты» при пустой половине ключевого условия.
func TestExtractPromptSplitsAlternativeDirections(t *testing.T) {
	system, _ := ExtractPrompt(aiProductVacancyLive)
	low := strings.ToLower(system)
	if !strings.Contains(low, "одном из двух") && !strings.Contains(low, "или") {
		t.Errorf("промпт разбора не объясняет правило для требований «в одном из двух направлений»:\n%s", system)
	}
	if !strings.Contains(low, "отдельн") && !strings.Contains(low, "разде") {
		t.Errorf("промпт требует разбивать альтернативные направления на отдельные требования:\n%s", system)
	}
}

// TestExtractPromptKeepsProductAndLanguageRequirements — продуктовые блоки
// (платёжные шлюзы, подписки, веб-воронки, мобильной UI) и языковое
// требование выпадали из разбора целиком: в вердикте их не было, поэтому
// «~100%» ничего не говорил о фактических пробелах.
func TestExtractPromptKeepsProductAndLanguageRequirements(t *testing.T) {
	system, _ := ExtractPrompt(aiProductVacancyLive)
	low := strings.ToLower(system)
	if !strings.Contains(low, "платеж") && !strings.Contains(low, "платёж") {
		t.Errorf("промпт не упоминает продуктовые блоки (платежи/подписки/воронки):\n%s", system)
	}
	if !strings.Contains(low, "b2") || !strings.Contains(low, "soft") {
		t.Errorf("промпт не разбирается с языковыми требованиями уровня (B2+) — сейчас они уходят в soft молча:\n%s", system)
	}
}

// TestAlternativeDirectionsNotHiddenByClosedOne — страховка на случай, когда
// модель-парсер всё же схлопнет «в одном из двух направлений» в одно
// требование. Закрытие по AI Agents не должно прятать пробел по Media/Video:
// требование должно уйти в оговорку с упоминанием непокрытого направления,
// а не в «закрыто в письме».
func TestAlternativeDirectionsNotHiddenByClosedOne(t *testing.T) {
	reqs := Requirements{Role: "fullstack", MustHave: []Requirement{
		{Text: "Экспертиза в одном из двух направлений: Media/Video (видеоредакторы, рендеринг, кодеки) или AI Agents (агентские системы, Tool Use, контекст)", Kind: "must", Category: "stack"},
	}}
	letter := "AI Agents: интеграция LLM (Llama 3.3) в production-сервисы, каскадные fallback-цепочки, управление контекстом. " +
		"Tool Use и цепочки рассуждений в рамках RAG-систем. Media/Video: опыта работы с видеопайплайнами и кодеками нет."
	f := Evaluate(DefaultConcepts(), reqs, "LLM: Llama 3.3, RAG CLI System, fallback-цепочки, кэширование.", letter, "AI Agents / Media")
	if f.Verdict == Skip {
		t.Fatalf("письмо не должно получать skip: %+v", f)
	}
	for _, c := range f.Covered {
		if strings.Contains(c.Note, "закрыто в письме") &&
			!strings.Contains(strings.ToLower(c.Note), "медиа") &&
			!strings.Contains(strings.ToLower(c.Note), "video") {
			t.Logf("закрыто целиком: %q — проверяем ниже по оговоркам", c.Note)
		}
	}
	// Если требование закрылось «в письме», в ноте должно быть видно, что
	// непокрытое направление осталось. Иначе пробел по Media/Video пропал.
	for _, c := range append(append([]Req{}, f.Caveats...), f.Missing...) {
		t.Logf("оговорка/пробел: %q", c.Note)
	}
	if len(f.Covered) == 1 && f.Covered[0].Source == SrcLetter &&
		len(f.Caveats) == 0 && len(f.Missing) == 0 {
		t.Errorf("пробел по Media/Video пропал при закрытии по AI Agents: %+v", f)
	}
}

// forbiddenClauseOnly — дословная строка-запрет context/01:336-340: ЕДИНСТВЕННОЕ
// упоминание «AI Agents» и «Cursor/Claude Code» во всём контексте. Запрет
// запрещает, а не подтверждает.
const forbiddenClauseOnly = `- **ИИ-инструменты разработки — НЕ заявлять без факта:** в профиле Cursor, Claude Code,
  Copilot, ChatGPT, Gemini — 0 вхождений. Если реально используешь — добавь сюда
  строку с конкретикой (задачи, что именно делаешь, с какого месяца). Модель
  дописывает «ИИ-инструменты (Cursor, Claude Code и аналоги) — ежедневная практика»
  по вакансии про AI Agents, и это ловится на интервью первым вопросом.`

// TestProfileForbiddenClauseIsNotAFact — живой кейс Fullstack Backend (октябрь
// 2026): fit сообщил «в профиле есть факт по большинству токенов (не упомянуты:
// tool) — впиши в письмо» и повесил это в fitFixable. Факта нет: токен нашёлся
// ВНУТРИ строки-запрета. Автоправка физически не может закрыть требование
// (писать запрещено), поэтому кавеат зацикливал автофикс.
func TestProfileForbiddenClauseIsNotAFact(t *testing.T) {
	reqs := Requirements{MustHave: []Requirement{{Text: "Экспертиза в AI Agents: опыт построения агентских систем, Tool Use, управления контекстом и многошаговых цепочек рассуждений моделей"}}}
	f := Evaluate(DefaultConcepts(), reqs, forbiddenClauseOnly, "", "AI Agents / Media")
	for _, c := range f.Caveats {
		if strings.Contains(c.Note, "впиши") || strings.Contains(c.Note, "в профиле есть факт") {
			t.Errorf("строка-запрет принята за факт: %+v", c)
		}
	}
	// Требование вообще не должно числиться закрытым.
	for _, c := range f.Covered {
		if strings.Contains(c.Text, "AI Agents") {
			t.Errorf("требование закрыто по строке-запрету: %+v", c)
		}
	}
}

// TestProfileRealFactStillClosedAfterForbiddenStrip — обратный случай: если
// факт лежит в обычной строке профиля, запрет-буллет рядом его не обнуляет.
func TestProfileRealFactStillClosedAfterForbiddenStrip(t *testing.T) {
	reqs := Requirements{MustHave: []Requirement{{Text: "Экспертиза в AI Agents: опыт построения агентских систем"}}}
	profile := forbiddenClauseOnly + "\n- **AI Agents в работе:** агентская цепочка (Tool Use, вызов инструментов, управление контекстом диалога) в production-сервисе.\n"
	f := Evaluate(DefaultConcepts(), reqs, profile, "", "AI Agents")
	if len(f.Caveats) == 0 && len(f.Covered) == 0 {
		t.Fatal("реальный факт про AI Agents пропал после вырезания запретов")
	}
}

// mediaAgentLetter — живое письмо прогона mistral-medium-3.5 (октябрь 2026):
// «AI-системы» и «Prometheus/Grafana» есть, агентской логики и видеорендеринга
// нет. Именно на этом тексте fit выдал два ложных закрытия.
const mediaAgentLetter = `Чем могу быть полезен:
- **AI-системы:** 5 production-сервисов с LLM/ML (Fraud Engine, Stable ID, Bundle ID, Domain ID, Geo-mapping Service), RAG CLI System (FTS5, bm25(), indexing, retrieval-каскады)
- **Оптимизация ИИ:** снижение стоимости API на 70% (Geo-mapping Service), экономия токенов 60–70% (Bundle ID), Prometheus + Grafana (3 дашборда, 23 панели)
- **Высоконагруженные системы:** Fraud Engine (1000+ RPS, P95 < 4.2ms), Stable ID (10 000 RPS, Kafka, 20+ воркеров)`

// TestAgentDutyNotClosedByGenericAI — живой ложный close №1: обязанность
// «Улучшение логики AI-агентов (распознавание намерений, подбор футажей,
// сборка монтажа, контекстное управление диалогом)» закрывалась письмом, где
// про агентов нет ничего — только «AI-системы».
//
// Механизм: normToken превращает «AI-агентов» в ЕДИНСТВЕННЫЙ токен «ai»
// (дефис срезается), письмо содержит «AI-системы» → all=true → «закрыто в
// письме». В том же прогоне must-have «Экспертиза в AI Agents…» (4 токена)
// честно не закрыт — одно требование и «закрыто», и «нет».
func TestAgentDutyNotClosedByGenericAI(t *testing.T) {
	reqs := Requirements{MustHave: []Requirement{{
		Text: "Улучшение логики AI-агентов (распознавание намерений пользователя, подбор футажей, сборка монтажа, контекстное управление диалогом)",
	}}}
	f := Evaluate(DefaultConcepts(), reqs, "", mediaAgentLetter, "AI Agents")
	for _, c := range f.Covered {
		t.Errorf("обязанность про AI-агентов закрыта без агентского опыта: src=%s note=%q", c.Source, c.Note)
	}
}

// TestVideoRequirementNotClosedByObservability — живой ложный close №3:
// «Обеспечение быстрого и отказоустойчивого рендеринга видео» закрывалось
// признаками концепта «эксплуатация и observability» (триггер содержит
// «отказоустойчив»), а сигналами были Prometheus/Grafana — про дашборды,
// не про видеорендеринг.
func TestVideoRequirementNotClosedByObservability(t *testing.T) {
	reqs := Requirements{MustHave: []Requirement{{
		Text: "Обеспечение быстрого и отказоустойчивого рендеринга, кодирования, стриминга, загрузки и экспорта видео",
	}}}
	f := Evaluate(DefaultConcepts(), reqs, "", mediaAgentLetter, "Media/Video")
	for _, c := range f.Covered {
		t.Errorf("видео-требование закрыто по observability-признакам: note=%q", c.Note)
	}
}

// adaptationLine — живая строка из письма mistral-medium-3.5 (октябрь 2026):
// перечисление стека ВАКАНСИИ, за которым письмо честно признаёт, что опыта нет
// («готова оперативно освоить»). До фикса fit читал «имею опыт fullstack-
// разработки» как опыт с каждой из перечисленных технологий.
const adaptationLine = "- TypeScript/React Native/Expo/Convex/Remotion/E2B: имею опыт fullstack-разработки (PHP/Go + React в CMS Blog) и AI-интеграций; готова оперативно освоить ваш медиа/видео-стек"

// TestAdaptationListingIsNotExperience — живой ложный close №2: пять
// требований (TypeScript, React Native/Expo, Convex, Remotion, E2B) отмечены
// «закрыто в письме», хотя письмо их только перечисляет как то, что предстоит
// освоить. sentences() режет по ‘;’, поэтому «готова освоить» осталось в
// соседней клаузе и understanding не сработал.
func TestAdaptationListingIsNotExperience(t *testing.T) {
	reqs := Requirements{MustHave: []Requirement{
		{Text: "TypeScript"}, {Text: "React Native / Expo"},
		{Text: "Convex"}, {Text: "Remotion"}, {Text: "E2B"},
	}}
	letter := "Адаптация под ваш стек:\n" + adaptationLine + "\n"
	f := Evaluate(DefaultConcepts(), reqs, "", letter, "Fullstack Backend")
	for _, c := range f.Covered {
		t.Errorf("технология из списка-адаптации закрыта как опыт: src=%s text=%q", c.Source, c.Text)
	}
}

// TestAdaptationListingKeepsRealStackFact — стоп-тест: список-адаптация не
// должен обнулять факты из других буллетов того же письма.
func TestAdaptationListingKeepsRealStackFact(t *testing.T) {
	reqs := Requirements{MustHave: []Requirement{{Text: "Go"}, {Text: "TypeScript"}}}
	letter := "- Go: 3 года production (Stable ID, Fraud Engine, Bundle ID)\n" +
		"Адаптация под ваш стек:\n" + adaptationLine + "\n"
	f := Evaluate(DefaultConcepts(), reqs, "", letter, "Fullstack Backend")
	found := false
	for _, c := range f.Covered {
		if strings.EqualFold(strings.TrimSpace(c.Text), "Go") {
			found = true
		}
	}
	if !found {
		t.Errorf("реальный факт Go пропал после вырезания списка-адаптации: covered=%+v missing=%+v", f.Covered, f.Missing)
	}
}
