package audit

import (
	"strings"
	"testing"
)

// Живое письмо SRE-вакансии (прогон 2, сентябрь 2026). Регрессия против
// фабрикаций, которые модель натянула «под вакансию» в блоке «Адаптация под
// ваш стек»: Tempo/Pyroscope/Service Mesh/CloudNative-PG не встречаются ни в
// одном context/*.md (проверено: 0 вхождений), а «навык построения трассировки
// событий через Kafka» выдумывает компетенцию, которой нет.
const sreLiveLetter2 = "Здравствуйте!\n\n" +
	"Мой опыт в высоконагруженных системах (17 лет) и работа с Go-бэкендом позволяют мне эффективно решать задачи SRE.\n\n" +
	"*   **SRE в high-load средах:** 10+ лет опыта обеспечения стабильности production-сервисов. Ключевые метрики: Fraud Engine — 92% F1, P95 < 4.2ms; Stable ID — 10 000 RPS.\n" +
	"*   **PostgreSQL (внутреннее устройство):** Глубокое знание индексов (B-tree/GIN/partial/covering), тюнинга `shared_buffers`/`work_mem`, мониторинга bloat и autovacuum. Автор TUI-профайлера SQL-Top для анализа execution plans и kill query. *Пробел:* Опыт работы с CloudNative-PG/WAL на уровне администрирования кластера ограничен; понимаю принципы WAL, готов углубиться в специфику CNPG.\n" +
	"*   **Observability:** Развернул Prometheus + Grafana (23 панели, алертинг по latency/error rate). Интегрировал логирование через Elasticsearch/Kibana в банковских микросервисах. *Пробел:* Нет прямого опыта с Tempo/OpenTelemetry, но есть навык построения трассировки событий через Kafka и структурированное логирование.\n" +
	"*   **Сети (HTTP/gRPC):** Диагностика сетевой подсистемы Linux (`/sys/class/net`) 17+ лет. Работа с gRPC (банк Росгосстрах), HTTP/REST API, TLS termination (Caddy). Понимание принципов Service Mesh, но *пробел:* нет практического опыта отладки внутри Cilium/Linkerd.\n" +
	"*   **Kubernetes:** *Честный пробел:* Нет опыта администрирования Managed K8s или Helm. Мост: уверенная работа с Docker Compose, systemd, понимание архитектуры оркестрации.\n\n" +
	"**Адаптация под ваш стек:** Быстро интегрируюсь в ваши инструменты observability (Tempo, Pyroscope) и инфраструктурные решения (CNPG, Service Mesh).\n\n" +
	"Стек: Go, PostgreSQL, Redis, ClickHouse, Kafka, Prometheus, Grafana, Elasticsearch, Docker, Caddy, GitLab CI, Linux\n\nС уважением,"

// Профиль здесь полный (compose-фильтрация на audit не влияет).
const sreLiveProfileFull = "ОБЩИЙ ПРОФИЛЬ\n" +
	"- **Языки:** Go (3 года), PHP (2005+), Bash, SQL\n" +
	"- **PostgreSQL:** индексы B-tree/GIN/partial/covering, autovacuum tuning, мониторинг bloat, execution plans; автор SQL-Top (pg_stat_statements, kill query). WAL-G/Patroni/pg_basebackup: НЕ работал; допустимо «понимаю принципы WAL».\n" +
	"- **Мониторинг:** Prometheus + Grafana (3 дашборда, 23 панели, алертинг P99 > 100ms, error rate > 1%).\n" +
	"- **Логи:** Elasticsearch/Kibana в банковских микросервисах (Росгосстрах).\n" +
	"- **Сеть:** gRPC и REST API (Росгосстрах), диагностика сетевой подсистемы Linux, Caddy/Nginx.\n" +
	"- **Метрики:** Fraud Engine F1 92%, P95 < 4.2ms; Stable ID 10 000 RPS.\n" +
	"- **Blue-Green Deploy:** Stable ID (model versions). **mt:** Go Runtime Tuner CLI.\n" +
	"- **Docker:** Docker Compose для production-стеков. **GitLab CI:** пайплайны в продакшене.\n" +
	"- **Kafka:** at-least-once, идемпотентность через ClickHouse Upsert.\n" +
	"- **Kubernetes:** НЕ работал. Service Mesh / Cilium / Linkerd / Istio: НЕ работал.\n" +
	"- **OpenTelemetry / Tempo / Pyroscope / Jaeger / Zipkin:** НЕ работал; допустимо «принципы трассировки понятны».\n" +
	"- **CloudNative-PG / CNPG / Patroni:** НЕ работал.\n" +
	"- **Ограничения:** не заявлять стаж SRE/сетевой диагностики сверх общего (17 лет бэкенд).\n"

// TestLiveSREFabricatedInfraTerms — универсальный guard: термины, которых
// НЕТ в профиле, но которые письмо заявляет как свои инструменты/компетенцию,
// должны давать warning. До правок audit знал только узкий список security-
// терминов (XSSI/CSRF/cookie), поэтому весь этот выдуманный SRE-сетап проходил
// молча, и письмо уходило с выдуманными Tempo, Pyroscope, Service Mesh, CNPG.
func TestLiveSREFabricatedInfraTerms(t *testing.T) {
	r := CheckProfile(sreLiveLetter2, sreLiveProfileFull)
	joined := ""
	for _, w := range r.Warnings {
		joined += w + " | "
	}
	// Ловим только УТВЕРДИТЕЛЬНО заявленные выдумки — блок «Адаптация под ваш
	// стек». Cilium/Linkerd/CloudNative письмо отрицает честно («нет практического
	// опыта отладки внутри Cilium/Linkerd», «опыт … ограничен»), и guard их
	// правильно пропускает: отрицание — не заявление о навыке.
	//
	// Tempo в письме встречается дважды: в честном пробеле («нет прямого опыта с
	// Tempo/OpenTelemetry») и в выдуманной адаптации («интегрируюсь в Tempo,
	// Pyroscope»). Ловится именно вторая подача.
	for _, want := range []string{"pyroscope", "tempo", "service mesh", "cnpg"} {
		if !strings.Contains(strings.ToLower(joined), want) {
			t.Errorf("нет warning на выдуманный термин %q (все warning: %s)", want, joined)
		}
	}
}

// TestLiveSREResumHiddenInGapBullet — живая конструкция письма: «Пробел: Нет
// прямого опыта с Tempo/OpenTelemetry, но есть навык построения трассировки
// событий через Kafka». Прежний butRe требовал порядка «опыт … есть …, но» и
// такой буллет пропускал, хотя достижение спрятано прямо в пробеле.
func TestLiveSREResumHiddenInGapBullet(t *testing.T) {
	letter := "Здравствуйте!\n\n" +
		"*   **Observability:** Развернул Prometheus + Grafana (23 панели).\n\n" +
		"Честно о пробелах:\n" +
		"*   *Пробел:* Нет прямого опыта с Tempo/OpenTelemetry, но есть навык построения трассировки событий через Kafka и структурированное логирование.\n\nС уважением,"
	r := Check(letter, "observability: метрики, логи, трейсинг")
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "спрятан") {
			found = true
		}
	}
	if !found {
		t.Errorf("достижение, спрятанное в буллете пробела («нет опыта с Tempo, но есть навык»), должно ловиться; warning: %v", r.Warnings)
	}
}

// TestLiveSRETooLongLetter — живое письмо SRE-прогона: 307 слов в реальном
// прогоне (210 в теле константы, без контактов и хвоста) — лимит 200
// из промпта («Объём — до 200 слов, но если не влезает, сокращай второстепенные
// факты, а не требования»). Audit длину не проверял вовсе: правило было только
// в тексте промпта, и модель его перевыполнила без последствий.
func TestLiveSRETooLongLetter(t *testing.T) {
	words := len(strings.Fields(sreLiveLetter2))
	if words <= 200 {
		t.Skipf("живое письмо короче лимита (%d слов) — тест устарел", words)
	}
	r := Check(sreLiveLetter2, "SRE: observability, Kubernetes, PostgreSQL")
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "200") || strings.Contains(w, "слов") {
			found = true
		}
	}
	if !found {
		t.Errorf("письмо на %d слов при лимите 200 должно давать warning; имеем: %v", words, r.Warnings)
	}
}

// TestLiveSRESeveralClosings — живое письмо заканчивалось тремя призывами подряд:
// «Готов обсудить детали…», «Буду рад обсудить… Спасибо за внимание!» и
// «С уважением,». Правило требует финальную строку, и модель её дала, но своё
// добавила тоже — никто не проверял, что призыв к диалогу ОДИН.
func TestLiveSRESeveralClosings(t *testing.T) {
	// Живое письмо целиком: константа хранит тело, хвост добавляем как в
	// реальном прогоне — контакты, призыв и подпись.
	letter := sreLiveLetter2 +
		"\n\n+7 (000) 000-00-00 | Telegram: @example | https://yusupov-tech.ru/ | github.com/turkprogrammer\n\n" +
		"Готов обсудить детали моей архитектуры и подход к надежности.\n\n" +
		"Буду рад обсудить… Спасибо за внимание!\n\nС уважением,"
	r := Check(letter, "SRE: observability")
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "закрыти") || strings.Contains(w, "призыв") {
			found = true
		}
	}
	if !found {
		t.Errorf("два призыва к диалогу подряд должны давать warning; имеем: %v", r.Warnings)
	}
}

// TestSingleClosingNotFlagged — обратная сторона: один призыв + «Спасибо за
// внимание» + подпись — это нормальная связка, ругаться нельзя.
func TestSingleClosingNotFlagged(t *testing.T) {
	letter := "Здравствуйте!\n\nGo, PHP, Kafka, PostgreSQL.\n\n" +
		"Буду рад обсудить ваши задачи. Спасибо за внимание!\n\nС уважением,"
	for _, w := range Check(letter, "Go-бэкенд").Warnings {
		if strings.Contains(w, "закрыти") || strings.Contains(w, "призыв") {
			t.Errorf("один призыв к диалогу — норма, warning лишний: %q", w)
		}
	}
}

// TestLiveFullstackFabricatedTools — живое письмо на Fullstack Backend Engineer
// (прогон с кастомным промптом v4, сентябрь 2026) заявляло «ИИ-инструменты
// (Cursor, Claude Code и аналоги) — ежедневная практика». В профиле ни
// Cursor, ни Claude Code не встречаются ни разу. Инструменты ИИ-разработки
// — тот же класс, что Tempo/Pyroscope: модель дорисовывает «правдоподобное».
func TestLiveFullstackFabricatedTools(t *testing.T) {
	letter := "Здравствуйте!\n\n" +
		"- **AI Agents и оптимизация ИИ:** ИИ-инструменты (Cursor, Claude Code и аналоги) — ежедневная практика " +
		"(постановка задач, генерация, ревью, отладка); Tool Use (bash-конвейеры GeoMapping).\n\n" +
		"С уважением,"
	profile := "LLM: Llama 3.3, DeepSeek, bash-конвейеры, RAG CLI System, fallback-цепочки."
	r := CheckProfile(letter, profile)
	joined := ""
	for _, w := range r.Warnings {
		joined += strings.ToLower(w) + " | "
	}
	for _, want := range []string{"cursor", "claude"} {
		if !strings.Contains(joined, want) {
			t.Errorf("нет warning на выдуманный ИИ-инструмент %q; имеем: %s", want, joined)
		}
	}
}

// TestPersuasionArgumentDetected — «коммерческого Go-опыта от 3 лет достаточно,
// глубина экспертизы компенсирует стаж» — не факт, а попытка уговорить
// работодателя пренебречь требованием. Такого в письме быть не должно.
func TestPersuasionArgumentDetected(t *testing.T) {
	letter := "Здравствуйте!\n\nGo, PHP, Kafka.\n\n" +
		"- **Go-стаж:** коммерческого Go-опыта «от 3 лет» достаточно, глубина экспертизы компенсирует стаж.\n\n" +
		"Буду рад обсудить ваши задачи. Спасибо за внимание!\n\nС уважением,"
	found := false
	for _, w := range Check(letter, "Go от 3 лет").Warnings {
		if strings.Contains(w, "аргумент") || strings.Contains(w, "убедить") || strings.Contains(w, "оправд") {
			found = true
		}
	}
	if !found {
		t.Errorf("оправдательный аргумент под требование должен ловиться; имеем: %v", Check(letter, "Go от 3 лет").Warnings)
	}
}

// TestPlainSeniorityStatementNotFlagged — обычное утверждение о соответствии
// стажа требованию — не оправдание, ругаться на него нельзя.
func TestPlainSeniorityStatementNotFlagged(t *testing.T) {
	letter := "Здравствуйте!\n\n3 года коммерческого опыта на Go, стаж соответствует требованию вакансии.\n\n" +
		"Буду рад обсудить ваши задачи. Спасибо за внимание!\n\nС уважением,"
	for _, w := range Check(letter, "Go от 3 лет").Warnings {
		if strings.Contains(w, "аргумент") || strings.Contains(w, "оправд") {
			t.Errorf("обычное утверждение о стаже не должно считаться оправданием: %q", w)
		}
	}
}

// forbiddenProfileBullets — дословный запрет из context/01:336-340 (единственное
// упоминание «Cursor/Claude Code/AI Agents» во всём контексте). Строка запрета
// НЕ является фактом: аудит и fit обязаны её вырезать перед сверкой.
const forbiddenProfileBullets = `- **ИИ-инструменты разработки — НЕ заявлять без факта:** в профиле Cursor, Claude Code,
  Copilot, ChatGPT, Gemini — 0 вхождений. Если реально используешь — добавь сюда
  строку с конкретикой (задачи, что именно делаешь, с какого месяца). Модель
  дописывает «ИИ-инструменты (Cursor, Claude Code и аналоги) — ежедневная практика»
  по вакансии про AI Agents, и это ловится на интервью первым вопросом.`

// TestAuditFlagsAIClaimWithoutProfileFact — живой кейс Fullstack Backend
// (октябрь 2026): письмо писало «ИИ-инструменты — ежедневная практика
// (постановка задач, генерация, ревью, отладка)» без единого названия
// инструмента. В профиле их 0 фактов — только запретная строка, и аудит
// молчал (1 замечание — только эхо), хотя фабрикация попадает на интервью
// первым вопросом.
func TestAuditFlagsAIClaimWithoutProfileFact(t *testing.T) {
	letter := "Здравствуйте!\n\n- **AI-native разработка:** ИИ-инструменты — ежедневная практика " +
		"(постановка задач, генерация, ревью, отладка); весь ИИ-код проходит code review.\n\nБуду рад обсудить."
	r := CheckProfile(letter, forbiddenProfileBullets)
	if len(r.Warnings) == 0 {
		t.Fatal("заявка «ИИ-инструменты — ежедневная практика» при 0 фактов в профиле прошла молча")
	}
	joined := strings.Join(r.Warnings, " | ")
	if !strings.Contains(joined, "ИИ-инструмент") {
		t.Errorf("warning должен называть класс фабрикации: %v", r.Warnings)
	}
}

// TestAuditIgnoresAIClaimWithRealFact — обратный случай: если пользователь
// зафиксировал факт в профиле (даже имя инструмента в другой строке), заявка
// легальна и не должна давать ложного warning'а.
func TestAuditIgnoresAIClaimWithRealFact(t *testing.T) {
	letter := "Здравствуйте!\n\n- ИИ-инструменты — ежедневная практика (постановка задач, ревью).\n\nБуду рад обсудить."
	profile := forbiddenProfileBullets + "\n- **ИИ-инструменты в работе:** Cursor + Claude Code — каждый день с марта 2026: генерация кода, ревью, отладка.\n"
	r := CheckProfile(letter, profile)
	for _, w := range r.Warnings {
		if strings.Contains(w, "ИИ-инструмент") {
			t.Errorf("легальный факт снят предупреждением: %s", w)
		}
	}
}

// TestAuditFlagsClaimedForeignStack — письмо заявляет опыт чужого стека
// (октябрь 2026, Fullstack Backend): в профиле ни Convex, ни Remotion, ни E2B,
// ни React Native. Имена без CamelCase-границы (Convex, Remotion) и с цифрой
// (E2B) не ловились ни CamelCase-guard'ом, ни all-caps, ни infraCommonWords.
func TestAuditFlagsClaimedForeignStack(t *testing.T) {
	letter := "Опыт: TypeScript, React Native, Convex, Remotion, E2B в production-продуктах."
	r := CheckInfraClaims(letter, "Go, PHP, Kafka, PostgreSQL — профиль без фронтенд-стека.")
	for _, want := range []string{"Convex", "Remotion", "E2B"} {
		found := false
		for _, w := range r.Warnings {
			if strings.Contains(w, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("заявленный «%s» без факта в профиле не поймано: %v", want, r.Warnings)
		}
	}
}

// TestAuditIgnoresReadyToLearnListing — стоп-тест: честное перечисление
// стека вакансии с признанием «готова освоить» — не заявка опыта, ругаться
// нельзя (иначе каждый блок адаптации даёт пачку ложных warning'ов).
func TestAuditIgnoresReadyToLearnListing(t *testing.T) {
	profile := "Go, PHP, Kafka, CMS Blog (React), PostgreSQL — профиль без остального фронтенд-стека."
	for _, w := range CheckInfraClaims(adaptationLine, profile).Warnings {
		for _, name := range []string{"TypeScript", "React Native", "Expo", "Convex", "Remotion", "E2B"} {
			if strings.Contains(w, name) {
				t.Errorf("честное «готова освоить» наказано warning'ом про %s: %s", name, w)
			}
		}
	}
}

// adaptationLine — строка-адаптация из живого письма (дублируется из fit для
// независимости пакетов).
const adaptationLine = "- TypeScript/React Native/Expo/Convex/Remotion/E2B: имею опыт fullstack-разработки (PHP/Go + React в CMS Blog); готова оперативно освоить ваш медиа/видео-стек"
