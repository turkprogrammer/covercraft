package fit

import "testing"

// Живое письмо на Fullstack Backend Engineer (прогон с кастомным промптом
// v4, сентябрь 2026). Две противоположные ошибки в одном прогоне:
//   - Media/Video закрыто как «закрыто в письме», хотя письмо писало «готовность
//     осваивать медиа-конвейеры» — декларация готовности, не опыт;
//   - fullstack-продукты ушли в «не закрыто ничем», хотя письмо перечислило
//     Fraud Engine, Bundle ID, Domain ID, Geo-mapping с метриками.
const fullstackLiveLetter = "**Fullstack и инфраструктура:** уверенно владею Git (17 лет), GitLab CI/CD, Docker Compose, systemd, PHP-фреймворки.\n" +
	"**AI Agents:** Tool Use (bash-конвейеры GeoMapping, LLM API), управление контекстом.\n" +
	"**Адаптация под ваш стек:**\n" +
	"- **Media / Video / Render pipelines:** опыт оптимизации обработки 10 000 RPS и гонок данных в Event-Driven системе (Stable ID); готовность осваивать медиа-конвейеры на базе инженерной дисциплины Go.\n" +
	"- **Go-стаж:** коммерческого Go-опыта от 3 лет достаточно, глубина экспертизы компенсирует стаж.\n"

// TestReadinessNotExperience — «готовность осваивать X» это декларация
// готовности, а не опыт. understandingRe знал только «готов … освоить», а в
// живом письве форма «готовн��сть осваИВАТЬ»: суффикс -ость и инфинитив
// осваивать вместо освоить. Из-за этого Media/Video закрывалось как «закрыто в
// письме» при честно названном пробеле.
func TestReadinessNotExperience(t *testing.T) {
	for _, clause := range []string{
		"готовность осваивать медиа-конвейеры на базе инженерной дисциплины Go",
		"готовность изучать специфику медиа-отрасли",
		"способность переносить подход на новый стек",
		"готовость осваивать видео-пайплайны",
	} {
		if !understandingRe.MatchString(clause) {
			t.Errorf("форма готовности «%s» должна считаться декларацией, а не фактом", clause)
		}
	}
}

// TestMediaVideoGapNotClosedByForeignFacts — буллет про Media/Video содержит
// слово «опыт» и метрику чужого проекта (10 000 RPS в Stable ID), но НИ
// одного факта про видео. Требование не должно закрываться: наличие слова
// «опыт» рядом с метрикой соседнего сервиса не доказывает опыт в медиа.
func TestMediaVideoGapNotClosedByForeignFacts(t *testing.T) {
	reqs := Requirements{Role: "fullstack", MustHave: []Requirement{
		{Text: "Экспертиза в Media/Video: опыт в видео/аудио приложениях, видеоредакторах, пайплайнах рендеринга, кодеках, плеере или медиа-инфраструктуре", Kind: "must", Category: "stack"},
	}}
	f := Evaluate(DefaultConcepts(), reqs, "Go, Kafka, Stable ID 10 000 RPS, LLM, PHP.", fullstackLiveLetter, "Fullstack Backend, Media/Video")
	if f.Verdict == Skip {
		t.Fatalf("письмо не должно получать skip: %+v", f)
	}
	for _, c := range f.Covered {
		t.Logf("ЗАКРЫТО %q", c.Note)
	}
	if len(f.Covered) == 1 && f.Covered[0].Source == SrcLetter {
		t.Errorf("Media/Video не должен закрываться чужими фактами: %+v", f)
	}
}

// TestFullstackProductsCloseRequirement — требование про fullstack-продукты
// обязано закрываться перечнем продуктов с метриками (Fraud Engine, Bundle ID,
// Domain ID), даже когда слово «fullstack» стоит только в заголовке буллета,
// а не рядом с фактами.
func TestFullstackProductsCloseRequirement(t *testing.T) {
	reqs := Requirements{Role: "fullstack", MustHave: []Requirement{
		{Text: "Практический опыт fullstack-разработки в реальных B2C/B2B-продуктах с полным циклом доведения фичей до продакшна", Kind: "must", Category: "domain"},
	}}
	letter := "**Fullstack:** Fraud Engine (Go/REST, multi-tenancy, 92% F1, 1000+ RPS); Bundle ID (760-850 эл/с); " +
		"Domain ID (416K+ доменов); Geo-mapping Service (Llama-3.3-70B, 3.1M+ записей). Docker Compose, GitLab CI/CD."
	profile := "Fraud Engine, Bundle ID, Domain ID, Geo-mapping Service, Docker Compose, GitLab CI, PHP, Symfony."
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, "Fullstack Backend")
	if f.Verdict == Skip {
		t.Fatalf("письмо не должно получать skip: %+v", f)
	}
	if len(f.Covered) == 0 {
		t.Errorf("перечень продуктов с метриками должен закрывать fullstack; получили %+v", f)
	}
}
