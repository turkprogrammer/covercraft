package presets

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadWithoutFile — первый запуск: файла нет, ошибка нет, список пуст.
func TestLoadWithoutFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := Load()
	if err != nil {
		t.Fatalf("Load без файла: %v", err)
	}
	if len(s.Presets) != 0 || s.ActivePresetID != "" {
		t.Errorf("хочу пустой store, получено %+v", s)
	}
}

// TestSaveLoadRoundtrip — запись и обратное чтение без потери полей.
func TestSaveLoadRoundtrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := Store{
		Presets: []Preset{{
			ID:           "aabbccdd",
			Name:         "Ответ рекрутеру",
			SystemPrompt: "Ты отвечаешь на вопросы рекрутера.",
		}},
		ActivePresetID: "aabbccdd",
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Presets) != 1 {
		t.Fatalf("пресетов: %d, хочу 1", len(got.Presets))
	}
	if got.Presets[0].Name != want.Presets[0].Name ||
		got.Presets[0].SystemPrompt != want.Presets[0].SystemPrompt ||
		got.ActivePresetID != want.ActivePresetID {
		t.Errorf("roundtrip потерял данные: %+v", got)
	}
}

// TestSavePermissions — файл пресетов, как и настройки, 0600: рядом API-ключ
// в settings.json, пресеты не должны быть читаемы другими.
func TestSavePermissions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Store{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	p, err := path()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("права %o, хочу 600", fi.Mode().Perm())
	}
}

// TestLoadBrokenJSON — битый файл = явная ошибка, а не тихо пустой store:
// молчаливое «потерялись все заготовки» хуже любого отказа.
func TestLoadBrokenJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p, err := path()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte("{битый"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(); err == nil {
		t.Error("битый JSON должен давать ошибку")
	}
}

// TestValidate — границы: пустое/длинное имя, пустой промпт. Длина
// промпта не ограничена (поле #systemPrompt не ограничено — v4 на 98 КБ
// обязан помещаться), поэтому огромный текст здесь валиден.
func TestValidate(t *testing.T) {
	longName := strings.Repeat("ы", MaxNameRunes+1)
	hugePrompt := strings.Repeat("ы", 60000) // ~120 КБ в байтах — валидно
	for _, tc := range []struct {
		name, prompt string
		wantErr      bool
	}{
		{"Письмо", "Ты пишешь письмо.", false},
		{"  Письмо  ", "Ты пишешь письмо.", false}, // пробелы схлопываются
		{"", "Ты пишешь письмо.", true},
		{"   ", "Ты пишешь письмо.", true},
		{longName, "Ты пишешь письмо.", true},
		{"Q&A", "", true},
		{"Q&A", "   ", true},
		{"v4", hugePrompt, false},
	} {
		err := Validate(tc.name, tc.prompt)
		if (err != nil) != tc.wantErr {
			t.Errorf("Validate(%q, %.20q…) = %v, wantErr %v", tc.name, tc.prompt, err, tc.wantErr)
		}
	}
}

// TestNewID — 32 hex-символа (16 байт) и уникальность: на идентификаторе
// держится удаление и активация пресета.
func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if len(id) != 32 {
			t.Fatalf("длина %d, хочу 32", len(id))
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatalf("ID не hex: %v", err)
		}
		if seen[id] {
			t.Fatalf("ID повторился: %s", id)
		}
		seen[id] = true
	}
}

// TestGet — поиск по ID: найденный возвращается, чужой — false.
func TestGet(t *testing.T) {
	s := Store{Presets: []Preset{{ID: "x", Name: "A"}}}
	if p, ok := s.Get("x"); !ok || p.Name != "A" {
		t.Errorf("Get(x) = %+v, %v; хочу A, true", p, ok)
	}
	if _, ok := s.Get("нет"); ok {
		t.Error("Get(нет) не должен находить")
	}
}
