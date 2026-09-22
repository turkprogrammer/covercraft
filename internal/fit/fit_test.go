package fit

import (
	"bytes"
	"log/slog"
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
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profileGo, "Стек: Go, Kafka", "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q: одно unknown не роняет вердикт", f.Verdict, Caveats)
	}
	if len(f.Caveats) != 1 || f.Caveats[0].Source != SrcUnknown {
		t.Errorf("unknown должен попасть в caveats: %+v", f.Caveats)
	}
}

// Регресс живого кейса (Evolution CMS, повторная генерация): 5 must-have
// закрыты, 0 missing, 3 unknown («проверь вручную») — вердикт обязан быть
// Caveats, не Skip: unknown = «нет данных», а не «нет опыта» (комментарий
// finish, fit.go:755-763). Массовое unknown без единого missing не может
// давать «не откликаться».
func TestEvaluateMassUnknownWithoutMissingNotSkip(t *testing.T) {
	reqs := mustReqs([]string{
		"Опыт в финтех-домене",
		"Опыт работы в продуктовой компании",
		"Знание предметной области логистики",
	}, nil, "go-primary")
	letter := "Стек: Go, Kafka, ClickHouse"
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if len(f.Missing) != 0 {
		t.Fatalf("missing должен быть пуст, получено: %+v", f.Missing)
	}
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q: три unknown при нулевом missing — серая зона, не skip", f.Verdict, Caveats)
	}
}

// Два unknown — caveats: это «нет данных», а не «нет опыта». Unknown сам
// по себе вердикт до skip не роняет — только missing и roleMismatch.
func TestEvaluateTwoUnknownIsCaveats(t *testing.T) {
	reqs := mustReqs([]string{"Опыт в финтех-домене", "Понимание скоринга"}, nil, "go-primary")
	f := Evaluate(DefaultConcepts(), reqs, profileGo, "Стек: Go, Kafka", "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q: два unknown — данных мало, но не приговор", f.Verdict, Caveats)
	}
}

// Три unknown — это «нет данных», а не «нет опыта»: caveats, не skip.
// Skip по массовому unknown без единого missing противоречил спецификации
// finish (fit.go) и давал ложное «не откликаться» при закрытых must-have
// (живой регресс Evolution CMS: 5 закрыто цитатами, 0 missing, 3 unknown).
func TestEvaluateThreeUnknownIsCaveats(t *testing.T) {
	reqs := mustReqs([]string{"Опыт в финтех-домене", "Понимание скоринга", "Опыт банковских интеграций"}, nil, "go-primary")
	f := Evaluate(DefaultConcepts(), reqs, profileGo, "Стек: Go, Kafka", "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q: три unknown — серая зона, не skip", f.Verdict, Caveats)
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
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if f.Verdict != Apply || f.Score != 100 {
		t.Errorf("verdict = %q, score = %d: nice не должен штрафовать", f.Verdict, f.Score)
	}
}

// Мягкие требования не считаются пробелами вовсе.
func TestEvaluateSoftIgnored(t *testing.T) {
	reqs := mustReqs([]string{"Самоорганизованность и темп работы"}, nil, "go-primary")
	f := Evaluate(DefaultConcepts(), reqs, profileGo, "Стек: Go", "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if f.Verdict != Skip {
		t.Errorf("verdict = %q, хочу %q: PHP-primary для Go-кандидата", f.Verdict, Skip)
	}
}

// Синонимы: вакансия пишет K8s, профиль — Kubernetes.
func TestEvaluateSynonyms(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с K8s"}, nil, "go-primary")
	letter := "Эксплуатировал Kubernetes в проде."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if f.Verdict != Apply || f.Score != 100 {
		t.Errorf("verdict = %q score = %d: K8s и Kubernetes — одно и то же", f.Verdict, f.Score)
	}
}

// Разбор не удался (пустые списки) — вердикта нет, панель не рендерится.
func TestEvaluateEmptyRequirementsNoVerdict(t *testing.T) {
	f := Evaluate(DefaultConcepts(), Requirements{}, profileGo, "письмо", "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия архитектор")

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
// unknown. Это «нет данных», а не «нет опыта»: вердикт caveats с советом
// «проверь вручную», а не skip — незакрытых must-have (missing) нет.
func TestEvaluateArchitectVacancyEmptyLetter(t *testing.T) {
	profile := "Основной язык — Go. Backend: 10 лет."
	reqs := mustReqs([]string{
		"Опыт проектирования распределённых систем",
		"Опыт работы с высоконагруженными и критичными системами",
		"Понимание эксплуатации, мониторинга, отказоустойчивости и деградации сервисов",
	}, nil, "go-primary")
	f := Evaluate(DefaultConcepts(), reqs, profile, "Стек: Go, Kafka", "вакансия")
	if f.Verdict != Caveats {
		t.Errorf("verdict = %q, хочу %q: письмо без сигналов концептов — unknown, не missing", f.Verdict, Caveats)
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
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, "", letter, "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, "", letter, "вакансия")
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
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcProfile {
		t.Fatalf("концепт должен закрыться профилем по признакам: %+v", f)
	}
	if !adviceHas(f.Advice, "впиши в письмо") {
		t.Errorf("нужен совет «впиши в письмо»: %+v", f.Advice)
	}
}

// Живой кейс (платёжная вакансия): письмо честно называет пробел
// («С OpenTelemetry опыта нет, готов освоить»). Голая подстрока считала
// это закрытием в письме — теперь отрицание в предложении распознаётся:
// не covered, а unknown с нотой о честном пробеле.
func TestEvaluateNegatedFactNotCovered(t *testing.T) {
	reqs := mustReqs([]string{"OpenTelemetry для трейсинга"}, nil, "go-primary")
	letter := "Стек: Go, Kafka, ClickHouse.\nС OpenTelemetry опыта нет, готов освоить."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if len(f.Covered) != 0 {
		t.Errorf("отрицание («опыта нет») не должно считаться закрытием: %+v", f.Covered)
	}
	if len(f.Caveats) != 1 || f.Caveats[0].Source != SrcUnknown {
		t.Fatalf("честный пробел должен стать unknown-кавеатом: %+v", f.Caveats)
	}
	if !strings.Contains(f.Caveats[0].Note, "честно назван пробел") {
		t.Errorf("нота должна отличать честный пробел от молчаливого пропуска: %q", f.Caveats[0].Note)
	}
}

// Регресс: отрицание в соседнем предложении не роняет валидный факт.
func TestEvaluateNegationSentenceScoped(t *testing.T) {
	reqs := mustReqs([]string{"Опыт с Kafka и ClickHouse"}, nil, "go-primary")
	letter := "Не работал с Kubernetes.\nСтек: Go, Kafka, ClickHouse."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Errorf("факт в другом предложении должен закрывать требование письмом: %+v", f)
	}
}

// Регресс (живой прогон): профиль-ограничитель «OpenTelemetry: опыта
// интеграции НЕТ» не должен считаться фактом — иначе matcher советует
// вписать в письмо то, чего нет.
func TestEvaluateProfileLimiterNotFact(t *testing.T) {
	reqs := mustReqs([]string{"OpenTelemetry — трейсинг на всех уровнях"}, nil, "go-primary")
	profile := "СТЕК: Go, Kafka.\nОграничители:\n- OpenTelemetry: опыта интеграции НЕТ (честный пробел)."
	letter := "Стек: Go, Kafka.\nС OpenTelemetry опыта нет, готов освоить."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	for _, c := range f.Covered {
		if c.Source == SrcProfile {
			t.Errorf("ограничитель профиля не должен засчитываться фактом: %+v", f.Covered)
		}
	}
	if len(f.Caveats) != 1 || f.Caveats[0].Source != SrcUnknown {
		t.Fatalf("честный пробел (письмо + ограничитель профиля) должен стать unknown: %+v", f.Caveats)
	}
}

// Регресс (живой прогон): письмо честно отрицает outbox, а профиль
// содержит мост-факт с упоминанием outbox — приоритет у честного пробела
// письма: unknown, а не совет «впиши transactional в письмо».
func TestEvaluateLetterHonestGapBeatsProfile(t *testing.T) {
	reqs := mustReqs([]string{"Проектирование event-driven цепочек через transactional outbox"}, nil, "go-primary")
	profile := "ОБЩИЙ ПРОФИЛЬ:\n- Надёжная доставка событий (мост к outbox): буферизация, идемпотентный Upsert, at-least-once, event-driven паттерны."
	letter := "Kafka, event-driven архитектуры.\nTransactional outbox на PostgreSQL не использовал; близкий опыт — событийный журнал в БД с polling-потребителями, готов применить паттерн."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	for _, c := range f.Covered {
		if strings.Contains(c.Note, "впиши в письмо") {
			t.Errorf("письмо честно отрицает outbox — совет «впиши в письмо» недопустим: %+v", c)
		}
	}
	foundUnknown := false
	for _, c := range f.Caveats {
		if c.Source == SrcUnknown && strings.Contains(c.Note, "честно назван пробел") {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatalf("честный пробел письма должен победить профиль: %+v", f)
	}
}

// Logbroker — «Kafka-like» (формулировка вакансии): требование про
// Logbroker закрывается письмом, где назван Kafka + event-driven.
func TestEvaluateLogbrokerSynonym(t *testing.T) {
	reqs := mustReqs([]string{"Logbroker (Kafka-like) как event bus"}, nil, "go-primary")
	letter := "Kafka consumer groups, event-driven паттерны, at-least-once."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Errorf("Logbroker должен синонимично закрываться Kafka из письма: %+v", f)
	}
}

// Outbox-мост: transactional outbox в письме/профиле не назван, но есть
// честные якоря — событийный журнал, буферизация, идемпотентность.
// Bridge (0.5, оговорка), а не «не закрыто ничем».
func TestEvaluateOutboxBridge(t *testing.T) {
	reqs := mustReqs([]string{"Проектирование event-driven цепочек через transactional outbox на PostgreSQL"}, nil, "go-primary")
	letter := "Kafka producer с буферизацией при недоступности брокера, идемпотентность через Upsert, at-least-once."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if len(f.Caveats) != 1 || f.Caveats[0].Source != SrcBridge {
		t.Fatalf("outbox должен закрываться мостом с оговоркой: %+v", f.Caveats)
	}
	if !strings.Contains(f.Caveats[0].Note, "идемпотентн") {
		t.Errorf("нота моста должна называть якоря: %q", f.Caveats[0].Note)
	}
}

// Observability-мост: в письме Prometheus + Grafana — требование
// «выстраивание observability» закрывается мостом, не missing.
func TestEvaluateObservabilityBridge(t *testing.T) {
	reqs := mustReqs([]string{"Выстраивание observability"}, nil, "go-primary")
	letter := "Prometheus + Grafana: 3 дашборда, 23 панели, алертинг."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if len(f.Caveats) != 1 || f.Caveats[0].Source != SrcBridge {
		t.Errorf("observability должен закрываться мостом: %+v", f.Caveats)
	}
}

// Честные пробелы, названные в письме, не должны ронять вердикт в skip
// через порог unknown (иначе скрытие пробелов даёт лучший вердикт, чем
// честность). Три честных пробела + закрытое ядро → caveats, не skip.
func TestEvaluateHonestGapsDoNotSkip(t *testing.T) {
	reqs := Requirements{Role: "go-primary"}
	for _, m := range []string{
		"Go — основной язык для новых сервисов",
		"OpenTelemetry — трейсинг на всех уровнях",
		"Проектирование через transactional outbox",
		"Kubernetes — эксплуатация",
	} {
		reqs.MustHave = append(reqs.MustHave, Requirement{Text: m, Kind: "must", Category: "stack"})
	}
	profile := "ОБЩИЙ ПРОФИЛЬ: Go — основной язык; сервисы на Go."
	letter := "Go — мой основной язык, сервисы в проде.\n" +
		"С OpenTelemetry опыта нет, готов освоить.\n" +
		"Transactional outbox не использовал; близкий опыт — событийный журнал в БД, готов применить паттерн.\n" +
		"С Kubernetes опыта эксплуатации нет, понимаю архитектуру, готов освоить."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Verdict == Skip {
		t.Fatalf("честное письмо не должно получать skip: %+v", f)
	}
	if f.Verdict != Caveats {
		t.Errorf("ожидался caveats: %s", f.Verdict)
	}
	for _, a := range f.Advice {
		if strings.Contains(a, "проверь вручную") && strings.Contains(a, "OpenTelemetry") {
			t.Errorf("честный пробел не должен попадать в свод «проверь вручную»: %q", a)
		}
	}
}

// Живой регресс платёжной вакансии: буллет-пробел перечисляет через
// запятую и пробел, и позитив. «opыта нет» в клаузе OTel не должно
// накрывать клаузу с observability — иначе закрытый факт падал в unknown.
func TestEvaluateNegationClauseScoped(t *testing.T) {
	reqs := mustReqs([]string{"Выстраивание observability"}, nil, "go-primary")
	letter := "- **OpenTelemetry:** опыта нет, observability — SQL-Top, Prometheus + Grafana, готов освоить."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("observability закрыт в письме, отрицание OTel из соседней клаузы не при чём: %+v", f)
	}
	// А сам OTel в той же строке остаётся честным пробелом.
	reqs2 := mustReqs([]string{"OpenTelemetry — трейсинг на всех уровнях"}, nil, "go-primary")
	f2 := Evaluate(DefaultConcepts(), reqs2, profileGo, letter, "вакансия")
	if len(f2.Caveats) != 1 || f2.Caveats[0].Source != SrcUnknown || !isHonestGap(f2.Caveats[0].Note) {
		t.Errorf("OTel должен остаться честным пробелом: %+v", f2.Caveats)
	}
}

// Хвостовая форма отрицания: маркер в отдельной клаузе после токена
// («OpenTelemetry, опыта нет») относится к предыдущей клаузе.
func TestEvaluateTrailingNegation(t *testing.T) {
	reqs := mustReqs([]string{"OpenTelemetry — трейсинг"}, nil, "go-primary")
	letter := "Стек: Go, Kafka.\nOpenTelemetry, опыта нет, готов освоить."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	for _, c := range f.Covered {
		if strings.Contains(c.Text, "OpenTelemetry") {
			t.Errorf("хвостовое отрицание не распознано, факт засчитан: %+v", c)
		}
	}
	if len(f.Caveats) != 1 || !isHonestGap(f.Caveats[0].Note) {
		t.Errorf("хвостовое отрицание должно дать честный пробел: %+v", f.Caveats)
	}
}

// Письмо закрывает большинство токенов, профиль — все: приоритет у письма
// (вес 1.0, совет «добавь недостающее»), а не совет «впиши в письмо».
func TestEvaluateLetterMajorityBeatsProfileAll(t *testing.T) {
	reqs := mustReqs([]string{"Дизайн доменной модели в стиле DDD + Hexagonal Architecture + CQRS"}, nil, "go-primary")
	letter := "- **DDD + Hexagonal:** E-commerce-Lite (Symfony 7.2, Hexagonal Architecture, DDD — 8 entities, 6 ports), Fraud Engine (domain/application/adapters)."
	profile := "Профиль: DDD, Hexagonal Architecture, CQRS, доменные модели."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("письмо закрывает большинство токенов — источник должен быть письмом: %+v", f.Covered)
	}
	if !strings.Contains(f.Covered[0].Note, "cqrs") {
		t.Errorf("нота должна называть недостающий токен: %q", f.Covered[0].Note)
	}
}

// «Гарантии консистентности и идемпотентности» — латиницы в требовании нет,
// закрывается концепт-признаками письма, а не уходит в unknown.
func TestEvaluateConsistencyConcept(t *testing.T) {
	reqs := mustReqs([]string{"Гарантии консистентности и идемпотентности"}, nil, "go-primary")
	letter := "Stable ID — at-least-once + идемпотентность через ClickHouse Upsert; Fraud Engine — idempotency keys, fail-closed."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if len(f.Covered) != 1 || f.Covered[0].Source != SrcLetter {
		t.Fatalf("требование закрыто признаками письма: %+v", f)
	}
	if !strings.Contains(f.Covered[0].Note, "гарантии консистентности") {
		t.Errorf("нота должна называть концепт: %q", f.Covered[0].Note)
	}
}

// Живой регресс платёжной вакансии: ограничитель профиля перевешивает
// метку моста. Профиль содержит «(мост к outbox)» в блоке фактов и живой
// ограничитель «Transactional outbox: НЕ использовал» — matcher не должен
// советовать «впиши в письмо, закроется полностью» по неприменённому
// паттерну; честный путь — мост с оговоркой.
func TestEvaluateProfileLimiterBeatsBridgeLabel(t *testing.T) {
	reqs := mustReqs([]string{"Проектирование event-driven цепочек через transactional outbox на PostgreSQL"}, nil, "go-primary")
	profile := "ОБЩИЙ ПРОФИЛЬ:\n- Надёжная доставка событий (мост к outbox): буферизация, идемпотентный Upsert, at-least-once, event-driven паттерны, PostgreSQL.\nОГРАНИЧИТЕЛИ:\n- Transactional outbox на PostgreSQL: не использовал."
	letter := "Event-driven архитектура: Kafka consumer groups, буферизация при недоступности брокера, идемпотентность, at-least-once; PostgreSQL."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	for _, c := range f.Covered {
		if c.Source == SrcProfile {
			t.Errorf("ограничитель «не использовал» должен перевесить метку моста: %+v", f.Covered)
		}
	}
	for _, a := range f.Advice {
		if strings.Contains(a, "впиши в письмо") {
			t.Errorf("совет вписать неприменённый паттерн недопустим: %q", a)
		}
	}
	if len(f.Caveats) != 1 || f.Caveats[0].Source != SrcBridge {
		t.Fatalf("ожидался мост с оговоркой: %+v", f)
	}
}

// adviceHas — проверка, что среди советов есть содержащий подстроку.
func adviceHas(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Живой регресс: честное отрицание «С платёжными процессингами не работал»
// в концептном требовании (вместо названия технологии — описание области опыта)
// должно давать пробел в Caveats, а не покрытие в Covered. До исправления
// conceptHit не проверял отрицания в клаузах сигналов, находил «интеграция»,
// «транзакции», «платежи» и засчитывал как закрытие.
func TestEvaluateConceptNegationRespected(t *testing.T) {
	reqs := mustReqs([]string{"Опыт интеграции с платёжными процессингами"}, nil, "go-primary")
	letter := "Интеграции строил через REST API. С платёжными процессингами не работал, отсутствует опыт транзакций, готов освоить."
	f := Evaluate(DefaultConcepts(), reqs, profileGo, letter, "вакансия")
	if len(f.Covered) > 0 {
		t.Errorf("концептное требование с честным отрицанием не должно попасть в Covered: %+v", f.Covered)
	}
	if len(f.Caveats) == 0 {
		t.Fatalf("честный пробел должен дать запись в Caveats: %+v", f)
	}
	if f.Caveats[0].Source != SrcUnknown || !isHonestGap(f.Caveats[0].Note) {
		t.Errorf("кавеат должен быть распознан как честный пробел: %+v", f.Caveats[0])
	}
	for _, a := range f.Advice {
		if strings.Contains(a, "проверь вручную") {
			t.Errorf("честный пробел не должен попадать в свод «проверь вручную»: %q", a)
		}
	}
}

// TestSynonymsLimitsLimiting — регресс: «rate limits» в требовании и
// «rate limiting» в письме должны закрываться через синонимы. Без
// синонима токен-матчинг промахивается (limits ≠ limiting).
func TestSynonymsLimitsLimiting(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Опыт работы с rate limits, пагинацией и курсорами внешних API", Kind: "must"},
		},
	}
	letter := "Реализовал rate limiting (token bucket) + gobreaker, исчерпывающие ретраи."
	profile := "Опыт: rate limiter, троттлинг на уровне шлюза."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "rate limits")
	if f.Score < 20 {
		t.Errorf("score = %d, ожидаю покрытие через синонимы limits/limiting", f.Score)
	}
	// Проверяем, что токен limits нашёл «limiting» и «rate limiting» в письме.
	if !findText("limits", letter) {
		t.Error("findText(\"limits\", letter) == false — синоним не сработал")
	}
	if !findText("limits", profile) {
		t.Error("findText(\"limits\", profile) == false — синоним не сработал")
	}
}

// TestRoleMismatchBilingualProfile — регресс: билингвальный профиль
// (Go + PHP с продакшн-опытом) на PHP-вакансии не должен давать roleMismatch.
// Раньше phpCand требовал «php-разработчик» или «основн...php» — узко,
// и кандидат с «PHP — PRODUCTION (17 ЛЕТ ОПЫТА)» ловил ложное срабатывание.
func TestRoleMismatchBilingualProfile(t *testing.T) {
	reqs := Requirements{Role: "php-primary", MustHave: []Requirement{{Text: "PHP в проде"}}}
	profile := "Go (3 года), PHP (2005+). PHP — PRODUCTION (17 ЛЕТ ОПЫТА), production-инциденты."
	if roleMismatch(reqs, profile) {
		t.Error("roleMismatch сработал на билингвальном профиле с 17-летним PHP в проде")
	}
	// Контр-кейс: чистый Go-разработчик на PHP-вакансии — должно сработать.
	profileGoOnly := "Go-разработчик, 3 года, Kafka и gRPC."
	if !roleMismatch(reqs, profileGoOnly) {
		t.Error("roleMismatch НЕ сработал на чистом Go-разработчике на PHP-вакансии")
	}
	// Контр-кейс: только PHP в профиле — не должно сработать (нет goCand).
	profilePhpOnly := "PHP-разработчик, 17 лет, Symfony, Laravel."
	if roleMismatch(reqs, profilePhpOnly) {
		t.Error("roleMismatch сработал на чистом PHP-разработчике на PHP-вакансии")
	}
}

// TestRestAPITokenMatch — регресс: требование «REST API» должно матчиться
// по токенам rest/api через профиль и письмо. Раньше rest/api были в
// stopwords, токены отбрасывались, и требование уходило в concept-путь,
// где нет ни одного подходящего концепта → «нет признаков».
func TestRestAPITokenMatch(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Опыт проектирования и разработки REST API", Kind: "must"},
		},
	}
	letter := "Проектирование публичных API сервиса и механизмы интеграции сторонних сервисов."
	profile := "Fraud Engine: API — REST (JSON), X-API-Key аутентификация, endpoints: POST /v1/score."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Score < 20 {
		t.Errorf("score = %d, REST API должен закрываться по токенам из профиля", f.Score)
	}
	// Токены rest/api не должны поглощаться stopwords.
	toks := reqTokens("Опыт разработки REST API")
	for _, want := range []string{"rest", "api"} {
		found := false
		for _, got := range toks {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("reqTokens не содержит %q: %v", want, toks)
		}
	}
}

// TestTestingConceptMatch — модульное тестирование должно закрываться
// концептом «тестирование и качество кода». Раньше требования про
// тестирование не триггерили ни один концепт → «нет данных».
func TestTestingConceptMatch(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Знание технологий и методик проведения модульного тестирования", Kind: "must"},
		},
	}
	letter := "155+ тестов (unit, интеграционные, E2E) в Go-проектах."
	profile := "SQL-Top: 44 unit-теста (hexagonal); Task Flow: dockertest; E-commerce-Lite: TDD."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Score < 20 {
		t.Errorf("score = %d, тестирование должно закрываться концептом", f.Score)
	}
	// Концепт должен триггериться на слове «тестирования» в требовании.
	if name, _ := conceptHit(DefaultConcepts(), reqs.MustHave[0].Text, profile, 2); name == "" {
		t.Error("conceptHit не нашёл концепт для «модульного тестирования»")
	}
}

// TestLoadProfileEmptyDirLogs — пустой каталог context должен вернуть
// пустую строку И залогировать предупреждение. Иначе пустой профиль
// тихо ломает матчинг: концепты не получают сигналов из профиля.
func TestLoadProfileEmptyDirLogs(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	got := LoadProfile(dir)
	if got != "" {
		t.Errorf("LoadProfile(%q) = %q, хочу пустую строку", dir, got)
	}
	if !strings.Contains(buf.String(), "profile") {
		t.Errorf("нет предупреждения о пустом профиле; вывод: %q", buf.String())
	}
}

// TestEvaluateBusinessCriticalConcept — регресс: составное требование
// «высоконагруженных, распределённых и отказоустойчивых систем ...
// уровня business critical» НЕ должно уходить в token-путь из-за латинских
// слов business/critical. Это концептное требование («распределённые
// системы»), а «business critical» — не технология: токен-матчинг по
// нему всегда промахивается.
func TestEvaluateBusinessCriticalConcept(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Опыт проектирования и разработки высоконагруженных, распределённых и отказоустойчивых систем реального времени уровня business critical", Kind: "must"},
		},
	}
	if len(reqTokens(reqs.MustHave[0].Text)) != 0 {
		t.Fatalf("reqTokens должен быть пустым (business/critical — не технологии), получил: %v", reqTokens(reqs.MustHave[0].Text))
	}
	letter := "Stable ID: Kafka, 10 000 RPS, event-driven, at-least-once, 20+ воркеров."
	profile := "Kafka consumer groups, event-driven, партиционирование, отказоустойчивость, 10 000 RPS, P99 < 46ms."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Score < 20 {
		t.Errorf("score = %d, концепт «распределённые системы» должен закрыть требование", f.Score)
	}
}

// TestEvaluateSystemIntegrationConcept — «Понимание принципов системной
// интеграции» должно закрываться концептом. В профиле есть gRPC
// (межсервисное взаимодействие), REST API, event-driven (Kafka),
// микросервисы — но концепта про интеграцию не было → unknown.
func TestEvaluateSystemIntegrationConcept(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Понимание современных принципов и технологий системной интеграции", Kind: "must"},
		},
	}
	letter := "REST API и интеграции: проектирование публичных API, gRPC в микросервисах банка."
	profile := "gRPC (межсервисное взаимодействие), REST API, event-driven (Kafka), микросервисы."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Score < 20 {
		t.Errorf("score = %d, системная интеграция должна закрываться концептом", f.Score)
	}
	if name, _ := conceptHit(DefaultConcepts(), reqs.MustHave[0].Text, profile, 2); name == "" {
		t.Error("conceptHit не нашёл концепт для «системной интеграции»")
	}
}

// TestEvaluateAgileConcept — «по гибким методологиям» без слова «Agile»
// должно закрываться концептом процессов разработки. Вакансия может
// сформулировать требование по-русски, и токен-путь тут бессилен.
func TestEvaluateAgileConcept(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Опыт работы в продуктовой команде по гибким методологиям", Kind: "must"},
		},
	}
	letter := "Работал в продуктовой команде."
	profile := "Agile (Scrum и Kanban): планирование спринтов, стендапы, ретроспективы, story points."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if len(f.Covered) == 0 {
		t.Errorf("score = %d, концепт Agile должен закрыть требование", f.Score)
	}
	if name, _ := conceptHit(DefaultConcepts(), reqs.MustHave[0].Text, profile, 2); name == "" {
		t.Error("conceptHit не нашёл концепт для «гибких методологий»")
	}
}

// TestEvaluateBrokerAlternatives — «(Kafka, RabbitMQ)» это OR-список:
// факта Kafka достаточно, честный пробел по RabbitMQ не роняет требование
// в unknown. Семантика совпадает с правилом «слэш = ИЛИ» в промпте письма.
func TestEvaluateBrokerAlternatives(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Опыт работы с брокерами очередей (Kafka, RabbitMQ)", Kind: "must"},
		},
	}
	letter := "Kafka — основной брокер в Stable ID. RabbitMQ — опыта нет, готов освоить."
	profile := "Kafka consumer groups, event-driven, партиционирование."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if len(f.Covered) == 0 {
		t.Errorf("score = %d, Kafka должна закрывать OR-список (Kafka, RabbitMQ)", f.Score)
	}
}

// TestEvaluateNonAlternativeStillCaveat — страховка: OR-правило не должно
// распространяться на обычные перечисления без скобок/слэша/«или». «Kafka,
// PostgreSQL» — оба нужны, отсутствие PostgreSQL остаётся неполным.
func TestEvaluateNonAlternativeStillCaveat(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Опыт работы с Kafka, PostgreSQL", Kind: "must"},
		},
	}
	letter := "Kafka: продовый опыт. PostgreSQL — опыта нет."
	profile := "Kafka consumer groups, event-driven."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if len(f.Covered) > 0 {
		t.Errorf("требование не должно закрываться по Kafka: %+v", f.Covered)
	}
}

// TestEvaluateBashProfileBridge — Bash есть в профиле (строка «Языки» и
// раздел BASH / DATA PIPELINE — GeoMapping), но мост «bash-автоматизация →
// готов освоить Python/Airflow» ложно помечал его отклонённым в профиле.
// Требование про Bash обязано закрываться.
func TestEvaluateBashProfileBridge(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Опыт работы с Bash", Kind: "must"},
		},
	}
	letter := "Стек: Go, Docker, Kafka."
	profile := "Языки: Go, PHP, Bash, SQL.\n" +
		"## BASH / DATA PIPELINE — GeoMapping\n" +
		"Bash-based data pipeline: auto_sync_geodata.sh, rebuild_fixed_from_mapping.sh.\n" +
		"Мост: bash-автоматизация → готов освоить Python/Airflow."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if len(f.Covered) == 0 {
		t.Fatalf("Bash должен закрываться профилем: caveats=%+v missing=%+v", f.Caveats, f.Missing)
	}
}

// TestDeclinedInProfileForwardMarker — «готов освоить» отрицает только то,
// что стоит ПОСЛЕ него: в мосте «bash-автоматизация → готов освоить
// Python/Airflow» пробел — Python/Airflow, а Bash остаётся фактом.
func TestDeclinedInProfileForwardMarker(t *testing.T) {
	profile := "Мост: bash-автоматизация → готов освоить Python/Airflow."
	if declinedInProfile("bash", profile) {
		t.Error("bash стоит до «готов освоить» — не должен быть declined")
	}
	if !declinedInProfile("python", profile) {
		t.Error("python стоит после «готов освоить» — должен быть declined")
	}
	if !declinedInProfile("airflow", profile) {
		t.Error("airflow стоит после «готов освоить» — должен быть declined")
	}
	if !declinedInProfile("outbox", "Transactional outbox на PostgreSQL: не использовал.") {
		t.Error("backward-маркер «не использовал» обязан отклонять outbox")
	}
}

// TestSQLTokenUnstopped — «Уверенный SQL» в требовании должен матчится
// по профилю («БД и SQL: PostgreSQL…»): sql — реальный навык в вакансиях,
// а не эпитет, поэтому выведен из стопвордов.
func TestSQLTokenUnstopped(t *testing.T) {
	toks := reqTokens("Уверенный SQL, оптимизация запросов")
	found := false
	for _, tok := range toks {
		if tok == "sql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reqTokens не содержит sql: %v", toks)
	}
	reqs := Requirements{
		Role: "data",
		MustHave: []Requirement{
			{Text: "Уверенный SQL, оптимизация запросов, работа с большими объёмами", Kind: "must"},
		},
	}
	profile := "БД и SQL: PostgreSQL, оптимизация запросов, pg_stat_statements."
	letter := "Оптимизация запросов PostgreSQL: индексы, планы."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Covered == nil || len(f.Covered) == 0 {
		t.Errorf("SQL-требование должно закрываться профилем, verdict=%s caveats=%d missing=%d", f.Verdict, len(f.Caveats), len(f.Missing))
	}
}

// TestAlertingIncidentTrigger — кириллическое требование «алертинг и
// разбор инцидентов» должно цеплять концепт «эксплуатация и
// observability» по новым триггерам алерт/инцидент и закрываться
// профилем (алертинг по SLO) без ухода в «проверь вручную».
func TestAlertingIncidentTrigger(t *testing.T) {
	reqs := Requirements{
		Role: "devops",
		MustHave: []Requirement{
			{Text: "Опыт настройки алертинга и разбора инцидентов", Kind: "must"},
		},
	}
	profile := "Мониторинг: Prometheus + Grafana, алертинг (P99 > 100ms, error rate > 1%)."
	letter := "Развернул наблюдаемость: дашборды Grafana и алерты по error rate."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Covered == nil || len(f.Covered) == 0 {
		t.Errorf("требование про алертинг должно закрываться концептом, verdict=%s caveats=%d", f.Verdict, len(f.Caveats))
	}
	for _, c := range f.Caveats {
		if strings.Contains(c.Text, "алертинг") && c.Source == SrcUnknown {
			t.Errorf("алертинг не должен попадать в unknown: %+v", c)
		}
	}
}

// TestFitFixableCaveats — FitFixableCaveats возвращает только те требования,
// чья note содержит "впиши" или "не упомянуты". Soft-требования (check manually)
// и честные пробелы отсекаются. Проверяет сканирование Caveats ∪ Covered.
func TestFitFixableCaveats(t *testing.T) {
	f := Fit{
		Covered: []Req{
			{Text: "vet", Source: SrcProfile, Note: "в профиле есть факт по большинству токенов (не упомянуты: vet) — впиши в письмо, закроется полностью"},
			{Text: "Git", Source: SrcProfile, Note: "в профиле есть факт, но в письмо не попал — впиши в письмо, закроется полностью"},
			{Text: "5+ лет", Source: SrcUnknown, Note: "в письме и профиле нет достаточных признаков по этому требованию — проверь вручную"},
			{Text: "Elasticsearch", Source: SrcUnknown, Note: "честный пробел"},
		},
		Caveats: []Req{
			{Text: "bridge-test", Source: SrcBridge, Note: "мост: опыт → Elasticsearch"},
		},
	}
	fixable := FitFixableCaveats(f)
	if len(fixable) != 2 {
		t.Fatalf("хочу 2 fixable, got %d: %+v", len(fixable), fixable)
	}
	for _, c := range fixable {
		if c.Text == "5+ лет" || c.Text == "Elasticsearch" || c.Text == "bridge-test" {
			t.Errorf("не должен попасть в fixable: %s src=%s", c.Text, c.Source)
		}
	}
}

// TestFitFixableFromEvaluate — интеграционный тест: профиль содержит факт Git,
// письмо его не содержит → fit.Evaluate должен вернуть Covered с нотой
// «впиши в письмо», и FitFixableCaveats должен его поймать.
// Регресс-тест для F1: если addMust перенесёт факт из Covered в Caveats,
// этот тест упадёт — защита от рефакторинга coverage-корзинок.
func TestFitFixableFromEvaluate(t *testing.T) {
	reqs := Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			{Text: "Работа с Git", Kind: "must"},
		},
	}
	// Письмо БЕЗ токена Git — факт есть в профиле, но не в письме.
	letter := "Стек: Go, PHP, ML, Kafka, Redis, PostgreSQL, MySQL, Docker."
	profile := "Git: уверенно, 17 лет; PHP production; Go highload."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Verdict == "" {
		t.Fatalf("verdict должен быть не пустым: %+v", f)
	}
	fixable := FitFixableCaveats(f)
	if len(fixable) == 0 {
		t.Fatalf("FitFixableCaveats должен найти хотя бы один fixable (Git в профиле, нет в письме); covered=%+v caveats=%+v", f.Covered, f.Caveats)
	}
	// Убедиться что Git действительно в fixable
	found := false
	for _, c := range fixable {
		if strings.Contains(c.Text, "Git") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("в fixable не найден Git; fixable=%+v", fixable)
	}
}

// T1: ограничитель в одной клаузе не отменяет факт профиля в других клаузах.
// Регресс 2026-09-19: «Опыт работы с SQL БД (Postgres)» уходил в missing
// при живом факте «PostgreSQL (глубокое знание)».
func TestProfileLimiterDoesNotLeakToOtherTokens(t *testing.T) {
	reqs := mustReqs([]string{"Опыт работы с SQL БД (Postgres)"}, nil, "go-primary")
	profile := "## Навыки\n- **БД и SQL:** PostgreSQL (глубокое знание), MySQL, ClickHouse.\n" +
		"### PostgreSQL (Основной опыт)\n- Covering Index — PostgreSQL.\n" +
		"## Ограничители\n- **Transactional outbox на PostgreSQL:** НЕ использовал.\n"
	f := Evaluate(DefaultConcepts(), reqs, profile, "", "вакансия")
	if len(f.Missing) > 0 {
		t.Errorf("postgres не должен быть missing: %+v", f.Missing)
	}
	found := false
	for _, c := range append(f.Covered, f.Caveats...) {
		if c.Source == SrcProfile {
			found = true
		}
	}
	if !found {
		t.Errorf("ожидался факт из профиля: covered=%+v caveats=%+v", f.Covered, f.Caveats)
	}
}

// T2: аннотация «НЕ заявлять как outbox» — ограничитель, а не факт
// (реальная строка профиля разбивается запятой на отдельные клаузы).
func TestProfileAnnotationOutboxStillDeclines(t *testing.T) {
	profile := "## Ограничители\n- **Transactional outbox на PostgreSQL:** НЕ использовал. " +
		"Близкий опыт (мост с оговоркой, НЕ заявлять как outbox): событийный журнал в БД, идемпотентный Upsert.\n"
	if !declinedInProfile("outbox", profile) {
		t.Error("outbox назван ограничителем — обязан остаться declined")
	}
	reqs := mustReqs([]string{"Проектирование event-driven цепочек через transactional outbox"}, nil, "go-primary")
	f := Evaluate(DefaultConcepts(), reqs, profile, "", "вакансия")
	for _, c := range append(f.Covered, f.Caveats...) {
		if strings.Contains(c.Note, "впиши в письмо") {
			t.Errorf("совет вписать неприменённый паттерн: %+v", c)
		}
	}
}

// T3: хвостовой маркер «X, опыта нет» остаётся отрицанием (lookahead).
func TestDeclinedInProfileTailMarkerPreserved(t *testing.T) {
	if !declinedInProfile("opentelemetry", "Ограничители:\n- OpenTelemetry, опыта нет") {
		t.Error("хвостовой маркер должен отклонять токен")
	}
}

// T4: метка моста («(мост к outbox)») — не факт владения технологией.
func TestBridgeLabelIsNotFact(t *testing.T) {
	profile := "ФАКТЫ:\n- Надёжная доставка (мост к outbox): буферизация, идемпотентный Upsert.\n" +
		"ОГРАНИЧИТЕЛИ:\n- Transactional outbox: не использовал.\n"
	if !declinedInProfile("outbox", profile) {
		t.Error("метка моста не должна перебивать ограничитель")
	}
}

// T5: честные пробелы профиля («НЕ Python», «готов освоить Airflow»)
// не превращаются в факты.
func TestHonestGapsStayDeclined(t *testing.T) {
	profile := "- **Границы ML:** ML-опыт — Go, bash, НЕ Python\n" +
		"- Перенос принципов: мост «bash-автоматизация → готов освоить Python/Airflow»\n"
	for _, tok := range []string{"python", "airflow"} {
		if !declinedInProfile(tok, profile) {
			t.Errorf("%s назван пробелом — обязан остаться declined", tok)
		}
	}
	reqs := mustReqs([]string{"Опыт работы с Python"}, nil, "go-primary")
	f := Evaluate(DefaultConcepts(), reqs, profile, "", "вакансия")
	for _, c := range append(f.Covered, f.Caveats...) {
		if strings.Contains(c.Note, "впиши в письмо") {
			t.Errorf("совет вписать Python при честном пробеле: %+v", c)
		}
	}
}

// T6: ограничитель под-клейма не отменяет сам токен, если профиль называет
// его фактом в других клаузах (Kafka — транспорт заявлен, «гарантия
// порядка» — нет).
func TestFactClauseWinsOverSubClaimLimiter(t *testing.T) {
	profile := "ФАКТЫ:\n- Kafka consumer groups, MinBytes 10KB, at-least-once.\n" +
		"ОГРАНИЧИТЕЛИ:\n- **Гарантия порядка в Kafka НЕ заявлять:** только at-least-once.\n"
	if declinedInProfile("kafka", profile) {
		t.Error("kafka назван фактом в отдельной клаузе — declined недопустим")
	}
}

// T7: veto гибридного LLM-пути (map.go) видит аннотацию-ограничитель.
func TestTokensDeclinedSeesAnnotation(t *testing.T) {
	profile := "## Ограничители\n- **Transactional outbox:** НЕ использовал. Близкий опыт (мост, НЕ заявлять как outbox): журнал в БД.\n"
	if !tokensDeclinedInProfile([]string{"outbox"}, profile) {
		t.Error("модельная запись source=profile по outbox обязана быть отклонена")
	}
}

// Живой регресс PHP-вакансии: билингвальный профиль (Go + PHP 17 лет)
// на PHP-вакансии не даёт ложный roleMismatch. Профиль без слова «PHP»
// в пределах 60 символов от маркера опыта — самый частый паттерн
// «Основные языки: Go (3 года), PHP (2005+)» — не должен ронять
// вердикт до skip со счётом 0.
func TestRoleMismatchBilingualNoFalseSkip(t *testing.T) {
	// Обрезанный профиль: «PHP (2005+)» — маркер года, не «лет опыта».
	profile := "17 лет в бэкенд-архитектуре. Основные языки: Go (3 года), PHP (2005+), Bash, SQL. Фреймворки Laravel - production."
	reqs := Requirements{Role: "php-primary", MustHave: []Requirement{{Text: "PHP в проде"}}}
	if roleMismatch(reqs, profile) {
		t.Error("roleMismatch сработал на билингвальном профиле с «PHP (2005+)» — ложный skip")
	}
	// Полный профиль с «17 ЛЕТ ОПЫТА» рядом — тоже не должен.
	profileFull := "Go (3 года), PHP (2005+). PHP — PRODUCTION (17 ЛЕТ ОПЫТА), production-инциденты."
	if roleMismatch(reqs, profileFull) {
		t.Error("roleMismatch сработал на билингвальном профиле с «17 ЛЕТ ОПЫТА» рядом")
	}
	// Чистый Go-разработчик на PHP-вакансии — должно сработать.
	profileGoOnly := "Go-разработчик, 3 года, Kafka и gRPC. No PHP."
	if !roleMismatch(reqs, profileGoOnly) {
		t.Error("roleMismatch НЕ сработал на чистом Go-разработчике на PHP-вакансии")
	}
}

// Англоязычное требование с длинной формулировкой (MySQL - query
// optimization, schema design, migrations) закрывается русским письмом
// по кириллическим альт-синонимах: query→запрос, optimization→
// оптимизац, migrations→миграци. Majority 50/50 при total>=6.
func TestEnglishReqCyrillicLetter(t *testing.T) {
	profile := "MySQL, PostgreSQL. Оптимизация запросов, индексы, миграции."
	letter := "MySQL: оптимизация запросов, индексы B-tree/GIN, миграции (Task Flow), schema design через Doctrine."
	reqs := mustReqs([]string{"MySQL - query optimization, schema design, migrations"}, nil, "php-primary")
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Score < 50 {
		t.Errorf("score = %d, >= 50: query→запрос, optimization→оптимизац, migrations→миграци должны закрыться по кириллическим альтам", f.Score)
	}
}

// Нарративные слова git-практик (branching/strategies/workflows/
// conflict/resolution) — стоп-слова: не раздувают majority-порог.
// Факты «Git», «GitHub», «PR» в письме закрывают требование.
func TestGitReqNarrativeStopwords(t *testing.T) {
	profile := "GitHub PR workflows, Git (17 лет), ADR (10)."
	letter := "GitHub Pull Requests - уверенно владею Git (17 лет)."
	reqs := mustReqs([]string{"Git & GitHub - branching strategies, PR workflows, conflict resolution"}, nil, "go-primary")
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "вакансия")
	if f.Score < 50 {
		t.Errorf("score = %d, >= 50: git/github/pr закрывают требование, нарративные слова не должны раздувать порог", f.Score)
	}
}
