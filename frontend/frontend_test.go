package frontend

import (
	"strings"
	"testing"
)

func TestIndexHTMLIsCompleteDocument(t *testing.T) {
	if !strings.Contains(IndexHTML, "<!DOCTYPE html>") {
		t.Error("index.html должен начинаться с <!DOCTYPE html>")
	}
	if !strings.Contains(IndexHTML, "</html>") {
		t.Error("index.html должен закрываться </html>")
	}
	if !strings.Contains(IndexHTML, "CoverCraft") {
		t.Error("в UI должно фигурировать имя CoverCraft")
	}
}

func TestFooterCopyright(t *testing.T) {
	// Авторская подпись в футере: компактная, но полная.
	for _, want := range []string{
		"Developed:",
		"Robert Yusupov",
		"https://github.com/turkprogrammer",
		"https://yusupov-tech.ru/",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в футере должен быть копирайт: не найдено %q", want)
		}
	}
}

func TestReasoningEffortSelect(t *testing.T) {
	// reasoning-модели (glm-5.3): без выбора усилия думают минутами.
	for _, want := range []string{
		`id="reasoningEffort"`,
		`value="none"`,
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет селекта reasoning effort: не найдено %q", want)
		}
	}
}

func TestGenerateShowsLiveTimer(t *testing.T) {
	// Медленный free-tier выглядит «зависшим» без обратной связи:
	// во время генерации UI тикает секундами.
	for _, want := range []string{
		"liveTimer", // интервал, обновляющий статус
		"сек",       // подпись рядом с тикающим числом
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет живого счётчика времени генерации: не найдено %q", want)
		}
	}
}

func TestTimeoutField(t *testing.T) {
	// Таймаут генерации настраивается из UI (дефолт 60 с).
	if !strings.Contains(IndexHTML, `id="timeoutSec"`) {
		t.Error("в UI нет поля timeoutSec")
	}
}

func TestAuditWarningsPanel(t *testing.T) {
	// Постпроверка письма: контейнер + функция отрисовки предупреждений.
	for _, want := range []string{`id="audit-warns"`, "showAuditWarnings", "data.warnings"} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет части постпроверки: не найдено %q", want)
		}
	}
}

func TestFitVerdictPanel(t *testing.T) {
	// Рекомендация отклика: контейнер + отрисовка вердикта из done.fit.
	for _, want := range []string{
		`id="fit-verdict"`, "showFitVerdict", "data.fit",
		"apply_with_caveats", // все три состояния вердикта различимы
		"verdict-skip",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет части рекомендации отклика: не найдено %q", want)
		}
	}
}
