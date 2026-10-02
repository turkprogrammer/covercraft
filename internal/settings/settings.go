// Package settings хранит настройки CoverCraft в JSON-файле
// ~/.config/covercraft/settings.json с правами 0600 (внутри API-ключ).
//
// Почему не LocalStorage из плана: WebView открывает сервер на случайном
// порту, origin меняется при каждом запуске, и LocalStorage теряет всё.
// Go — единственный надёжный владелец настроек.
//
// SystemPrompt — НЕ настройка: кастомный промпт живёт только в текущей
// сессии UI, при каждом запуске восстанавливается DefaultSystemPrompt.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultSystemPrompt — прямая инструкция модели. Всегда восстанавливается
// при запуске; кастомный промпт пользователь вводит в текущей сессии.
const DefaultSystemPrompt = `Ты пишешь сопроводительное письмо к отклику на вакансию.

Правила:
- Язык письма — по языку вакансии, лаконично и по делу.
- Опирайся только на факты из профиля и описания вакансии. Ничего не выдумывай: если о чём-то нет данных — пропусти это.
- Структура: приветствие → чем я подхожу (маркированный список: каждое обязательное требование вакансии — отдельный пункт с фактом из профиля) → короткое закрытие с призывом к диалогу.
- Обязательные требования вакансии (must-have) — приоритет письма. Объём — до 200 слов; если не влезает, сокращай второстепенные факты, а не требования. При 6 и более must-have допустимо до 300 слов: сокращать требования — хуже, чем выйти за лимит.
- Секция «Адаптация под ваш стек» обязательна, но это про переносимость на чужой стек, а не второй проход по своему опыту: не пересказывай там факты из буллетов (PostgreSQL, Docker Compose, systemd уже описаны выше) — только то, что в вакансии есть, а у тебя нет.
- Один призыв к диалогу в конце. Не добавляй закрытие дважды.
- Пробел — не слабость: называй его как мост на ближайший релевантный факт.
- Подпись: имя бери только из профиля; если имени в профиле нет — подпиши «С уважением,» без имени. Выдуманное имя = фабрикация.`

// Settings — всё, что пользователь настраивает в UI. SystemPrompt сюда
// не входит: он не персистится и всегда сбрасывается к дефолту.
type Settings struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
	// ReasoningEffort: "", "none", "low", "medium", "high". "none" — для
	// reasoning-моделей (glm-5.3 и др.), которые иначе думают минутами.
	ReasoningEffort string `json:"reasoningEffort"`
	// TimeoutSec — таймаут генерации в секундах (5–900). 60 по умолчанию:
	// free-tier не должен висеть; медленным моделям можно дать больше.
	TimeoutSec int `json:"timeoutSec"`
	// FitEngine — движок матчинга фита: "" (или "deterministic") — матчер на
	// правилах: быстро, ноль токенов; "llm" — гибрид: модель размечает
	// покрытие письма, код проверяет цитаты и отрицания и считает вердикт.
	FitEngine string `json:"fitEngine"`
}

// Defaults — значения для первого запуска. Model пуста: имя модели зависит от
// провайдера (llama3.2 у Ollama, gpt-4o-mini у OpenAI), его задаёт пользователь.
var Defaults = Settings{
	BaseURL:    "http://127.0.0.1:11434/v1",
	Model:      "",
	TimeoutSec: 60,
}

// path возвращает абсолютный путь к файлу настроек.
func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "covercraft", "settings.json"), nil
}

// Load читает настройки; если файла нет — возвращает Defaults.
// Битый JSON — ошибка (пользователю лучше увидеть её, чем тихо потерять ключ).
func Load() (Settings, error) {
	s := Defaults
	p, err := path()
	if err != nil {
		return s, err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, fmt.Errorf("не удалось разобрать %s: %w", p, err)
	}
	return s, nil
}

// Save атомарно записывает настройки с правами 0600.
func Save(s Settings) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
