package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setConfigDir направляет os.UserConfigDir во временную папку теста.
func setConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestDefaultSystemPromptIsDirect(t *testing.T) {
	// Дефолтный промпт — прямая инструкция, а не расплывчатая фраза.
	p := DefaultSystemPrompt
	for _, want := range []string{"сопроводительное письмо", "Язык письма", "выдумывай"} {
		if !strings.Contains(p, want) {
			t.Errorf("дефолтный промпт должен содержать %q:\n%s", want, p)
		}
	}
}

func TestLoadReturnsDefaultsWhenNoFile(t *testing.T) {
	setConfigDir(t)

	s, err := Load()
	if err != nil {
		t.Fatalf("Load без файла не должен ошибаться: %v", err)
	}
	if s.BaseURL == "" {
		t.Error("BaseURL должен иметь значение по умолчанию")
	}
	// Модель по умолчанию пуста — имя зависит от провайдера
	// (llama3.2 у Ollama, gpt-4o-mini у OpenAI), его задаёт пользователь.
	if s.Model != "" {
		t.Errorf("Model по умолчанию должен быть пуст, got %q", s.Model)
	}
	// Таймаут генерации по умолчанию — 60 с: free-tier не должен висеть.
	if s.TimeoutSec != 60 {
		t.Errorf("TimeoutSec по умолчанию = %d, хочу 60", s.TimeoutSec)
	}
	// SystemPrompt — НЕ настройка: всегда восстанавливается дефолт.
	if s.ReasoningEffort != "" && s.BaseURL == "" {
		t.Error("не относится к SystemPrompt — просто защита от случайных полей")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := setConfigDir(t)
	want := Settings{
		BaseURL:         "http://127.0.0.1:18999/v1",
		APIKey:          "sk-test",
		Model:           "test-model",
		ReasoningEffort: "none",
		TimeoutSec:      90,
	}

	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Файл лежит по XDG-пути с правами 0600 (там лежит API-ключ).
	p := filepath.Join(dir, "covercraft", "settings.json")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("файл настроек не создан: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("права файла %v, хочу 0600", fi.Mode().Perm())
	}
	// Промпт не должен попадать на диск.
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "systemPrompt") {
		t.Errorf("systemPrompt не должен сохраняться на диск: %s", raw)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load после Save: %v", err)
	}
	if got != want {
		t.Errorf("roundtrip: got %+v, want %+v", got, want)
	}
}

func TestLoadBrokenJSONReturnsError(t *testing.T) {
	dir := setConfigDir(t)
	p := filepath.Join(dir, "covercraft")
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "settings.json"), []byte("{битый"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Error("битый JSON должен давать ошибку, а не тихие дефолты")
	}
}
