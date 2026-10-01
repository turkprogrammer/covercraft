// Интеграционный прогон АФЛТ-Системс (Go Backend) на живом LLM.
//
// Отличие от live_aeroflot_test.go: там письмо и профиль — КОНСТАНТЫ, а
// проверяется только детерминированный матчер. Здесь письма ещё нет: его
// пишет настоящая модель по личному промпту v4. Это единственный тест,
// который ловит класс багов, невидимый остальным: промпт требует «Protobuf»
// как пробел, модель пишет «имею опыт с Protobuf», матчер верит — и вердикт
// становится выдумкой. Детерминированный матчер такое не проверяет, он
// получает текст на вход и не знает, откуда тот взялся.
//
// ОДИН прогон, а не два. Первая версия этого файла держала два теста, и
// каждый вызывал модель заново: 8 платных вызовов, два РАЗНЫХ письма, и
// второй тест проверял детектор на тексте, который он же и породил. Хуже
// — второй прогон однажды не упомянул Protobuf вовсе, и проверка «нет
// выдумок» прошла вхолостую, ни разу не сработав. Теперь требования
// извлекаются один раз, письмо генерируется один раз, и аудит, фит и
// детектор выдумок работают на ОДНОМ И ТОМ ЖЕ письме.
//
// Ключ НИКОГДА не берётся из исходников: только settings.Load(), тот же
// путь, что у приложения. Провайдер и модель — из ~/.config/covercraft.
// Без настроек тест пропускается, а не падает: в CI ключа нет.
//
// Файл НЕ помечен //go:build live намеренно: с «live» он выпадает из
// компиляции при обычном `go test ./...`, и опечатка дожила бы до
// следующего запуска с ключом в руках.
//
// Но запускаться без спроса он тоже не должен: каждый прогон — это 2
// платных вызова к модели, а `go test ./...` кто-то запускает просто для
// проверки сборки. Поэтому включение явное: CC_LIVE=1. Без него тест
// компилируется (gofmt/vet его видят), но пропускается.
//
// Запуск:
//
//	CC_LIVE=1 go test ./internal/fit/ -run TestAFLTLiveEndToEnd -v -timeout 20m
//
// Флаг -letter=путь к файлу гоняет аудит/фит/детектор на СОХРАНЁННОМ
// письме, без единого вызова модели. Это делает проверку воспроизводимой:
// сегодняшняя выдумка воспроизводится завтра, а не зависит от того, что
// модель нагенерирует в этот раз.
package fit

import (
	"context"
	"flag"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/turkprogrammer/covercraft/internal/audit"
	"github.com/turkprogrammer/covercraft/internal/cover"
	"github.com/turkprogrammer/covercraft/internal/llm"
	"github.com/turkprogrammer/covercraft/internal/settings"
)

// letterFile — путь к сохранённому письму: -letter=/tmp/aflt_letter.md.
// При нём модель не вызывается вообще, прогон целиком детерминирован.
var letterFile = flag.String("letter", "", "прогнать аудит/фит/детектор на сохранённом письме (без вызовов модели)")

// v4PromptPath — личный системный промпт пользователя. Лежит вне
// репозитория намеренно (приватные данные), поэтому тест читает его с
// диска и пропускает себя, если файла нет. Путь переопределяется через
// CC_V4_PROMPT — на другой машине он другой.
const v4PromptPath = "/home/robert/Загрузки/Telegram Desktop/системный_промпт_сопроводительное_письмо_v4.md"

// aeroflotVacancy — реальный текст вакансии АФЛТ-Системс. Хранится здесь
// дословно, включая формулировку «Опыт работы с gRPC, Protobuf, REST-API,
// JSON-RPC, HTTP»: именно на этом перечислении модель решала, разбивать
// его или нет, и разные ответы давали противоположные вердикты.
const aeroflotVacancy = `АФЛТ-Системс – аккредитованная IT-компания в составе ГК "Аэрофлот", которая создает сервисы для авикомпаний и пассажиров. Мы делаем качество обслуживания выше, а перелеты комфортнее и безопаснее.

Обязанности:
    Проведение code review, контроль качества кода. Разработка производительных сервисов: API для web-приложений, интеграционных и служебных модулей.
    Применение потоковой и событийной архитектуры в разработке.
    Построение надёжной, масштабируемой системы на микросервисах.
    Активное участие в оптимизации процессов разработки и их реализации.
    Наставничество и менторство над младшими членами команды. Создание и адаптация метрик, дашбордов и систем мониторинга для сервисов.
    Проработка и реализация архитектурных решений.
    Анализ требований бизнеса, проработка и предложение оптимальных технических решений.
    Написание качественного, масштабируемого кода, а также тестов к нему.
    Предложения по улучшению текущего проекта с использованием новейших технологий и методик.

Требования:
    Минимум 5 лет опыта коммерческой разработки, в том числе веб-сервисов.
    Глубокие знания Golang и его концепций.
    Опыт работы с gRPC, Protobuf, REST-API, JSON-RPC, HTTP.
    Профессиональное владение СУБД Postgres: оптимизация, проектирование, работа под нагрузкой.
    Отличное понимание SOLID, DDD, TDD.
    Ориентированность на результат, командная работа и желание постоянно развиваться.

Будет плюсом:
    Опыт разработки высоконагруженных систем.
    Уверенное владение Python.
    Опыт разработки с использованием микросервисной архитектурой и контейнеризации: docker, k8s.
    Опыт в облачных решениях, таких как AWS, YandexCloud, SberCloud.
    Готовность быстро изучать и адаптироваться к новым технологиям.`

// fabricatedTechs — технологии, которых НЕТ в профиле ни разу, но которые
// стоят в требованиях вакансии. Поэтому модель их называет: она читает
// список «gRPC, Protobuf, REST-API, JSON-RPC, HTTP» и решает, что
// перечисление относится к кандидату.
//
// Protobuf — самая опасная: её нет ни в одном context/*.md, и модель
// достраивает её из знания «gRPC ⇒ Protobuf». Это достройка по аналогии,
// а не утечка справочника, поэтому механизмы против утечек бесполезны —
// помогает только проверка «названо ли слово рядом со словом "опыт"».
var fabricatedTechs = []string{"Protobuf", "JSON-RPC", "JSON RPC", "JSONRPC"}

// liveClient поднимает клиента по настройкам пользователя. Пустая модель —
// это «ключа нет», такой прогон пропускаем, а не считаем ошибкой.
func liveClient(t *testing.T) (llm.Client, bool) {
	t.Helper()
	if os.Getenv("CC_LIVE") != "1" {
		t.Skip("живой прогон выключен: нужен CC_LIVE=1 (платные вызовы к модели)")
	}
	s, err := settings.Load()
	if err != nil {
		t.Skipf("настройки недоступны (%v) — нужен ~/.config/covercraft/settings.json", err)
	}
	if s.Model == "" || s.APIKey == "" {
		t.Skipf("нет модели или ключа (model=%q) — прогон пропущен", s.Model)
	}
	timeout := time.Duration(s.TimeoutSec) * time.Second
	if timeout < 60*time.Second {
		timeout = 180 * time.Second // два LLM-вызова: разбор + письмо
	}
	t.Logf("LIVEGO модель=%s адрес=%s reasoning=%s таймаут=%s", s.Model, s.BaseURL, s.ReasoningEffort, timeout)
	return llm.Client{
		BaseURL: s.BaseURL, APIKey: s.APIKey, Model: s.Model,
		ReasoningEffort: s.ReasoningEffort, Timeout: timeout,
	}, true
}

// readV4 отдаёт личный промпт пользователя.
func readV4(t *testing.T) string {
	t.Helper()
	path := v4PromptPath
	if p := os.Getenv("CC_V4_PROMPT"); p != "" {
		path = p
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("промпт v4 недоступен по %s: %v (задай CC_V4_PROMPT)", path, err)
	}
	if len(raw) == 0 {
		t.Skipf("промпт v4 пуст: %s", path)
	}
	return string(raw)
}

// mentionsTech — встречается ли технология в письме в любом написании.
// JSON-RPC пишут ещё «JSON RPC» и «JSONRPC», поэтому написаний несколько.
func mentionsTech(letter, tech string) bool {
	low := strings.ToLower(letter)
	for _, v := range tokenVariants(tech) {
		if strings.Contains(low, strings.ToLower(v)) {
			return true
		}
	}
	return false
}

// Фраза выдумки = технология названа ОПЫТОМ. Поэтому у клаузы должен быть
// явный глагол опыта. Без этого правила ловятся и не-ложные места:
// заголовок буллета «- Protobuf / JSON-RPC», формулировка «JSON-RPC —
// естественное расширение», строка стека «Стек: Go, gRPC». Все они
// НАЗЫВАЮТ технологию, но ни одну не заявляют как опыт.
//
// Условие двойное: есть глагол опыта и нет отрицания/пробела рядом.
var positiveExperienceRe = regexp.MustCompile(`(?i)опыт|работал|работаю|работает|использ|писал|напис|применял|примен|внедрил|внедр|освоил|осво|знаю|знаком|владею|эксплуатир`)

// experienceClaims — фразы письма, где tech названа ОПЫТОМ. Честный пробел
// («не применял», «готов освоить», «пробел», «нет опыта») опытом не
// считается — по нему письмо как раз и сделано правильно.
func experienceClaims(letter, tech string) []string {
	var claims []string
	for _, line := range strings.Split(letter, "\n") {
		if !mentionsTech(line, tech) {
			continue
		}
		var lineClauses []string
		// Двоеточие НЕ разделитель: «Protobuf: опыт есть» — одна мысль, и
		// глагол опыта стоит в той же клаузе, что и название технологии.
		for _, clause := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || r == '.'
		}) {
			if strings.TrimSpace(clause) != "" {
				lineClauses = append(lineClauses, strings.TrimSpace(clause))
			}
		}
		// Пробел может стоять в соседней клаузе и относиться к tech через
		// местоимение: «Protobuf — следствие gRPC-протокола; отдельно с НИМ
		// не работал». По клаузе такой пробел не виден, и письмо, которое
		// честно раскрыло пробел, было бы помечено как выдумка.
		// Условие строгое: в клаузе с пробелом tech повторяться НЕ должна.
		// Иначе «Опыт с Protobuf есть, с Redis не работал» объявило бы
		// Protobuf честным — а это ровно та ложь, которую детектор ловит.
		backRef := false
		for _, clause := range lineClauses {
			if negatedOrGapped(clause) && gapBackReferenceRe.MatchString(clause) &&
				!mentionsTech(clause, tech) {
				backRef = true
			}
		}
		for _, clause := range lineClauses {
			if !mentionsTech(clause, tech) {
				continue
			}
			if negatedOrGapped(clause) || backRef {
				continue
			}
			// Утверждение об опыте ищется ПОСЛЕ двоеточия. Всё до двоеточия —
			// заголовок буллета, он технологии перечисляет, но ничего о них не
			// утверждает: «Protobuf / JSON-RPC: использую gRPC» называет Protobuf в
			// заголовке, а опыт заявляет про gRPC. Если двоеточия нет, утверждение
			// ищется по всей клаузе.
			assertion, header := clause, ""
			if i := strings.Index(clause, ":"); i >= 0 {
				header, assertion = clause[:i], clause[i+1:]
			}
			// Технология названа ТОЛЬКО в заголовке: строка её не описывает,
			// опыт заявлен про другое.
			if header != "" && mentionsTech(header, tech) && !mentionsTech(assertion, tech) {
				continue
			}
			if !positiveExperienceRe.MatchString(assertion) {
				continue
			}
			claims = append(claims, clause)
		}
	}
	return claims
}

// gapBackReferenceRe — пробел, относящийся к УЖЕ НАЗВАННОЙ технологии через
// местоимение: «с ним не работал», «с ней». Так формулирует ограничитель
// Protobuf из профиля («отдельно с ним не работал»), и на эту формулировку
// опираются и профиль, и этот тест.
//
// Условие строгое: ТОЛЬКО местоимение. «с Redis отдельно не работал» —
// пробел по другой технологии, он не оправдывает опыт с Protobuf в этом же
// буллете. Если разрешить здесь «отдельно», детектор перестанет видеть
// выдумку целиком (регресс TestAFLTDetectorGapDoesNotExcuseOtherTech).
// Без \b: движок Go не считает кириллицу символом \w, и границы слова на
// русском тексте просто не существуют — \bс\s+(ним|ней) не сматчилось бы
// никогда. Пробел вокруг «с» и граница после местоимения проверяются явно.
var gapBackReferenceRe = regexp.MustCompile(`(?i)(^|[^а-яёa-z])с\s+(ним|ней)([^а-яёa-z]|$)`)

// negatedOrGapped — клауза называет пробел или отсутствие опыта, а не опыт.
// negRe — тот же словарь отрицаний, что использует матчер, чтобы тест и
// код смотрели на письмо одинаково.
func negatedOrGapped(clause string) bool {
	return negRe.MatchString(clause) || understandingRe.MatchString(clause)
}

// profileDeclinesTech — профиль называет технологию пробелом, а не опытом
// («в профиле НЕТ», «не работал»). Отдельная копия правила: internal/audit
// держит такое в своём пакете, а тест живёт в internal/fit и лишней
// зависимости не заводит.
var profileDeclinesTech = regexp.MustCompile(`(?i)не работал|не применял|не использовал|опыта нет|нет опыта|в\s+профиле\s+(нет|не)|не\s+интегрировал|не\s+настраивал`)

func profileDeclinesTerm(tech, profile string) bool {
	low := strings.ToLower(profile)
	needle := strings.ToLower(tech)
	for i := 0; ; {
		idx := strings.Index(low[i:], needle)
		if idx < 0 {
			return false
		}
		start := i + idx
		end := start + len(needle)
		// Окно вокруг вхождения: строка целиком плюс соседи по абзацу.
		lo, hi := start-120, end+120
		if lo < 0 {
			lo = 0
		}
		if hi > len(profile) {
			hi = len(profile)
		}
		if profileDeclinesTech.MatchString(profile[lo:hi]) {
			return true
		}
		i = start + len(needle)
	}
}

// savedLetter — сохранённое письмо из -letter=path, если оно задано.
func savedLetter(t *testing.T) (string, bool) {
	t.Helper()
	if *letterFile == "" {
		return "", false
	}
	raw, err := os.ReadFile(*letterFile)
	if err != nil {
		t.Fatalf("письмо из -letter не прочитано: %v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("письмо из -letter пусто: %s", *letterFile)
	}
	return string(raw), true
}

// aeroflotGoldenRequirements — эталонный разбор вакансии АФЛТ для режима
// -letter, где модель не вызывается, а фиту нужны требования.
//
// Это НЕ «ожидаемый вывод модели», а описание вакансии в терминах матчера.
// Оно намеренно хранит ровно те формулировки, из-за которых ломались
// вердикты: «Опыт работы с HTTP» (было unknown из-за стоп-слова http) и
// «Разработка производительных сервисов: API для web-приложений» (было
// ложное missing из-за токена «web»).
func aeroflotGoldenRequirements() Requirements {
	req := func(text, kind string) Requirement {
		return Requirement{Text: text, Kind: kind, Category: "stack"}
	}
	return Requirements{
		Role: "go-primary",
		MustHave: []Requirement{
			req("Опыт работы с gRPC, Protobuf, REST-API, JSON-RPC, HTTP", "must"),
			req("Разработка производительных сервисов: API для web-приложений, интеграционных и служебных модулей", "duty"),
			req("Глубокие знания Golang и его концепций", "must"),
			req("Опыт разработки высоконагруженных распределённых систем", "must"),
			req("Профессиональное владение СУБД Postgres: оптимизация, проектирование, работа под нагрузкой", "must"),
			req("Понимание принципов работы СУБД, умение писать эффективные запросы", "must"),
			req("Опыт работы с микросервисной архитектурой, понимание принципов проектирования и интеграции сервисов", "must"),
			req("Навыки менторства и код-ревью, умение давать обратную связь", "must"),
			req("Владение принципами SOLID, паттернами проектирования", "must"),
			req("Умение проектировать архитектуру решений, исходя из требований бизнеса", "must"),
		},
		NiceToHave: []Requirement{
			req("Умение работать в распределённой команде, используя Agile-подходы", "nice"),
		},
	}
}

// TestAFLTDetector — фикстурная проверка САМИХ детекторов теста.
//
// Пока детекторы не проверены константами, живой прогон не самотестируется:
// утверждение «модель не выдумала Protobuf» верно ровно настолько, насколько
// верно experienceClaims. Если детектор перестанет ловить выдумку, живой
// тест станет зелёным на выдуманном письме и сообщит об успехе.
//
// Здесь нет сети и нет ключа: константы, ответы — фиксированы. Константы
// взяты из живых прогонов: «Protobuf как часть gRPC — опыт есть» и есть
// выдумка, «Protobuf не применял, готов освоить» — честный пробел.
func TestAFLTDetector(t *testing.T) {
	cases := []struct {
		name      string
		letter    string
		tech      string
		wantFound bool
		wantClaim bool
	}{
		{
			name:      "protobuf_как_часть_grpc_выдумка",
			tech:      "Protobuf",
			letter:    "• Микросервисы: gRPC (банк Росгосстрах), Protobuf как часть протокола — опыт есть.",
			wantFound: true,
			wantClaim: true,
		},
		{
			name:      "protobuf_честный_пробел",
			tech:      "Protobuf",
			letter:    "• С Protobuf отдельно не работал, готов освоить; gRPC в микросервисах банка Росгосстрах.",
			wantFound: true,
			wantClaim: false,
		},
		{
			name:      "protobuf_следствие_протокола_не_выдумка",
			tech:      "Protobuf",
			letter:    "• Protobuf — естественное следствие gRPC-протокола; отдельно с ним не работал.",
			wantFound: true,
			wantClaim: false,
		},
		{
			name:      "json_rpc_дефис_пробел",
			tech:      "JSON-RPC",
			letter:    "• С JSON-RPC не работал, готов освоить.",
			wantFound: true,
			wantClaim: false,
		},
		{
			name:      "json_rpc_пробелом_пробел",
			tech:      "JSON RPC",
			letter:    "• С JSON RPC не работал, готов освоить.",
			wantFound: true,
			wantClaim: false,
		},
		{
			name:      "jsonrpc_слитно_выдумка",
			tech:      "JSONRPC",
			letter:    "• Опыт с JSONRPC есть, использовал в интеграциях.",
			wantFound: true,
			wantClaim: true,
		},
		{
			name:      "технологии_в_письме_нет",
			tech:      "Protobuf",
			letter:    "• Go и highload: Stable ID (Kafka, 10 000 RPS), ClickHouse Upsert.",
			wantFound: false,
			wantClaim: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mentionsTech(tc.letter, tc.tech); got != tc.wantFound {
				t.Errorf("mentionsTech(%q) = %v, ожидалось %v", tc.tech, got, tc.wantFound)
			}
			claims := experienceClaims(tc.letter, tc.tech)
			if got := len(claims) > 0; got != tc.wantClaim {
				t.Errorf("experienceClaims(%q) вернули %d фраз %v, ожидалось наличие выдумки=%v",
					tc.tech, len(claims), claims, tc.wantClaim)
			}
		})
	}
}

// TestAFLTDetectorVariantsEqual — все написания одной технологии обязаны
// давать ОДИНАКОВ результат. Иначе выдумка проскальзывает: модель пишет
// «JSON RPC», детектор знает только «JSON-RPC» и молчит, а тест зелёный.
func TestAFLTDetectorVariantsEqual(t *testing.T) {
	for _, group := range [][]string{
		{"JSON-RPC", "JSON RPC", "JSONRPC", "json-rpc"},
		{"gRPC", "grpc"},
		{"PostgreSQL", "Postgres", "postgresql"},
	} {
		for _, v := range group {
			gap := "• С " + v + " не работал, готов освоить."
			if claims := experienceClaims(gap, v); len(claims) != 0 {
				t.Errorf("%q: честный пробел «не работал» засчитан как опыт: %v", v, claims)
			}
			claim := "• Опыт с " + v + " есть, использовал в production."
			if claims := experienceClaims(claim, v); len(claims) == 0 {
				t.Errorf("%q: «опыт есть» не засчитан как выдумка — детектор молчит на лжи", v)
			}
		}
	}
}

// TestAFLTDetectorNegationNotFabrication — слова пробела не должны
// отменяться словами опыта в соседней клаузе. Регресс на разбиение письма
// по запятым: «не работал с MongoDB, использовал Redis в кэше» — это две
// разные технологии, и отрицание первой не относится ко второй.
func TestAFLTDetectorNegationNotFabrication(t *testing.T) {
	letter := "• С MongoDB не работал, использовал Redis в кэше."
	if claims := experienceClaims(letter, "MongoDB"); len(claims) != 0 {
		t.Errorf("честный пробел MongoDB засчитан как опыт: %v", claims)
	}
	if claims := experienceClaims(letter, "Redis"); len(claims) != 1 {
		t.Errorf("Redis заявлен как факт — детектор должен его поймать, а вернул %v", claims)
	}
}

// TestAFLTDetectorGapDoesNotExcuseOtherTech — пробел по ОДНОЙ технологии не
// оправдывает заявленный опыт по ДРУГОЙ в той же строке. Регресс на
// механизм «пробел через местоимение»: он не должен становиться общим
// амнистией на весь буллет, иначе выдумка проходит под видом пробела
// соседней технологии.
func TestAFLTDetectorGapDoesNotExcuseOtherTech(t *testing.T) {
	letter := "• Опыт с Protobuf есть; с Redis отдельно не работал."
	if claims := experienceClaims(letter, "Protobuf"); len(claims) == 0 {
		t.Error("пробел по Redis оправдал заявленный опыт с Protobuf — детектор ослеблён")
	}
	if claims := experienceClaims(letter, "Redis"); len(claims) != 0 {
		t.Errorf("честный пробел Redis засчитан как опыт: %v", claims)
	}
}

// TestAFLTLiveEndToEnd — полный конвейер, ровно как в UI:
// разбор вакансии → письмо по v4 → вердикт → аудит → проверка выдумок.
// Всё на ОДНОМ письме: отдельного прогона ради детектора больше нет.
//
// С флагом -letter=path модель не вызывается: письмо берётся из файла, и
// весь конвейер становится детерминированным. Это и есть воспроизводимый
// вердикт — один и тот же текст всегда даёт один и тот же результат.
//
// Утверждений намеренно мало. LLM недетерминирован: любая жёсткая проверка
// текста замигает при первом же прогоне и станет мигающей всегда. Тест ценен
// отчётами — их читает человек. Утверждения ниже ловят только то, что
// ломается ВСЕГДА, а не то, что модель пишет сегодня иначе.
func TestAFLTLiveEndToEnd(t *testing.T) {
	profile := LoadProfile("../../context", nil)
	if strings.TrimSpace(profile) == "" {
		t.Skip("профиль недоступен: нужен каталог ../../context")
	}

	// Технология не должна быть ЗАЯВЛЕНА в профиле как опыт. Называться в
	// профиле она обязана — иначе письмо не сможет честно написать «не
	// работал». Поэтому проверяем не вхождение слова, а его характер:
	// профильDeclinesTerm ищет рядом отрицание/пробел («отдельно с ним не
	// работал») — именно так выглядит ограничитель в разделе 13 профиля.
	// Если профиль ЗАЯВЛЯЕТ опыт, тест проверяет пустое и обязан молчать
	// об этом (Fatal), а не искать выдумку.
	profileLow := strings.ToLower(profile)
	for _, tech := range fabricatedTechs {
		if !strings.Contains(profileLow, strings.ToLower(tech)) {
			continue
		}
		if profileDeclinesTerm(tech, profile) {
			t.Logf("LIVEGO профиль называет %q пробелом — выдумка проверяется честно", tech)
			continue
		}
		t.Fatalf("тест устарел: профиль заявляет опыт с %q — он больше не проверяет выдумку", tech)
	}

	var letter string
	var reqs Requirements

	if saved, ok := savedLetter(t); ok {
		// Режим воспроизведения: письмо сохранено, модель не нужна. Разбор
		// вакансии тоже не выполняем — это был бы платный вызов; вместо него
		// берётся эталонный набор требований с теми же формулировками.
		letter = saved
		reqs = aeroflotGoldenRequirements()
		t.Logf("LIVEGO письмо из -letter=%s, модель не вызывается", *letterFile)
	} else {
		c, ok := liveClient(t)
		if !ok {
			return
		}
		v4 := readV4(t)
		fn := LLMFunc(func(ctx context.Context, system, user string) (string, error) {
			return c.Generate(ctx, system, user)
		})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		// Шаг 1. Разбор вакансии на требования — ОДИН вызов.
		var err error
		reqs, err = ExtractRequirements(ctx, fn, aeroflotVacancy)
		if err != nil {
			t.Fatalf("разбор вакансии: %v", err)
		}
		t.Logf("LIVEGO role=%s требований: must=%d nice=%d soft=%d",
			reqs.Role, len(reqs.MustHave), len(reqs.NiceToHave), len(reqs.Soft))
		for _, m := range reqs.MustHave {
			t.Logf("LIVEGO must: %s", m.Text)
		}

		// Шаг 2. Письмо: v4 уходит в system, user-часть собирает cover.
		var musts []string
		for _, m := range reqs.MustHave {
			musts = append(musts, m.Text)
		}
		letter, err = c.Generate(ctx, v4, cover.BuildUserPrompt("../../context", aeroflotVacancy, musts, nil))
		if err != nil {
			t.Fatalf("генерация письма: %v", err)
		}
	}

	words := len(strings.Fields(letter))
	t.Logf("LIVEGO письмо: %d слов\n-----\n%s\n-----", words, letter)

	// Шаг 3. Вердикт детерминированного матчера — на ЭТОМ ЖЕ письме.
	f := Evaluate(DefaultConcepts(), reqs, profile, letter, aeroflotVacancy)
	t.Logf("LIVEGO ВЕРДИКТ=%s ОЦЕНКА=%d", f.Verdict, f.Score)
	for _, r := range f.Covered {
		t.Logf("LIVEGO ОК    [%s] %s :: %s", r.Source, r.Text, r.Note)
	}
	for _, r := range f.Caveats {
		t.Logf("LIVEGO ОГОВ [%s] %s :: %s", r.Source, r.Text, r.Note)
	}
	for _, r := range f.Missing {
		t.Logf("LIVEGO ПРОБЕЛ [%s/%s] %s :: %s", r.Source, r.Kind, r.Text, r.Note)
	}
	for _, a := range f.Advice {
		t.Logf("LIVEGO СОВЕТ: %s", a)
	}

	// Шаг 4. Аудит письма — тоже на этом же письме.
	for _, w := range audit.CheckProfile(letter, profile).Warnings {
		t.Logf("LIVEGO AUDIT-ПРОФИЛЬ: %s", w)
	}
	for _, w := range audit.Check(letter, aeroflotVacancy).Warnings {
		t.Logf("LIVEGO AUDIT-ВАКАНСИЯ: %s", w)
	}

	// Шаг 5. Детектор выдумок — на этом же письме, а не на отдельном прогоне.
	for _, tech := range fabricatedTechs {
		if !mentionsTech(letter, tech) {
			t.Logf("LIVEGO %s в письме не упомянут — это тоже честный вариант", tech)
			continue
		}
		for _, claim := range experienceClaims(letter, tech) {
			t.Errorf("ВЫДУМКА: %s назван опытом в фразе %q — в профиле его нет", tech, claim)
		}
	}

	// Кросс-проверка: если письмо всё-таки написало «опыт с Protobuf», то и
	// матчер не должен считать это закрытым требованием. Расхождение двух
	// источников — сам по себе сигнал о баге.
	for _, r := range f.Covered {
		if strings.Contains(r.Text, "Protobuf") && r.Source == SrcLetter {
			t.Logf("LIVEGO ВНИМАНИЕ: Protobuf помечен как закрыто письмом, проверьте формулировку: %q", r.Note)
		}
	}

	// --- утверждения: только то, что ломается всегда -------------------

	// Письмо должно быть, а не пустота и не эхо профиля. Модель, упавшая в
	// повтор промпта, вернёт инструкции — это ловится, а не оценивается.
	if words < 40 {
		t.Errorf("письмо из %d слов — модель не отработала или вернула обрывок", words)
	}
	if len(f.Covered)+len(f.Caveats)+len(f.Missing) == 0 {
		t.Errorf("матчер не разобрал ни одного требования: %+v", reqs)
	}
	if f.Verdict == "" {
		t.Errorf("пустой вердикт при %d требованиях", len(reqs.MustHave))
	}
}
