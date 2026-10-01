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

// TestComposePromptButton — кнопка compose: отбор разделов профиля и его откат.
// Композер НЕ переписывает системный промпт, поэтому в UI не должно остаться
// ни статуса «авто», ни предупреждений о потерянных инвариантах playbook'а.
func TestComposePromptButton(t *testing.T) {
	for _, want := range []string{
		`id="composePrompt"`,
		`id="undoPrompt"`,
		`id="prompt-dropinfo"`,
		`class="btn-row"`,
		`"/api/prompt/compose"`,
		"vacancy: els.vacancy.value",
		"dropSections: promptDrops",
		"function promptState()",
		"function undoPrompt()",
		"function dropStaleDrops()",
		"dropStaleDrops();",
		"promptVacancy",
		"отбор разделов сброшен",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — кнопка compose обязана быть", want)
		}
	}
	for _, gone := range []string{"missingInvariants", "missingInvariantsCut", "promptAuto", "заменить его результатом compose"} {
		if strings.Contains(IndexHTML, gone) {
			t.Errorf("в UI осталось %q от переписывания промпта — снято вместе с контрактом", gone)
		}
	}
	// Три режима генерации передают drops.
	if got := strings.Count(IndexHTML, "dropSections: promptDrops"); got < 3 {
		t.Errorf("dropSections: promptDrops встречается %d раз(а), хочу минимум 3 (generate/auditFix/fitFix)", got)
	}
}

// TestFitFixStopsWhenNoProgress — живой баг: повторный фит на вакансии IAM
// крутил 3 итерации по 60+90 сек (~7,5 мин молчания), даже если fitFixable
// не уменьшался. Цикл обязан останавливаться, как только улучшения нет.
func TestFitFixStopsWhenNoProgress(t *testing.T) {
	for _, want := range []string{
		"fitMaxIter",    // сервер получает бюджет итераций
		"prevFixable",   // предыдущее число fixable для сравнения
		"без прогресса", // понятный статус вместо молчания
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в index.html нет %q — цикл fit-fix не ограничен", want)
		}
	}
}

// TestFitFixBudgetDefaultsToOne — по умолчанию одна итерация: повторный
// клик пользователь делает осознанно, а не ждёт 3×150 сек.
func TestFitFixBudgetDefaultsToOne(t *testing.T) {
	if !strings.Contains(IndexHTML, "fitMaxIter: 1") {
		t.Error("fitFix должен отправлять fitMaxIter: 1 по умолчанию")
	}
}

func TestLetterSyncFromServerResponse(t *testing.T) {
	// Живой баг (октябрь 2026): сервер отклонил эхо промпта и вернул
	// исходное письмо в data.letter, а UI продолжал показать дельты потока
	// (мусор) и блокировал копирование. Каждая точка, где результат
	// зависит от data.letter, обязана сверять поле с ответом сервера.
	if strings.Count(IndexHTML, "data.letter !== els.result.textContent") < 3 {
		t.Error("generate/auditFix/fitFix должны приводить поле к data.letter — найдено меньше 3 мест")
	}
}

func TestGenerateReportsEmptyLetterAsError(t *testing.T) {
	// Пустой ответ после отклонённого эха — не «ок»: пользователь должен
	// увидеть причину, а не считать, что письмо готово.
	if !strings.Contains(IndexHTML, "err: письмо не сохранено") {
		t.Error("пустой letter после генерации должен давать err-статус")
	}
}
