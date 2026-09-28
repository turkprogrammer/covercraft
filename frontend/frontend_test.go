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

func TestProfileWarningSurfaced(t *testing.T) {
	for _, want := range []string{
		`id="profile-warning"`,
		"function setProfileWarning",
		"setProfileWarning(data.profileWarning)",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — профиль пуст должен быть виден пользователю", want)
		}
	}
}

func TestFitFixGuardsAndReportsFailures(t *testing.T) {
	for _, want := range []string{
		"function busy()",
		"if (!btn || btn.disabled || busy()) return;",
		"fit-разбор вакансии не удался",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — защита цикла fit-fix обязана быть", want)
		}
	}
}

func TestStopButtonAndAbortLifetime(t *testing.T) {
	for _, want := range []string{
		`id="stop"`,
		"genAbort.abort()",
		"if (genAbort === ctrl) genAbort = null;",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — кнопка stop должна быть, а отмена — работать до конца стрима", want)
		}
	}
}

func TestFitEngineSelect(t *testing.T) {
	for _, want := range []string{
		`id="fitEngine"`,
		"fitEngine: els.fitEngine.value,",
		`els.fitEngine.value = s.fitEngine || "";`,
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — движок фита должен переключаться из приложения", want)
		}
	}
}

func TestFitPanelShowsEvidenceSource(t *testing.T) {
	for _, want := range []string{
		"письмо, но не в письме",
		"мост",
		"нет данных",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет подписи источника %q — пользователь должен видеть, откуда доказательство", want)
		}
	}
}

func TestLiveRegions(t *testing.T) {
	for _, want := range []string{
		`aria-live="polite"`,
		`<footer id="status" role="status">`,
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — стриминг и статус должны объявляться скринридеру", want)
		}
	}
}
