package fit

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// cacheSize — число записей в wordReCache: нужно, чтобы оценить рост кэша
// от пользовательского ввода (ключи приходят из reqTokens(текст вакансии)).
// resetWordReCache — сброс кэша и счётчика. Счётчик жив отдельно от sync.Map,
// поэтому оба чистятся вместе.
func resetWordReCache() {
	wordReCache.Range(func(k, _ any) bool { wordReCache.Delete(k); return true })
	wordReCacheSize.Store(0)
}

func cacheSize() int {
	n := 0
	wordReCache.Range(func(_, _ any) bool { n++; return true })
	return n
}

// BenchmarkWordReCacheGrowth — сколько записей накапливает кэш на realistic
// и на adversarial вакансии. Второй случай — длинная вакансия с тысячами
// уникальных латинских токенов: это верхняя граница роста.
func BenchmarkWordReCacheGrowth(b *testing.B) {
	real := "Требуется senior backend разработчик: Go, PostgreSQL, Kafka, Redis, " +
		"Kubernetes, Docker, gRPC, REST API, микросервисы, CI/CD, Terraform, " +
		"Prometheus, Grafana, RabbitMQ, ClickHouse, Kafka Streams, Helm, Ansible"

	b.Run("realistic/10vacancies", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for v := 0; v < 10; v++ {
				text := real + fmt.Sprintf(" вакансия номер %d с дополнительными словами tech%d stack%d", v, v, v)
				for _, t := range reqTokens(text) {
					for _, clause := range strings.Split(real, ", ") {
						findText(t, clause)
					}
				}
			}
		}
	})
	b.Run("adversarial/unique", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var sb strings.Builder
			sb.WriteString(real)
			for v := 0; v < 2000; v++ {
				fmt.Fprintf(&sb, " techname%d framework%d", v, v)
			}
			text := sb.String()
			for _, t := range reqTokens(text) {
				findText(t, real)
			}
		}
	})
}

// TestWordReCacheGrowthIsBoundedByTokens — фиксирует фактическую ёмкость кэша
// на реальной вакансии. Ключ — вариант написания токена, поэтому рост
// ограничен числом уникальных токенов, а не числом запросов: повторные
// генерации тех же вакансий кэш не растят.
func TestWordReCacheGrowthIsBoundedByTokens(t *testing.T) {
	resetWordReCache()
	vacancy := "Требуется senior backend разработчик: Go, PostgreSQL, Kafka, Redis, " +
		"Kubernetes, Docker, gRPC, REST API, микросервисы, CI/CD, Terraform"

	tokens := reqTokens(vacancy)
	for round := 0; round < 50; round++ { // 50 «пользователей» одной вакансии
		for _, tok := range tokens {
			findText(tok, vacancy)
		}
	}
	first := cacheSize()

	for round := 0; round < 50; round++ {
		for _, tok := range tokens {
			findText(tok, vacancy)
		}
	}
	second := cacheSize()

	if first != second {
		t.Errorf("кэш растёт на повторных прогонах той же вакансии: %d → %d", first, second)
	}
	t.Logf("уникальных токенов в вакансии: %d, записей в кэше после 100 прогонов: %d", len(tokens), second)
	if second > 4096 {
		t.Errorf("кэш вырос до %d записей на одной вакансии — нужен лимит", second)
	}
}

// TestWordReCacheConcurrentSafe — wordReCache обязан быть безопасен при
// параллельных запросах: сервер обслуживает SSE-потоки одновременно.
func TestWordReCacheConcurrentSafe(t *testing.T) {
	resetWordReCache()
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			text := fmt.Sprintf("backend разработчик Go Kafka PostgreSQL worker-%d", g)
			for i := 0; i < 200; i++ {
				for _, tok := range reqTokens(text) {
					findText(tok, text)
				}
			}
		}(g)
	}
	wg.Wait()
	t.Logf("записей в кэше после 32 конкурентных горутин: %d", cacheSize())
}

// TestWordReCacheRespectsCap — потолок держится. Подаём заведомо больше
// ключей, чем потолок (как вакансия от пользователя), и ждём, что рост
// прекратится ровно на wordReCacheMax.
//
// Обращаемся к wordRe напрямую, а не через findText: findText гоняет каждый
// токен по всему тексту вакансии, и на 8 000 слов это квадратично (тест шёл
// 23 секунды). Потолок держит wordRe, его и проверяем.
func TestWordReCacheRespectsCap(t *testing.T) {
	resetWordReCache()
	offered := wordReCacheMax * 2
	for v := 0; v < offered; v++ {
		wordRe(fmt.Sprintf("techname%d", v))
	}
	got := cacheSize()
	if got > wordReCacheMax {
		t.Errorf("кэш вырос до %d записей при потолке %d", got, wordReCacheMax)
	}
	if got != wordReCacheMax {
		t.Errorf("ожидалось заполнение до потолка %d, получено %d", wordReCacheMax, got)
	}
	t.Logf("ключей подано: %d, записей в кэше: %d (потолок %d)", offered, got, wordReCacheMax)
}

// TestWordReCacheStillCachesAfterCap — переполнение кэша не ломает
// корректность: после достижения потолка набор токенов всё ещё работает.
func TestWordReCacheStillCachesAfterCap(t *testing.T) {
	resetWordReCache()
	for v := 0; v < wordReCacheMax+500; v++ {
		wordRe(fmt.Sprintf("filler%d", v))
	}
	if !findText("kafka", "мы используем Kafka и Postgres") {
		t.Error("после переполнения кэша поиск токена перестал работать")
	}
	if findText("kafka", "только Postgres и Redis") {
		t.Error("после переполнения кэха ложное срабатывание")
	}
}
