package fit

import (
	"strings"
	"testing"

	"github.com/turkprogrammer/covercraft/frontend"
)

// profileGo — фикстура профиля Go-кандидата (сокращённая карта фактов).
const profileGo = "Основной язык — Go. Проект «Мониторинг»: VictoriaMetrics, " +
	"Kafka, ClickHouse, CI/CD на GitLab CI, деплой автоматизирован. " +
	"155+ сервисов, p99 latency. Проект «Классификация»: ML-модели, F1 92%."

func mustReqs(must, nice []string, role string) Requirements {
	r := Requirements{Role: role}
	for _, m := range must {
		r.MustHave = append(r.MustHave, Requirement{Text: m, Kind: "must", Category: "stack"})
	}
	for _, n := range nice {
		r.NiceToHave = append(r.NiceToHave, Requirement{Text: n, Kind: "nice", Category: "stack"})
	}
	return r
}

// Чистое письмо закрывает все must-have в письме — apply, 100%.
func TestEvaluateApply(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Kafka и ClickHouse"}, nil, "go-primary")
	letter := "Стек: Go, Kafka, ClickHouse\nПара слов о проекте."
	f := Evaluate(reqs, profileGo, letter, "вакансия")
	if f.Verdict != Apply {
		t.Errorf("verdict = %q, хочу %q (детали: %+v)", f.Verdict, Apply, f)
	}
	if f.Score != 100 {
		t.Errorf("score = %d, хочу 100", f.Score)
	}
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Errorf("покрытие должно быть из письма: %+v", f.Covered)
	}
}

// Факт в профиле, но не в письме: covered с советом «добавь», скор 70.
func TestEvaluateProfileButNotLetter(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с VictoriaMetrics"}, nil, "go-primary")
	letter := "Пишу про другой проект, метрики не упоминаю."
	f := Evaluate(reqs, profileGo, letter, "вакансия")
	if f.Verdict != Apply {
		t.Errorf("verdict = %q, хочу %q: факт в профиле закрывает must-have", f.Verdict, Apply)
	}
	if f.Score != 70 {
		t.Errorf("score = %d, хочу 70", f.Score)
	}
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcProfile {
		t.Fatalf("источник должен быть profile: %+v", f.Covered)
	}
	if !adviceHas(f.Advice, "впиши в письмо") {
		t.Errorf("нужен совет «добавь в письмо»: %+v", f.Advice)
	}
}

// K8s нет нигде и моста нет — missing → skip. Классический риск отклика.
// Один незакрытый must-have — caveats, а не skip: серая зона, совет называет
// пробел и предлагает оценить его критичность. Два missing — уже skip.
func TestEvaluateSingleMissingIsCaveats(t *testing.T) {
	reqs := mustReqs([]string{"Опыт эксплуатации Kubernetes в проде"}, nil, "go-primary")
	letter := "Стек: Go, Kafka"
	f := Evaluate(reqs, profileGo, letter, "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q: один пробел не роняет вердикт", f.Verdict, Caveats)
	}
	if len(f.Missing) != 1 {
		t.Errorf("должен быть один missing: %+v", f)
	}
	if !adviceHas(f.Advice, "критичен ли он для этой вакансии") {
		t.Errorf("совет должен называть пробел и предлагать оценить критичность: %+v", f.Advice)
	}
	// Заголовок уже называет единственный пробел — построчного дубля нет.
	if adviceHas(f.Advice, "не закрыто ничем") {
		t.Errorf("при одном missing построчный совет дублирует заголовок: %+v", f.Advice)
	}
}

// Два незакрытых must-have — skip: дырки много, откликаться не стоит.
func TestEvaluateSkipOnTwoMissing(t *testing.T) {
	reqs := mustReqs([]string{"Опыт эксплуатации Kubernetes в проде", "Опыт с Docker Swarm"}, nil, "go-primary")
	letter := "Стек: Go, Kafka"
	f := Evaluate(reqs, profileGo, letter, "вакансия")
	if f.Verdict != Skip {
		t.Errorf("verdict = %q, хочу %q: два missing — skip", f.Verdict, Skip)
	}
	// При skip построчные советы обязательны: заголовок имён не называет.
	for _, r := range f.Missing {
		if !adviceHas(f.Advice, "«"+r.Text+"» не закрыто ничем") {
			t.Errorf("нет построчного совета для «%s»: %+v", r.Text, f.Advice)
		}
	}
}

// Живой кейс (SQL-вакансия): один must-have закрыт профилем (совет
// «впиши в письмо» — полезен и остаётся), один — missing. Вердикт —
// caveats, заголовок называет missing; построчного дубля быть не должно.
func TestEvaluateMixedProfileAndSingleMissing(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с ClickHouse", "Опыт эксплуатации Kubernetes в проде"}, nil, "go-primary")
	f := Evaluate(reqs, profileGo, "Стек: Go, Kafka", "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q: профиль-закрытие + один missing", f.Verdict, Caveats)
	}
	if !adviceHas(f.Advice, "впиши в письмо") {
		t.Errorf("совет «впиши в письмо» должен остаться: %+v", f.Advice)
	}
	if adviceHas(f.Advice, "не закрыто ничем") {
		t.Errorf("построчный совет про единственный missing дублирует заголовок: %+v", f.Advice)
	}
}

// unknown: требование без технологий (домен) — caveat, но не skip сам по себе.
func TestEvaluateUnknownSingleIsCaveats(t *testing.T) {
	reqs := mustReqs([]string{"Опыт в финтех-домене"}, nil, "go-primary")
	letter := "Стек: Go, Kafka, ClickHouse"
	f := Evaluate(reqs, profileGo, letter, "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q: одно unknown не роняет вердикт", f.Verdict, Caveats)
	}
	if len(f.Caveats) != 1 || f.Caveats[0].Source != SrcUnknown {
		t.Errorf("unknown должен попасть в caveats: %+v", f.Caveats)
	}
}

// Два unknown — caveats: это «нет данных», а не «нет опыта», порог
// занижения вердикта — три.
func TestEvaluateTwoUnknownIsCaveats(t *testing.T) {
	reqs := mustReqs([]string{"Опыт в финтех-домене", "Понимание скоринга"}, nil, "go-primary")
	f := Evaluate(reqs, profileGo, "Стек: Go, Kafka", "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q: два unknown — данных мало, но не приговор", f.Verdict, Caveats)
	}
}

// Три unknown — данных о кандидате слишком мало, советовать отклик
// нельзя: skip.
func TestEvaluateThreeUnknownIsSkip(t *testing.T) {
	reqs := mustReqs([]string{"Опыт в финтех-домене", "Понимание скоринга", "Опыт банковских интеграций"}, nil, "go-primary")
	f := Evaluate(reqs, profileGo, "Стек: Go, Kafka", "вакансия")
	if f.Verdict != Skip {
		t.Errorf("verdict = %q, хочу %q: три unknown — данных нет", f.Verdict, Skip)
	}
	// Сводный совет по unknown — одна строка со всеми требованиями.
	if adviceHas(f.Advice, "это не значит «опыта нет»: «Опыт в финтех-домене», «Понимание скоринга», «Опыт банковских интеграций»") == false {
		t.Errorf("нужна сводная строка unknown со всеми требованиями: %+v", f.Advice)
	}
	countUnknownLines := 0
	for _, a := range f.Advice {
		if strings.Contains(a, "проверь вручную") {
			countUnknownLines++
		}
	}
	if countUnknownLines != 1 {
		t.Errorf("unknown-советы должны быть сведены в одну строку, строк: %d", countUnknownLines)
	}
}

// Мост: Elasticsearch нет ни в письме, ни в профиле, но есть соседний
// опыт (ClickHouse в профиле) — caveats с мостом, а не skip.
func TestEvaluateBridge(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Elasticsearch"}, nil, "go-primary")
	letter := "Проект «Мониторинг»: метрики, p99."
	f := Evaluate(reqs, profileGo, letter, "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q", f.Verdict, Caveats)
	}
	if len(f.Caveats) != 1 || f.Caveats[0].Source != SrcBridge {
		t.Fatalf("должен быть bridge-кавеат: %+v", f.Caveats)
	}
	if !adviceHas(f.Advice, "слабое место") {
		t.Errorf("оговорка должна называть слабое место: %+v", f.Advice)
	}
}

// «Будет плюсом» не требует покрытия: nice не покрыт — apply без штрафа.
func TestEvaluateNiceToHaveNoPenalty(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Kafka"}, []string{"Знание ClickHouse будет плюсом"}, "go-primary")
	letter := "Стек: Go, Kafka"
	f := Evaluate(reqs, profileGo, letter, "вакансия")
	if f.Verdict != Apply || f.Score != 100 {
		t.Errorf("verdict = %q, score = %d: nice не должен штрафовать", f.Verdict, f.Score)
	}
}

// Мягкие требования не считаются пробелами вовсе.
func TestEvaluateSoftIgnored(t *testing.T) {
	reqs := mustReqs([]string{"Самоорганизованность и темп работы"}, nil, "go-primary")
	f := Evaluate(reqs, profileGo, "Стек: Go", "вакансия")
	if f.Verdict == Skip {
		t.Errorf("soft-требования не должны ронять вердикт: %+v", f)
	}
	if len(f.Missing) != 0 {
		t.Errorf("soft не должен попадать в missing: %+v", f.Missing)
	}
}

// Роль другого профиля: PHP-primary у Go-кандидата — skip.
func TestEvaluateRoleMismatch(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Laravel"}, nil, "php-primary")
	letter := "Стек: Go, Kafka"
	f := Evaluate(reqs, profileGo, letter, "вакансия")
	if f.Verdict != Skip {
		t.Errorf("verdict = %q, хочу %q: PHP-primary для Go-кандидата", f.Verdict, Skip)
	}
}

// Синонимы: вакансия пишет K8s, профиль — Kubernetes.
func TestEvaluateSynonyms(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с K8s"}, nil, "go-primary")
	letter := "Эксплуатировал Kubernetes в проде."
	f := Evaluate(reqs, profileGo, letter, "вакансия")
	if f.Verdict != Apply || f.Score != 100 {
		t.Errorf("verdict = %q score = %d: K8s и Kubernetes — одно и то же", f.Verdict, f.Score)
	}
}

// Разбор не удался (пустые списки) — вердикта нет, панель не рендерится.
func TestEvaluateEmptyRequirementsNoVerdict(t *testing.T) {
	f := Evaluate(Requirements{}, profileGo, "письмо", "вакансия")
	if f.Verdict != "" {
		t.Errorf("verdict = %q, хочу пустой — панели не должно быть", f.Verdict)
	}
}

// Реальный кейс из прогона: архитекторская вакансия, письмо Go-кандидата.
// До правки все концептные требования падали в unknown («нет распознаваемых
// технологий»), вердикт был skip при пустом missing — это регрессия.
func TestEvaluateArchitectVacancyUserCase(t *testing.T) {
	// Сокращённое письмо из реального прогона: сигналы концептов сохранены.
	letter := "Мой профиль (17 лет бэкенда, 3 года Go). 10 ADR, Hexagonal Architecture / DDD. " +
		"Stable ID: Kafka, event-driven, 10 000 RPS, at-least-once, идемпотентность. " +
		"миграция 329 847 legacy-записей без даунтайма. " +
		"Prometheus + Grafana, алертинг P99 > 100ms, blue-green deploy, graceful shutdown."
	profile := "Основной язык — Go. Backend: 17 лет, PHP с 2005. Проект «Мониторинг»: VictoriaMetrics, Kafka."
	reqs := mustReqs([]string{
		"Релевантный опыт работы архитектором от 5 лет",
		"Опыт работы в backend-разработке",
		"Опыт проектирования распределённых систем",
		"Опыт работы с высоконагруженными и критичными системами",
		"Практический опыт модернизации legacy-систем",
		"Умение принимать решения с учётом сроков, стоимости, рисков и сложности сопровождения",
		"Понимание эксплуатации, мониторинга, отказоустойчивости и деградации сервисов",
	}, nil, "go-primary")
	f := Evaluate(reqs, profile, letter, "вакансия архитектор")

	if f.Verdict == Skip {
		t.Errorf("verdict = skip, а в письме нет ни одного незакрытого must-have: %+v", f)
	}
	if len(f.Missing) != 0 {
		t.Errorf("missing должен быть пуст — концептные требования закрываются по признакам: %+v", f.Missing)
	}
	if f.Score < 80 {
		t.Errorf("score = %d, хочу >= 80: пять из семи must-have закрыты признаками/токенами", f.Score)
	}
	// «Распределённые системы» — по признакам Kafka/event-driven.
	var dist *Req
	for i := range f.Covered {
		if strings.Contains(f.Covered[i].Text, "распределённых") {
			dist = &f.Covered[i]
		}
	}
	if dist == nil || dist.Source != SrcLetter || !strings.Contains(dist.Note, "распределённые системы") {
		t.Errorf("«распределённые системы» должны быть закрыты в письме по признакам: %+v", f.Covered)
	}
	// unknown — сводной строкой, не построчно.
	lines := 0
	for _, a := range f.Advice {
		if strings.Contains(a, "проверь вручную") {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("unknown-советы должны быть одной строкой, строк: %d (%+v)", lines, f.Advice)
	}
}

// Негатив: то же вакансия, письмо без сигналов концептов — все концепты
// unknown, вердикт честно роняется в skip.
func TestEvaluateArchitectVacancyEmptyLetter(t *testing.T) {
	profile := "Основной язык — Go. Backend: 10 лет."
	reqs := mustReqs([]string{
		"Опыт проектирования распределённых систем",
		"Опыт работы с высоконагруженными и критичными системами",
		"Понимание эксплуатации, мониторинга, отказоустойчивости и деградации сервисов",
	}, nil, "go-primary")
	f := Evaluate(reqs, profile, "Стек: Go, Kafka", "вакансия")
	if f.Verdict != Skip {
		t.Errorf("verdict = %q, хочу %q: письмо без сигналов концептов", f.Verdict, Skip)
	}
	if len(f.Missing) != 0 {
		t.Errorf("unknown не должен превращаться в missing: %+v", f.Missing)
	}
}

// Имена вердиктов — контракт с UI (словарь подписей и CSS-классы
// verdict-* в index.html). Смена строки здесь ломает фронт.
func TestVerdictNamesMatchUI(t *testing.T) {
	for _, v := range []string{Apply, Caveats, Skip} {
		if !strings.Contains(frontend.IndexHTML, v) {
			t.Errorf("вердикт %q не упомянут в UI — плашка покажет undefined", v)
		}
	}
}

// Правило большинства в токен-ветке: «Linux (systemd, cron)» — systemd в
// письме есть, cron нет. Один неупомянутый термин не должен ронять
// требование в «не закрыто ничем»: закрыто письмом, недостающее названо.
func TestEvaluateMajorityTokensLetter(t *testing.T) {
	reqs := mustReqs([]string{"Знание системных сервисов ОС Linux (systemd, cron)"}, nil, "go-primary")
	letter := "Linux 17+ лет диагностики, systemd-юниты в production, журналы."
	profile := ""
	f := Evaluate(reqs, profile, letter, "вакансия")
	if f.Verdict == Skip {
		t.Errorf("verdict = skip, но ядро требования закрыто письмом: %+v", f)
	}
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("покрытие должно быть из письма: %+v", f.Covered)
	}
	if !strings.Contains(f.Covered[0].Note, "cron") {
		t.Errorf("нота должна честно называть недостающий токен: %q", f.Covered[0].Note)
	}
}

// Большинство токенов в профиле, не в письме: закрыто профилем (0.7),
// нота называет недостающие токены.
func TestEvaluateMajorityTokensProfile(t *testing.T) {
	reqs := mustReqs([]string{"Знание системных сервисов ОС Linux (systemd, cron)"}, nil, "go-primary")
	letter := "Про другой проект."
	profile := "Linux 17+ лет, systemd (production), /etc/crontab."
	f := Evaluate(reqs, profile, letter, "вакансия")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcProfile {
		t.Fatalf("покрытие должно быть из профиля: %+v", f.Covered)
	}
	if !strings.Contains(f.Covered[0].Note, "cron") {
		t.Errorf("нота должна называть недостающий токен: %q", f.Covered[0].Note)
	}
}

// Меньше половины токенов — покрытие нет: письмо с одним «systemd» из
// четырёх токенов требование не закрывает, идёт в missing (один — caveats).
func TestEvaluateMinorityTokensStillMissing(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Linux, systemd, cron и journald"}, nil, "go-primary")
	letter := "Стек: Linux, Go, Kafka."
	f := Evaluate(reqs, "", letter, "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу caveats: 1 токен из 4 — не покрытие, но это один missing", f.Verdict)
	}
	if len(f.Missing) != 1 {
		t.Errorf("требование должно быть в missing: %+v", f)
	}
}

// Концепт «многопоточность и жизненный цикл»: требование без токенов
// закрывается по признакам письма (горутины, ProcessManager, shutdown).
func TestEvaluateConcurrencyConceptFromLetter(t *testing.T) {
	reqs := mustReqs([]string{"Мультипоточное программирование, диспетчеризация процессов"}, nil, "go-primary")
	letter := "Гео-маппинг доменов: горутины, каналы; ProcessManager в Stable ID — диспетчеризация к воркерам, graceful shutdown."
	f := Evaluate(reqs, "", letter, "вакансия")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("концепт должен закрыться письмом по признакам: %+v", f)
	}
	if !strings.Contains(f.Covered[0].Note, "многопоточность") {
		t.Errorf("нота должна называть концепт: %q", f.Covered[0].Note)
	}
}

// Концепт SOLID: письмом не назван, но профиль содержит факты
// (SOLID/GRASP, паттерны) — закрытие из профиля.
func TestEvaluateSolidConceptFromProfile(t *testing.T) {
	reqs := mustReqs([]string{"ООП, SOLID, паттерны проектирования"}, nil, "go-primary")
	letter := "Стек: Go, Kafka, ClickHouse."
	profile := "SOLID и GRASP — Task Flow; паттерны в production: ProcessManager + Strategy, Hexagonal, DDD."
	f := Evaluate(reqs, profile, letter, "вакансия")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcProfile {
		t.Fatalf("концепт должен закрыться профилем по признакам: %+v", f)
	}
	if !adviceHas(f.Advice, "впиши в письмо") {
		t.Errorf("нужен совет «впиши в письмо»: %+v", f.Advice)
	}
}

// adviceHas — проверка, что среди советов есть содержащий подстроку.
// adviceHas — проверка, что среди советов есть содержащий подстроку.
func adviceHas(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
