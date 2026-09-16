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
- ничего не выдумывай: если требования нет в тексте вакансии — его нет в JSON.`
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
		r.MustHave[i].Kind = "must"
	}
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
