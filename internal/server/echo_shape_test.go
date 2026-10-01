package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Скриншот живого бага: модель вернула профиль с ПЕРЕФОРМАТИРОВАНИЕМ —
// двоеточие после заголовка, пометки-эмодзи. Дословного совпадения строк
// с промптом нет, поэтому построчный детектор молчал.
const profileEchoReformatted = "### 1.3 Bundle ID Service — классификация приложений\n" +
	"- **Task Flow:** Kafka (predict input + train input) — Transport Workers — ProcessManager — Worker Workflow — Output — ClickHouse / PostgreSQL\n" +
	"- **Priority Queue:** predict (75%), train (12.5%), export (12.5%) — через priority queue с приоритетами\n" +
	"### 1.4 Domain ID / Domain Classification Service — классификация доменов\n" +
	"**Назначение:** классификация веб-доменов по IAB Content Taxonomy (v3.1)\n" +
	"- **IAB Content Taxonomy:** 3.1.** 2-level классификация (our_category + iab_detailed + iab_parent); переход на отраслевую таксономию покрытие категорий с 6 до 9 (100%)\n" +
	"- **Миграция:** 329,847 legacy records, 85,420 whitelist domains auto-processed. 🔴 Не переносить «без даунайна» — ЗАФИКСИРОВАНО в профиле\n" +
	"### 1.5–1.9 Прочие Go-инструменты (кратко)\n" +
	"- **Task Flow:** Go REST API управления задачами; Layered (Handler→Service→Store), MySQL 8.0, Redis\n"

// TestIsPromptEchoDetectsReformattedProfile — эхо с переформатированием
// (двоеточия после заголовков, эмодзи-пометки) должно распознаваться.
func TestIsPromptEchoDetectsReformattedProfile(t *testing.T) {
	user := "### профиль\n## КАРТА ФАКТОВ\n### 1.3 Bundle ID Service — классификация приложений\n" +
		"- **Task Flow:** Kafka (predict input + train input) — Transport Workers — ProcessManager\n" +
		"### 1.4 Domain ID / Domain Classification Service\n" +
		"- **IAB Content Taxonomy:** 3.1.*, 2-level классификация\n\n### Вакансия\nGo backend engineer"
	if !isPromptEcho(profileEchoReformatted, user, "", "") {
		t.Errorf("эхо профиля с переформатированием должно распознаваться:\n%q", profileEchoReformatted)
	}
}

// TestIsPromptEchoDetectsProfileShape — профиль без дословных строк, но с
// узнаваемой формой: ≥4 markdown-заголовка раздела и ни одного письменного
// признака.
func TestIsPromptEchoDetectsProfileShape(t *testing.T) {
	user := "### профиль\n## КАРТА ФАКТОВ\n- факт 1\n- факт 2\n\n### Вакансия\nGo engineer"
	if !isPromptEcho(profileEchoReformatted, user, "", "") {
		t.Error("форма профиля (много заголовков, нет письменных признаков) должна распознаваться")
	}
}

// TestIsPromptEchoAllowsLetterWithManyHeadings — настоящее письмо может иметь
// несколько жирных подзаголовков; оно не должно считаться эхом.
func TestIsPromptEchoAllowsLetterWithManyHeadings(t *testing.T) {
	user := "### профиль\n## КАРТА ФАКТОВ\n- **Stable ID:** Kafka, 10 000 RPS\n\n### Вакансия\nGo engineer"
	letter := "Здравствуйте!\n\n**Go и highload:** Fraud Engine — 92% F1, 1000+ RPS; Stable ID — 10 000 RPS.\n\n" +
		"**System Design:** ADR (10), Hexagonal, DDD; retries, deadlines, idempotency.\n\n" +
		"**Базы данных:** ClickHouse, PostgreSQL, MySQL, Redis.\n\n" +
		"**AI Agents:** Llama 3.3, fallback-цепочки, экономия токенов 60-70%.\n\n" +
		"Буду рад обсудить ваши задачи. Спасибо за внимание!\n\nС уважением,"
	if isPromptEcho(letter, user, user, "") {
		t.Errorf("настоящее письмо принято за эхо:\n%q", letter)
	}
}

// TestOversizedLetterRejected — ответ неприемлемого размера (профиль целиком,
// 11 742 символа на скриншоте живого бага) не должен сохраняться как письмо.
func TestOversizedLetterRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "01-профиль.md"),
		[]byte("## КАРТА ФАКТОВ\n- **Stable ID:** Kafka, 10 000 RPS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("Профиль: факты, метрики, детали проекта, заметки. ", 400)
	h := New(Config{
		ContextDir: dir,
		LLMStream: func(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
			onDelta(huge)
			return huge, nil
		},
	})
	body, _ := json.Marshal(map[string]any{"vacancy": "Go backend"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	ev := parseSSE(t, rec.Body.String())
	if ev.Done == nil {
		t.Fatal("нет done-события")
	}
	if len([]rune(ev.Done.Letter)) > 0 {
		t.Errorf("ответ неприемлемого размера не должен стать письмом: %d символов", len([]rune(ev.Done.Letter)))
	}
	found := false
	for _, w := range ev.Done.Warnings {
		if strings.Contains(w, "размер") || strings.Contains(w, "символ") {
			found = true
			// Длина в сообщении должна быть настоящей. Раньше letter
			// обнулялся ДО замера, и предупреждение всегда сообщало
			// «0 символов вместо ~8000» — размер ответа при отладке
			// был неузнаваем.
			want := strconv.Itoa(len([]rune(huge))) + " символов"
			if !strings.Contains(w, want) {
				t.Errorf("в предупреждении нет реального размера %q: %q", want, w)
			}
			if strings.Contains(w, "(0 символов") {
				t.Errorf("размер обнулён до измерения — это баг: %q", w)
			}
		}
	}
	if !found {
		t.Errorf("нет предупреждения про размер ответа: %v", ev.Done.Warnings)
	}
}

// TestPromptEchoIgnoresContactLine — живой баг (октябрь 2026): контактная
// строка письма дословно лежит в context/00-контакты.md и длиннее порога
// (105 символов ≥ 40), поэтому isPromptEcho отклоняло ВАЛИДНОЕ письмо:
// done.letter = "", копирование блокировалось, audit.Check по пустой строке
// выдавал 7 замечаний-мусора («нет обязательной секции», «Потерян факт»),
// а fit-fix откатывал каждую исправленную версию обратно — «деградация».
// Письмо — не эхо, даже если одна длинная строка совпала с профилем:
// это нормально для контактов, строки стека и цитат.
func TestPromptEchoIgnoresContactLine(t *testing.T) {
	user := "### профиль\n## КАРТА ФАКТОВ\n" +
		"- **Stable ID:** Kafka, 10 000 RPS, at-least-once, идемпотентность через ClickHouse Upsert\n" +
		"+7 (000) 000-00-00 | Telegram: @example | https://yusupov-tech.ru/ | github.com/turkprogrammer\n" +
		"\n### Вакансия\nGo backend engineer"
	letter := "Здравствуйте!\n\n" +
		"- Go и highload: Stable ID — Kafka, 10 000 RPS, at-least-once через ClickHouse Upsert; " +
		"Fraud Engine — Random Forest, 92% F1, 1000+ RPS, P95 < 4.2ms.\n" +
		"- System Design: ADR, Hexagonal, DDD; retries, deadlines, идемпотентность.\n\n" +
		"+7 (000) 000-00-00 | Telegram: @example | https://yusupov-tech.ru/ | github.com/turkprogrammer\n" +
		"Буду рад обсудить ваши задачи. Спасибо за внимание!\n\nС уважением,"
	if isPromptEcho(letter, user, user, "") {
		t.Errorf("валидное письмо принято за эхо из-за контактной строки:\n%q", letter)
	}
}

// TestGenerateEchoReturnsOnlyEchoWarning — при отклонённом эхо наружу не
// должны утекать замечания аудита, посчитанные по пустой строке: они
// дезориентируют («нет обязательной секции», «Потерян факт»), а пользователь
// принимает их за реальные дефекты письма. warnings = только echo.
func TestGenerateEchoReturnsOnlyEchoWarning(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "01-профиль.md"),
		[]byte("## КАРТА ФАКТОВ\n- **Stable ID:** Kafka, 10 000 RPS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(Config{
		ContextDir: dir,
		LLMStream: func(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
			echo := "### 1.3 Bundle ID Service — классификация приложений\n" +
				"- **Task Flow:** Kafka — Transport Workers — ProcessManager — Worker Workflow\n" +
				"### 1.4 Domain ID / Domain Classification Service\n" +
				"### 1.5–1.9 Прочие Go-инструменты\n"
			onDelta(echo)
			return echo, nil
		},
	})
	body, _ := json.Marshal(map[string]any{"vacancy": "Go backend engineer"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	ev := parseSSE(t, rec.Body.String())
	if ev.Done == nil {
		t.Fatal("нет done-события")
	}
	if ev.Done.Letter != "" {
		t.Errorf("эхо не должно стать письмом: %q", ev.Done.Letter)
	}
	for _, w := range ev.Done.Warnings {
		if strings.Contains(w, "секци") || strings.Contains(w, "Потерян факт") ||
			strings.Contains(w, "финальн") || strings.Contains(w, "контакт") {
			t.Errorf("замечание аудита по пустому письму утекло наружу: %q", w)
		}
	}
	if len(ev.Done.Warnings) != 1 {
		t.Errorf("ожидалось ровно одно предупреждение об эхо, получено %d: %v",
			len(ev.Done.Warnings), ev.Done.Warnings)
	}
}

// TestFitFixPromptDropsFullProfile — интеграционный: в fit-fix уходит только
// релевантная секция профиля, а не все 85 КБ context/*.md (живой баг:
// модель тонет в объёме, отвечает эхом, правки откатываются).
func TestFitFixPromptDropsFullProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	profile := "# Профиль\n\n## Go и highload\n" +
		"Stable ID: Kafka, 10 000 RPS, at-least-once, идемпотентность через ClickHouse Upsert.\n\n" +
		"## Маркетинг и SEO\n" +
		"Собрал воронку, настроил таргетированную рекламу, курил контент-план, подбирал ключи.\n"
	if err := os.WriteFile(filepath.Join(dir, "01-профиль.md"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotUser string
	h := New(Config{
		ContextDir: dir,
		LLMStream: func(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
			gotUser = user
			return "Здравствуйте! С уважением,", nil
		},
	})
	body, _ := json.Marshal(map[string]any{
		"vacancy":    "Go backend engineer",
		"fitFix":     true,
		"letter":     "Здравствуйте!",
		"fitCaveats": []string{"Kafka at-least-once идемпотентность ClickHouse — впиши в письмо"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if !strings.Contains(gotUser, "Stable ID: Kafka, 10 000 RPS") {
		t.Errorf("релевантная секция не попала в промпт fit-fix:\n%.400s", gotUser)
	}
	if strings.Contains(gotUser, "таргетированную") {
		t.Errorf("нерелевантная секция не должна уходить в fit-fix:\n%.400s", gotUser)
	}
}

// --- Регресс: самоотменяющееся исключение по origLetter (октябрь 2026) ---

// Профиль-источник: строки эха дословно взяты из него.
const echoProfileSrc = "# КАРТА МЕТРИК\n" +
	"- 92% F1 — Fraud Detection Engine (чистый Go, антифрод)\n" +
	"- 10 000 RPS — Stable ID (Kafka, 20+ воркеров)\n" +
	"## 2. PHP — PRODUCTION (17 ЛЕТ ОПЫТА)\n" +
	"- **Symfony:** E-commerce-Lite (7.2, Hexagonal Architecture, DDD, TDD), Symfony-URL-Shortener (8.0, REST API, PHPUnit 13)\n" +
	"## 5. DevOps / ИНФРАСТРУКТУРА\n" +
	"- **Docker:** полный production-стек: API + PostgreSQL + Redis + Prometheus + Grafana\n"

// Эхо второй итерации: модель вернула кусок профиля, и этот кусок уже
// стал origLetter, потому что fit-fix берёт письмо из предыдущей итерации.
const echoSecondIteration = "Здравствуйте!\n\n" +
	"- 92% F1 — Fraud Detection Engine (чистый Go, антифрод)\n" +
	"- 10 000 RPS — Stable ID (Kafka, 20+ воркеров)\n" +
	"- **Symfony:** E-commerce-Lite (7.2, Hexagonal Architecture, DDD, TDD), Symfony-URL-Shortener (8.0, REST API, PHPUnit 13)\n" +
	"- **Docker:** полный production-стек: API + PostgreSQL + Redis + Prometheus + Grafana\n\nС уважением,\n"

// TestIsPromptEchoNotSelfCancelledByOrigLetter — корень бага «виснет и
// выводит профиль»: эхо, став origLetter'ом, оправдывало само себя и на
// следующей итерации детектор его не видел.
func TestIsPromptEchoNotSelfCancelledByOrigLetter(t *testing.T) {
	user := "### Профиль\n" + echoProfileSrc + "\n### Письмо:\nЗдравствуйте!\n"
	if !isPromptEcho(echoSecondIteration, user, echoProfileSrc, echoSecondIteration) {
		t.Error("эхо, ставшее исходным письмом, должно распознаваться: строки взяты из профиля дословно")
	}
}

// TestIsPromptEchoKeepsLetterLinesOutsideProfile — стоп-тест: правка письма,
// строки которой НЕ встречаются в профиле, остаётся письмом.
func TestIsPromptEchoKeepsLetterLinesOutsideProfile(t *testing.T) {
	user := "### Профиль\n- **Stable ID:** Kafka, 10 000 RPS, at-least-once, идемпотентность через ClickHouse Upsert\n\n### Письмо:\nЗдравствуйте!\n"
	orig := "Здравствуйте!\n\n- Go и highload: подробное описание моего подхода к построению надёжной системы очередей."
	edited := orig + "\n\n- OAuth 2.0 / OIDC: интеграции в production не делал, но JWT HS256 и X-API-Key в Fraud Engine дают прямую базу."
	if isPromptEcho(edited, user, user, orig) {
		t.Errorf("правка письма принята за эхо:\n%q", edited)
	}
}

// TestEchoSupersedesSingleHashHeadings — одиночная «#» входит в подсчёт
// заголовков: на живом эхе их было три (две «##» + одна «#»), и старая
// регулярка считала только две, не достигая порога.
func TestEchoSupersedesSingleHashHeadings(t *testing.T) {
	letter := "# КАРТА МЕТРИК\n" +
		"## 2. PHP — PRODUCTION (17 ЛЕТ ОПЫТА)\n" +
		"## 5. DevOps / ИНФРАСТРУКТУРА\n"
	if !isPromptEcho(letter, "### Профиль\nсовсем другой текст\n", "", "") {
		t.Errorf("эхо с одиночной «#» должно распознаваться структурно:\n%q", letter)
	}
}

// TestFitVerdictSuppressedOnEcho — правка 4: при отклонённом эхе вердикт
// фита не считается. Раньше «письмо» из профиля давало ~87% покрытия,
// пользователь принимал это за оценку своего ответа.
func TestFitVerdictSuppressedOnEcho(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	profile := "# КАРТА МЕТРИК\n- 10 000 RPS — Stable ID (Kafka, 20+ воркеров)\n- **Symfony:** E-commerce-Lite (7.2, Hexagonal), PHPUnit 13\n"
	if err := os.WriteFile(filepath.Join(dir, "01-профиль.md"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(Config{
		ContextDir: dir,
		LLMStream: func(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
			echo := "# КАРТА МЕТРИК\n- 10 000 RPS — Stable ID (Kafka, 20+ воркеров)\n" +
				"## 2. PHP — PRODUCTION (17 ЛЕТ ОПЫТА)\n" +
				"- **Symfony:** E-commerce-Lite (7.2, Hexagonal), PHPUnit 13\n" +
				"## 5. DevOps / ИНФРАСТРУКТУРА\n- **Docker:** production-стек: API + PostgreSQL + Redis\n"
			onDelta(echo)
			return echo, nil
		},
		FitLLM: func(ctx context.Context, system, user string) (string, error) {
			return `{"role":"php-primary","mustHave":[{"text":"Symfony","kind":"must","category":"stack"}]}`, nil
		},
	})
	body, _ := json.Marshal(map[string]any{"vacancy": "PHP/Symfony backend, требуется Symfony"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	ev := parseSSE(t, rec.Body.String())
	if ev.Done == nil {
		t.Fatal("нет done-события")
	}
	if ev.Done.Letter != "" {
		t.Errorf("эхо не должно стать письмом: %q", ev.Done.Letter)
	}
	if ev.Done.Fit != nil {
		t.Errorf("при отклонённом эхе вердикт фита не считается, а пришёл: %+v", *ev.Done.Fit)
	}
	if ev.Done.FitFixable != 0 {
		t.Errorf("fitFixable должен быть 0 при отклонённом эхе, получено %d", ev.Done.FitFixable)
	}
}

// TestStreamParamsDistinctFieldsReachConsumers — защита от перестановки
// однотипных аргументов. До streamParams сигнатура принимала шесть string
// подряд (system, user, vacancy, profile, origLetter), и «профиль» с
// «вакансией» могли поменяться местами молча: компилятор доволен, а фит
// считается по вакансии вместо профиля.
//
// Тест гоняет настоящий /api/generate и проверяет, что UsedProfileBytes и
// UsedUserPromptBytes отражают РАЗНЫЕ строки, а verdict строится на профиле:
// при перестановке полей профиль и вакансия поменялись бы местами в
// user-промпте и в фите.
func TestStreamParamsDistinctFieldsReachConsumers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	profile := "# КАРТА ФАКТОВ\n- **Symfony:** E-commerce-Lite, 7 лет, Symfony-URL-Shortener\n"
	if err := os.WriteFile(filepath.Join(dir, "01-профиль.md"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	letter := "Здравствуйте!\n\n- Symfony: E-commerce-Lite, PHPUnit.\n\nС уважением,\n"
	h := New(Config{
		ContextDir: dir,
		LLMStream: func(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
			onDelta(letter)
			return letter, nil
		},
	})
	body, _ := json.Marshal(map[string]any{"vacancy": "PHP backend Symfony, Docker"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	ev := parseSSE(t, rec.Body.String())
	if ev.Done == nil {
		t.Fatal("нет done-события")
	}
	if ev.Done.UsedProfileBytes != len(profile) {
		t.Errorf("UsedProfileBytes=%d, ожидалась длина профиля %d — вероятно, поля переставлены",
			ev.Done.UsedProfileBytes, len(profile))
	}
	if ev.Done.UsedUserPromptBytes <= 0 {
		t.Error("UsedUserPromptBytes должен быть положительным")
	}
	if ev.Done.UsedUserPromptBytes == ev.Done.UsedProfileBytes {
		t.Error("user-промпт и профиль не могут иметь одинаковую длину — подозрение на перестановку")
	}
}
