package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRules кладёт .audit-rules.md в tmp-каталог и возвращает путь к нему.
// Каждый тест, загружающий файл, обязан закончиться на дефолтах: Cleanup
// возвращает глобальные срезы к встроенным правилам, чтобы порядок тестов
// (-shuffle) не протекал чужими/пустыми правилами в соседние тесты.
func writeRules(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, RulesFilename), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resetDefaults)
	return dir
}

// candidateRulesDir — закреплённая фикстура полных правил реального кандидата
// (копия context/.audit-rules.md). Встроенные дефолты намеренно generic
// (кандидата-данных в коде быть не должно — DoD «grep-0»), поэтому
// кандидат-специфичные тесты грузят правила из этой фикстуры.
const candidateRulesDir = "testdata/candidate-rules"

// loadCandidateRules загружает полные правила реального кандидата в
// пакетное состояние для кандидат-специфичных тестов.
func loadCandidateRules(t *testing.T) {
	t.Helper()
	resetDefaults()
	if err := LoadProfileRules(candidateRulesDir); err != nil {
		t.Fatalf("не загрузилась фикстура правил кандидата: %v", err)
	}
}

// (а) файла нет → generic (пустые) правила, LoadProfileRules не ошибается.
// Generic-фейлбэк не должен навязывать чужому кандидату обязательства:
// правила кандидата живут только в .audit-rules.md (data-файл, универсальность).
func TestLoadProfileRulesNoFileKeepsDefaults(t *testing.T) {
	resetDefaults()
	dir := t.TempDir()
	if err := LoadProfileRules(dir); err != nil {
		t.Fatalf("нет файла — ожидается nil, получили %v", err)
	}
	if len(obligations) != 0 || len(metricRules) != 0 || len(stackForbidden) != 0 ||
		len(claimableTerms) != 0 || len(infraDomainTerms) != 0 || len(infraNeutralWords) != 0 {
		t.Errorf("нет файла — ожидается generic (пустые) списки, получены не пустые: данные кандидата протекли в код")
	}
	// Generic-фейлбэк: highload-вакансия не навязывает обязательства кандидата
	// (структурные v4-проверки «секции/строки/стек/контакты» срабатывать могут).
	res := Check("текст без фактов", "нужен highload инженер")
	joined := strings.Join(res.Warnings, "|")
	if strings.Contains(joined, "Stable ID") || strings.Contains(joined, "Fraud Engine") ||
		strings.Contains(joined, "PHPUnit") || strings.Contains(joined, "MariaDB") {
		t.Errorf("generic-фейлбэк навязал обязательства кандидата: %v", res.Warnings)
	}
}

// Шаблон нового профиля обязан давать рабочую защиту. Встроенных правил
// в коде нет, поэтому если docs/templates/.audit-rules.md снова станет
// полностью закомментированным, новый кандидат получит аудит без
// профильных проверок, и никто этого не заметит — молчание выглядит как
// штатная работа.
func TestTemplateGivesWorkingProtection(t *testing.T) {
	t.Cleanup(resetDefaults)
	const templatePath = "../../docs/templates/.audit-rules.md"
	body, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("шаблон недоступен: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, RulesFilename), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadProfileRules(dir); err != nil {
		t.Fatalf("шаблон не парсится: %v", err)
	}
	if len(claimableTerms) == 0 {
		t.Error("в шаблоне пуст ## claimableTerms — новый кандидат останется без защиты от выдуманных технологий")
	}
	if len(infraNeutralWords) == 0 {
		t.Error("в шаблоне пуст ## infraNeutralWords — CamelCase-guard будет ловить служебные слова")
	}
	// Базовый термин шаблона обязан реально ловиться: в профиле, где его
	// нет, письмо с ним должно получить замечание.
	w := CheckProfile("Веб-безопасность: XSS sanitization и OAuth в продакшене.",
		"Профиль: Go, Kafka, PostgreSQL, Caddy — без веб-фреймворков и авторизации.")
	if len(w.Warnings) == 0 {
		t.Errorf("claimable-термины шаблона не дают защиты: %v", w.Warnings)
	}
	// ...и молчать, когда профиль подтверждает факт.
	w2 := CheckProfile("Веб-безопасность: XSS sanitization.", "Профиль: XSS sanitization в CMS Blog.")
	for _, warn := range w2.Warnings {
		if strings.Contains(warn, "«XSS»") {
			t.Errorf("подтверждённый профилем термин XSS не должен ругаться: %v", w2.Warnings)
		}
	}
}

// (б) валидный файл → все секции заполнены из данных, дефолты сброшены.
func TestLoadProfileRulesValidFile(t *testing.T) {
	resetDefaults()
	body := `## obligations
need: (?i)(a/b|эксперимент)
has: (?i)a/?b
fact: A/B тестирование

## metricRules
metric: (?i)77\+
owners: (?i)synthetic engine
fact: 77+ тестов — проект Synthetic Engine

## stackForbidden
angular

## claimableTerms
OAuth2
JWT refresh

## infraDomainTerms
Nomad
Consul

## infraNeutralWords
nomad
consul
`
	dir := writeRules(t, body)
	if err := LoadProfileRules(dir); err != nil {
		t.Fatalf("валидный файл: %v", err)
	}
	if len(obligations) != 1 {
		t.Fatalf("obligations: want 1, got %d", len(obligations))
	}
	if obligations[0].fact != "A/B тестирование" || !obligations[0].need.MatchString("нужен эксперимент") {
		t.Errorf("obligation загружен неверно: %+v", obligations[0])
	}
	if len(metricRules) != 1 || !metricRules[0].metric.MatchString("77+") ||
		!metricRules[0].owners[0].MatchString("Synthetic Engine") ||
		metricRules[0].fact != "77+ тестов — проект Synthetic Engine" {
		t.Errorf("metricRule загружен неверно: %+v", metricRules[0])
	}
	if strings.Join(stackForbidden, ",") != "angular" {
		t.Errorf("stackForbidden: %v", stackForbidden)
	}
	ct := make([]string, len(claimableTerms))
	for i, c := range claimableTerms {
		ct[i] = c.pat
	}
	if strings.Join(ct, ",") != "OAuth2,JWT refresh" {
		t.Errorf("claimableTerms: %v", ct)
	}
	if strings.Join(infraDomainTerms, ",") != "Nomad,Consul" {
		t.Errorf("infraDomainTerms: %v", infraDomainTerms)
	}
	if !infraNeutralWords["nomad"] || !infraNeutralWords["consul"] || infraNeutralWords["prometheus"] {
		t.Errorf("infraNeutralWords: %v", infraNeutralWords)
	}
	// Дефолтные правила кандидата после загрузки не мешают чужому профилю.
	res := Check("Стек: Go, Angular, PHP.\n77+ тестов в Synthetic Engine.", "Senior Go разработчик")
	joined := strings.Join(res.Warnings, "|")
	if !strings.Contains(joined, "«angular» в строке стека") {
		t.Errorf("переопределённое stackForbidden не сработало: %v", res.Warnings)
	}
	if strings.Contains(joined, "symfony") || strings.Contains(joined, "yii2") {
		t.Errorf("дефолтный stackForbidden протёк в переопределённый профиль: %v", res.Warnings)
	}
}

// (в) битые строки → ошибка; срезы остаются дефолтными (падение парсера не ломает проверку).
func TestLoadProfileRulesBroken(t *testing.T) {
	resetDefaults()
	cases := map[string]string{
		"регэксп obligation битый": "## obligations\nneed: (((\nhas: ok\nfact: x\n",
		"регэксп metric битый":     "## metricRules\nmetric: (((\nowners: ok\nfact: x\n",
		"obligation без has":       "## obligations\nneed: (?i)x\nfact: y\n",
		"obligation без fact":      "## obligations\nneed: (?i)x\nhas: (?i)y\n",
		"метрика без owners":       "## metricRules\nmetric: (?i)x\nfact: y\n",
		"неизвестная секция":       "## wat\nfoo\n",
		"строка вне секции":        "some text\n## obligations\nneed: (?i)x\nhas: (?i)y\nfact: z\n",
		"ключ без значения":        "## obligations\nneed:\nhas: (?i)y\nfact: z\n",
		"дубли ключа в блоке":      "## obligations\nneed: (?i)x\nneed: (?i)z\nhas: (?i)y\nfact: w\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			// Подтесты гоняют глобальное состояние пакета и идут в случайном
			// порядке (map) — каждый стартует с дефолтов.
			resetDefaults()
			dir := writeRules(t, body)
			if err := LoadProfileRules(dir); err == nil {
				t.Fatalf("ожидалась ошибка, получили nil (%s)", name)
			}
			// После ошибки дефолты восстановлены — проверка не остаётся с пустыми/чужими правилами.
			if len(obligations) != len(defaultObligations()) ||
				len(metricRules) != len(defaultMetricRules()) ||
				len(stackForbidden) != len(defaultStackForbidden()) {
				t.Errorf("после ошибки срезы не сброшены к дефолту: obligations=%d metricRules=%d stackForbidden=%d",
					len(obligations), len(metricRules), len(stackForbidden))
			}
		})
	}
}

// Повторный вызов со другим файлом полностью заменяет предыдущие правила.
func TestLoadProfileRulesReloadResets(t *testing.T) {
	resetDefaults()
	dir := writeRules(t, "## stackForbidden\nangular\nreact\n")
	if err := LoadProfileRules(dir); err != nil {
		t.Fatal(err)
	}
	dir2 := writeRules(t, "## stackForbidden\nvue\n")
	if err := LoadProfileRules(dir2); err != nil {
		t.Fatal(err)
	}
	if strings.Join(stackForbidden, ",") != "vue" {
		t.Errorf("повторная загрузка не сбросила старые правила: %v", stackForbidden)
	}
	// И снова — без файла: возврат к generic (пустым) спискам.
	if err := LoadProfileRules(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(stackForbidden) != 0 || len(claimableTerms) != 0 {
		t.Errorf("после загрузки пустого каталога generic-списки не пусты: stackForbidden=%v claimableTerms=%v", stackForbidden, claimableTerms)
	}
}

// Универсальность: чужой профиль со СВОИМИ правилами не вызывает правила
// реального кандидата (Fraud Engine/Stable ID/Symfony/PHPUnit/…). Это
// главный регресс-сценарий плана: на чужом кандидате защита не должна
// молчать по чужим фактам И не должна навязывать чужие обязательства.
func TestAuditUniversalNoForeignFalsePositive(t *testing.T) {
	resetDefaults()
	// Правила ЧУЖОГО frontend-кандидата: свои obligation/метрика/стек.
	foreign := writeRules(t, `## obligations
need: (?i)react|frontend|веб-разраб
has: (?i)react
fact: React обязан быть в буллетах (frontend-профиль)

## metricRules
metric: (?i)120\+ компонент
owners: (?i)design system
fact: 120+ компонентов — только Design System

## stackForbidden
angular
`)
	if err := LoadProfileRules(foreign); err != nil {
		t.Fatalf("чужие правила не загрузились: %v", err)
	}

	// Чистое письмо ЧУЖОГО кандидата: закрывает его требования, не содержит
	// фактов реального кандидата.
	letter := "Стек: React, TypeScript.\nReact (Shop SPA), Design System: 120+ компонентов."
	vacancy := "Ищем frontend-разработчика: React, TypeScript, внимание к рендеру."
	res := Check(letter, vacancy)
	joined := strings.Join(res.Warnings, "|")

	// Правила реального кандидата не должны сработать на чужом письме.
	for _, fact := range []string{"Fraud Engine", "Stable ID", "Symfony", "PHPUnit",
		"MariaDB", "Bundle ID", "Domain ID", "Geo-mapping", "Yii2", "Lumen"} {
		if strings.Contains(joined, fact) {
			t.Errorf("чужое письмо вызвало правило реального кандидата %q: %v", fact, res.Warnings)
		}
	}
	// Чужие свои правила работают: React закрыт — обязанность не ругается.
	if strings.Contains(joined, "React обязан быть") {
		t.Errorf("чужое обязательство по React ложно сработало (React в письме): %v", res.Warnings)
	}

	// Контроль: то же письмо БЕЗ React → чужое обязательство реально ругается.
	letterNoReact := "Стек: Vue, TypeScript.\nVue (Shop SPA), Design System: 120+ компонентов."
	res2 := Check(letterNoReact, vacancy)
	if !strings.Contains(strings.Join(res2.Warnings, "|"), "React обязан быть") {
		t.Errorf("чужое обязательство по React НЕ сработало при его отсутствии: %v", res2.Warnings)
	}

	resetDefaults()
}

// (в) «###…» — пометка, а не секция: одна случайная ### не должна
// отбрасывать весь файл в fallback (иначе правила кандидата молча не применятся).
func TestLoadProfileRulesTripleHashNote(t *testing.T) {
	resetDefaults()
	body := "### моя пометка\n## stackForbidden\nangular\n\n### ещё пометка\n## claimableTerms\nOAuth2\n"
	dir := writeRules(t, body)
	if err := LoadProfileRules(dir); err != nil {
		t.Fatalf("###-пометки сломали файл: %v", err)
	}
	if strings.Join(stackForbidden, ",") != "angular" {
		t.Errorf("stackForbidden: want angular, got %v", stackForbidden)
	}
	if len(claimableTerms) != 1 || claimableTerms[0].pat != "OAuth2" {
		t.Errorf("claimableTerms: want OAuth2, got %+v", claimableTerms)
	}
	resetDefaults()
}

// claimableTerms: строка «термин: сообщение» несёт авторское предупреждение
// кандидата (в файле context/.audit-rules.md), голое слово — generic.
// Реальный кандидат даёт полные тексты, поэтому тест зафиксировал оба ветки:
// регресс на generic msg теряет 6 авторских предупреждений (блокер 2).
func TestLoadProfileRulesClaimableTermCustomMsg(t *testing.T) {
	resetDefaults()
	body := "## claimableTerms\nXSSI: XSSI sanitization — в профиле такого факта нет (только XSS в CMS Blog)\nJWT\n"
	dir := writeRules(t, body)
	if err := LoadProfileRules(dir); err != nil {
		t.Fatalf("claimableTerms с msg не загрузились: %v", err)
	}
	byPat := map[string]string{}
	for _, c := range claimableTerms {
		byPat[c.pat] = c.msg
	}
	// Авторское сообщение сохранено дословно.
	if byPat["XSSI"] != "XSSI sanitization — в профиле такого факта нет (только XSS в CMS Blog)" {
		t.Errorf("XSSI msg: %q", byPat["XSSI"])
	}
	// Голое слово получает generic-предупреждение (не авторское).
	if !strings.Contains(byPat["JWT"], "не выдумывай защиту") {
		t.Errorf("JWT (голое) msg: want generic, got %q", byPat["JWT"])
	}
	// Письмо, заявляющее XSSI, ловит именно авторское предупреждение.
	res := CheckProfile("Опыт: XSSI sanitization в CMS.", "Стек: PHP, Symfony")
	if !strings.Contains(strings.Join(res.Warnings, "|"), "только XSS в CMS Blog") {
		t.Errorf("авторское XSSI-предупреждение не сработало: %v", res.Warnings)
	}
	resetDefaults()
}

// TestNoCandidateDataInEngine — в движке (код без _test.go) не должно быть
// ссылок на фикстуры правил. Фикстуры существуют только для тестов: если
// не-тестовый код начнёт читать testdata, кандидат-специфичные данные
// попадут в исполняемый путь, а профиль перестанет управлять письмом сам.
func TestNoCandidateDataInEngine(t *testing.T) {
	t.Helper()
	var offenders []string
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Только исполняемые литералы и вызовы; упоминание в комментарии
		// допустимо — оно объясняет происхождение дефолтов.
		for i, line := range strings.Split(string(body), "\n") {
			code := line
			if idx := strings.Index(code, "//"); idx >= 0 {
				code = code[:idx]
			}
			if strings.Contains(code, "testdata") {
				offenders = append(offenders, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("код движка ссылается на фикстуры (кандидат-данные утекут в исполняемый путь):\n%s",
			strings.Join(offenders, "\n"))
	}
}

// TestProfileRulesOnlyFromContextDir — контракт загрузки: правила кандидата
// читаются ровно из <dir>/.audit-rules.md (server.go строит путь от
// ContextDir) и никакого другого файла. Гарантия, что профиль остаётся
// единственным источником кандидат-специфичных данных.
func TestProfileRulesOnlyFromContextDir(t *testing.T) {
	dir := writeRules(t, "## obligations\nneed: (?i)needle\nhas: (?i)needle\nfact: игла\n")
	if err := LoadProfileRules(dir); err != nil {
		t.Fatalf("LoadProfileRules: %v", err)
	}
	if len(obligations) == 0 {
		t.Fatal("правила из <dir>/.audit-rules.md не загрузились")
	}
	// Посторонние файлы с похожим содержимым игнорируются.
	if err := os.WriteFile(filepath.Join(dir, "rules.md"),
		[]byte("## obligations\nneed: (?i)other\nhas: (?i)other\nfact: другое\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resetDefaults()
	if err := LoadProfileRules(dir); err != nil {
		t.Fatalf("LoadProfileRules: %v", err)
	}
	for _, o := range obligations {
		if o.fact == "другое" {
			t.Fatal("загружены правила из постороннего файла каталога")
		}
	}
}
