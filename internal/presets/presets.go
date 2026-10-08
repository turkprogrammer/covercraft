// Package presets хранит пользовательские пресеты системного промпта
// в JSON-файле ~/.config/covercraft/presets.json с правами 0600.
//
// Пресет — именованный шаблон промпта под конкретную цель (письмо,
// ответы рекрутеру, свои заготовки). Он переживает рестарт, в отличие от
// поля #systemPrompt: активный пресет при запуске копируется в поле и
// дальше живёт сессионной копией — правки не уходят в пресет без
// явного сохранения (кнопка save preset).
//
// Дефолтный промпт пресетом НЕ является: settings.DefaultSystemPrompt
// остаётся отдельной сущностью, с нею работают resolveSystemPrompt и
// reset default. Пресеты — чисто пользовательский слой поверх.
//
// Хранение устроено как в package settings: атомарная запись .tmp +
// rename, битый JSON — ошибка (тихая потеря заготовок хуже явной ошибки).
package presets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Лимиты пресета. Имя — короткая метка в селекторе. Длина промпта НЕ
// ограничена: поле #systemPrompt не ограничено, пресет — его зеркало, и
// лимит здесь блокировал бы легитимные большие промпты (v4 — 98 КБ).
// Защита от «вакансии в поле» живёт в sentinfo (предупреждение при ≥8 КБ
// и ≥4× дефолта) и от длины пресета не зависит.
const MaxNameRunes = 80

// PresetMode — режим выполнения пресета. Два режима: полный пайплайн
// письма (compose + fit + audit) и режим ответа на вопросы (главий чат,
// fit/audit отключены).
type PresetMode string

const (
	// ModeCoverLetter — полный пайплайн: compose, fit, audit, аудит.
	ModeCoverLetter PresetMode = "cover_letter"
	// ModeQA — без извлечения требований и без fit/audit: ответ рекрутеру.
	ModeQA PresetMode = "qa"
)

// Preset — один именованный шаблон системного промпта.
type Preset struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	SystemPrompt string     `json:"systemPrompt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	Mode         PresetMode `json:"mode"`
}

// Store — содержимое presets.json. ActivePresetID — какой пресет
// копируется в поле при запуске; пусто — кастомный промпт, привязки нет.
type Store struct {
	Presets        []Preset `json:"presets"`
	ActivePresetID string   `json:"activePresetId"`
}

// path возвращает абсолютный путь к файлу пресетов.
func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "covercraft", "presets.json"), nil
}

// Load читает пресеты; если файла нет — пустой Store (пользователь
// создаёт первым пресетом сам). Битый JSON — ошибка. Пресеты без поля
// mode (старые файлы) считаются cover_letter, так что уже созданные
// шаблоны не сломаются.
func Load() (Store, error) {
	s := Store{}
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
		return Store{}, fmt.Errorf("не удалось разобрать %s: %w", p, err)
	}
	if s.Presets == nil {
		s.Presets = []Preset{}
	}
	for i := range s.Presets {
		if s.Presets[i].Mode == "" {
			s.Presets[i].Mode = ModeCoverLetter
		}
	}
	return s, nil
}

// Save атомарно записывает пресеты с правами 0600.
func Save(s Store) error {
	p, err := path()
	if err != nil {
		return err
	}
	if s.Presets == nil {
		s.Presets = []Preset{}
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

// NewID — случайный 16-байтовый hex-идентификатор. crypto/rand вместо
// uuid-библиотеки: проект только на stdlib.
func NewID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Validate — проверка содержимого пресета перед записью. Имя 1..80 рун,
// промпт — непустой (длина не ограничена, см. MaxNameRunes). Ошибки —
// на русском, они уходят в UI как есть.
func Validate(name, systemPrompt string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("имя пресета пусто")
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return fmt.Errorf("имя пресета длиннее %d символов", MaxNameRunes)
	}
	if strings.TrimSpace(systemPrompt) == "" {
		return errors.New("промпт пресета пуст")
	}
	return nil
}

// Get — пресет по идентификатору; false, если нет.
func (s Store) Get(id string) (Preset, bool) {
	for _, p := range s.Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
