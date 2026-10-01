// Извлечение структуры требований из вакансии — LLM-шаг фита.
//
// Свободный текст вакансии регэкспами на must-have/nice-to-have не
// разбирается, поэтому единственное, что делает LLM в фите, — извлечение
// структуры. Всё остальное (матчинг, скор, вердикт) — детерминированный
// код в fit.go и воспроизводимо при одном и том же разборе.
package fit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// LLMFunc — шов для тестов: не-стриминговый вызов модели. Тот же
// контракт, что и server.LLMFunc (конвертация типов, без цикла импорта).
type LLMFunc func(ctx context.Context, system, user string) (string, error)

func ExtractPrompt(vacancy string) (system, user string) {
	system = `Ты — парсер вакансий. Разбери текст вакансии на структуру требований.
Верни ТОЛЬКО JSON без markdown-обёрток и пояснений, по схеме:
{"role":"<go-primary|php-primary|ml-research|fullstack|other>","mustHave":[{"text":"...","kind":"must","category":"stack|domain|metric|role|other"}],"niceToHave":[{"text":"...","kind":"nice","category":"..."}],"soft":[{"text":"...","kind":"soft","category":"other"}]}
Правила:
- mustHave — только обязательные требования ("опыт с X обязателен", "не менее N лет");
- niceToHave — "будет плюсом", "желательно";
- soft — качества личности (самоорганизованность, темп, стрессоустойчивость);
- text — короткая формулировка требования, 1 строка, без объяснений;
- НЕ ТЕряй продуктовые блоки из обязанностей: если в обязанностях названы
  платёжные шлюзы, подписки, веб-воронки, онбординг, мобильной/веб-интерфейс,
  аналитика — вынеси это в mustHave отдельным требованием с kind:"duty",
  даже если в блоке «Требования» такой строки нет: иначе пробел не виден.
  Отличие duty от must важно: обязанность — не обязательное требование, и две
  незакрытые обязанности не должны давать вердикт «не откликаться»
  (их пробел показывается отдельной строкой совета);
- ТРЕБОВАНИЯ-АЛЬТЕРНАТИВЫ РАЗБИВАЙ. «Экспертиза в одном из двух направлений:
  Media/Video … или AI Agents …» — это ДВА отдельных требования, а не одно.
  Схлопывать их нельзя: закрытие по одному направлению прячет пробел по
  другому, и пробел пропадает из вердикта совсем;
- языковые требования («Professional working level English», «Russian —
  minimum A1», «speak English», «B2+») — в soft, не в mustHave: это качество
  общения, а не технология; оно не должно ронять вердикт до skip. НО
  формулировку сохрани дословно с уровнем («Английский B2+»), иначе в вердикте
  не видно, ЧТО именно проверяется;
- ничего не выдумывай: если требования нет в тексте вакансии — его нет в JSON;
- выпиши ВСЕ обязательные требования: «Требования» плюс обязательные
  обязанности. Отбрасывать требование только потому, что оно «мягко
  сформулировано», нельзя — это и есть смысл фита.`
	user = vacancy
	return system, user
}

// ParseExtraction разбирает ответ модели на требования. Терпима к
// markdown-обёртке ```json ... ``` и мусору вокруг: модель возвращает
// не всегда чистый JSON.
func ParseExtraction(raw string) (Requirements, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return Requirements{}, errors.New("в ответе модели нет JSON")
	}
	var r Requirements
	if err := json.Unmarshal([]byte(raw[start:end+1]), &r); err != nil {
		return Requirements{}, fmt.Errorf("битый JSON разбора вакансии: %w", err)
	}
	// kind нормализуем по корзине: модель иногда путает теги.
	for i := range r.MustHave {
		// "duty" сохраняем: обязанность из блока обязанностей не должна
		// считаться обязательным требованием в skip-пороге (октябрь 2026).
		if r.MustHave[i].Kind != "duty" {
			r.MustHave[i].Kind = "must"
		}
	}
	r.MustHave = expandConjunctiveLists(r.MustHave)
	for i := range r.NiceToHave {
		r.NiceToHave[i].Kind = "nice"
	}
	for i := range r.Soft {
		r.Soft[i].Kind = "soft"
	}
	return r, nil
}

// ExtractRequirements — шаг 1 фита: LLM разбирает вакансию на требования.
// Возвращает пустой Requirements с ошибкой, если разбор не удался.
// Чистая функция от шва LLMFunc — тестируется с fake-функцией.
func ExtractRequirements(ctx context.Context, fn LLMFunc, vacancy string) (Requirements, error) {
	if fn == nil {
		return Requirements{}, errors.New("нет LLM-функции")
	}
	system, user := ExtractPrompt(vacancy)
	raw, err := fn(ctx, system, user)
	if err != nil {
		return Requirements{}, err
	}
	return ParseExtraction(raw)
}

// conjListRe — хвост требования вида «…, gRPC, Protobuf, REST-API, JSON-RPC,
// HTTP». Первая группа — кириллический префикс («Опыт работы с»), вторая —
// сам перечень. Требование не содержит ни одного признака, что это
// альтернативы («или», скобки, слэш), — значит это конъюнкция: каждая
// технология нужна.
//
// Разные модели разбирали такую строку по-разному: одна отдавала её пятью
// отдельными must-have (Protobuf и REST-API выпадали в «не закрыто» и роняли
// вердикт в «не откликаться»), другая — одним требованием (и оно закрывалось
// по большинству токенов, хотя письмо честно называло Protobuf пробелом).
// Одни и те же данные давали противоположные вердикты. Разбиение выполняет
// код, поэтому результат воспроизводим.
//
// Префикс обязан быть непустым и оканчиваться кириллицей: строка «Kafka,
// PostgreSQL, Redis» — это описание роли, а не перечень внутри требования.
var conjListRe = regexp.MustCompile(`(?i)^(.*[а-яё]\s+)([A-Za-z][A-Za-z0-9+#.-]*(?:\s+[A-Za-z][A-Za-z0-9+#.-]*)*(?:\s*,\s*[A-Za-z][A-Za-z0-9+#.-]*(?:\s+[A-Za-z][A-Za-z0-9+#.-]*)*){2,})$`)

// Разбиваются только списки из ТРЁХ и более технологий: короткие перечисления
// («Kafka, PostgreSQL») в требованиях встречаются как описание роли, и их
// разбиение добавило бы шум без выигрыша в честности.
func expandConjunctiveLists(in []Requirement) []Requirement {
	var out []Requirement
	for _, r := range in {
		m := conjListRe.FindStringSubmatch(strings.TrimSpace(r.Text))
		if m == nil {
			out = append(out, r)
			continue
		}
		prefix := strings.TrimSpace(m[1])
		for _, item := range strings.Split(m[2], ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			text := item
			if prefix != "" {
				text = prefix + " " + item
			}
			out = append(out, Requirement{Text: text, Kind: r.Kind, Category: r.Category})
		}
	}
	return out
}
