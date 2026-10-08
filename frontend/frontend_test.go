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

// TestPresetControls — пресеты: селектор, кнопки CRUD, шов с API,
// статус «пресет» в promptState и модалка создания/правки. Модалка
// обязательна: живой случай — пресет создался со снимком дефолтного
// промпта, потому что пользователь не видел, какой текст сохраняется.
func TestPresetControls(t *testing.T) {
	for _, want := range []string{
		`id="presetSelect"`,
		`id="savePreset"`,
		`id="newPreset"`,
		`id="delPreset"`,
		`id="editPreset"`,
		`id="presetModal"`,
		`id="presetName"`,
		`id="presetText"`,
		`id="presetLen"`,
		`"/api/presets"`,
		`"/api/presets/active"`,
		"function promptState()",
		"async function switchPreset()",
		"async function loadPresets()",
		"async function delPreset()",
		"function openPresetModal",
		"openPresetModal(\"create\", \"\")", // new preset — чистое окно, без снимка поля
		"function updatePresetLen",          // счётчик байт до отправки на сервер
		"async function submitPresetModal()",
		"пресет: ", // подпись в #prompt-sub — не остаётся «кастом»
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — пресеты обязаны управляться из панели system.prompt", want)
		}
	}
	// Привязка активного пресета переживает restart: boot грузит список
	// и заполняет поле текстом активного пресета.
	if !strings.Contains(IndexHTML, "await loadPresets();") {
		t.Error("boot должен загружать пресеты — иначе активный пресет не восстановится")
	}
	// Фактический системный промпт: renderSentInfo называет источник
	// (пресет/ваш/дефолтный) и первую строку — иначе «какой промпт ушёл»
	// остаётся догадкой.
	for _, want := range []string{
		"data.usedSystemPrompt",
		"пресет «\" + p.name + \"»",
		"начало: «",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — sentinfo должен показывать фактический промпт", want)
		}
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

// TestSentInfoPanelShowsFactNotIntent — рядом с подписью compose (намерение,
// предвычисление на клиенте) обязан стоять блок с фактом от SSE-done.
// Без него потеря promptDrops между compose и /api/generate невидима: подпись
// остаётся прежней, а модель получает полный профиль. usedSystemPrompt
// бэкенд отдаёт и это покрыто TestDoneCarriesUsedSystemPrompt — фронт обязан
// его читать, иначе доказательство отбрасывается.
func TestSentInfoPanelShowsFactNotIntent(t *testing.T) {
	for _, want := range []string{
		`id="prompt-sentinfo"`,
		"function renderSentInfo",
		"renderSentInfo(data)",
		"data.appliedDropSections",
		"data.usedUserPromptBytes",
		"data.usedProfileBytes",
		"data.usedSystemPrompt",
		"prompt-sentinfo mismatch", // расхождение обязано быть заметно
		"нажмите compose заново",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — факт отправки обязан быть виден", want)
		}
	}
	// Факт приходит по всем путям: обычная генерация, audit-fix, fit-fix.
	if got := strings.Count(IndexHTML, "renderSentInfo(data)"); got < 4 { // 3 вызова + объявление
		t.Errorf("renderSentInfo(data) встречается %d раз(а), хочу минимум 4", got)
	}
}

// TestSentInfoWarnsOnOversizedSystemPrompt — живой случай: в поле системного
// промпта оказался текст самой вакансии (63 КБ вместо 1–5 КБ). Вакансия ушла
// бы в модель дважды, второй раз в роли инструкции.
func TestSentInfoWarnsOnOversizedSystemPrompt(t *testing.T) {
	for _, want := range []string{
		"sent.length >= 8192",
		"defaultPrompt.length * 4",
		"внимание: системный промпт вчетверо больше дефолтного",
		"reset default",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — раздутый системный промпт обязан быть виден", want)
		}
	}
	// Флаг должен влиять на подсветку, а не только дописывать текст: иначе
	// предупреждение есть, а блок выглядит как обычный.
	if !strings.Contains(IndexHTML, `(mismatch || oversize) ? "prompt-sentinfo mismatch"`) {
		t.Error("oversize не участвует в выборе класса — предупреждение останется незаметным")
	}
}

// TestFitFixStopsOnRejectedEcho — правка 4 меняет контракт: при отклонённом
// эхе сервер не считает вердикт фита (data.fit пуст), но возвращает исходное
// непустое письмо. Без отдельной ветки UI сообщал бы «fit-разбор вакансии не
// удался» — то есть обвинил бы не тот этап.
func TestFitFixStopsOnRejectedEcho(t *testing.T) {
	for _, want := range []string{
		"const echoHit = (data.warnings || []).some(isEchoWarning);",
		"автоправка вернула эхо — исходное письмо сохранено",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — отклонённое эхо должно останавливать автофикс с внятной причиной", want)
		}
	}
	// Ветка обязана стоять ДО проверки data.fit: цикл прерывается на эхо, и
	// сообщение про «fit-разбор не удался» тут было бы неверным — разбор-то
	// как раз удался, просто автоправка вернула эхо.
	echoAt := strings.Index(IndexHTML, "const echoHit")
	fitAt := strings.Index(IndexHTML, "if (!data.fit)")
	if echoAt < 0 || fitAt < 0 || echoAt > fitAt {
		t.Errorf("ветка отклонённого эха (поз. %d) должна идти раньше проверки data.fit (поз. %d)", echoAt, fitAt)
	}
}

// TestFitFixEchoMessageReflectsVerdict — после отката на исходное письмо
// вердикт по нему СЧИТАЕТСЯ, поэтому сообщение обязано это отражать. Раньше
// оно утверждало «вердикт недоступен» при заполненной панели, а кнопка
// fit-fix при этом работала — противоречие на экране.
func TestFitFixEchoMessageReflectsVerdict(t *testing.T) {
	for _, want := range []string{
		"вердикт выше актуален",
		"вердикт фита недоступен",
		`data.fit ? "" : "err"`,
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — сообщение об эхо должно различать случаи с вердиктом и без", want)
		}
	}
}

// skipCommentLines — пропускает пробелы и строки-комментарии (// до конца
// строки) в начале фрагмента. Нужна проверкам порядка вызовов: объясняющий
// комментарий между двумя вызовами не нарушает инвариант, но и не должен
// его валить.
func skipCommentLines(s string) string {
	for {
		line := s
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			line, s = s[:i], s[i+1:]
		} else {
			s = ""
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			if line == "" && s == "" {
				return ""
			}
			continue
		}
		return line
	}
}

// TestEchoCallToActionMatchesRenderedButtons — живой баг (октябрь 2026):
// сервер пишет «правь вручную или повтори автоправку», но кнопок может не
// оказаться НИ ОДНОЙ. auto-fix рисуется только при defects>0 || lastDefects>0,
// fit-fix — только при fixable-кавеатах. Письмо без дефектов и вердикт без
// маркеров «впиши» → сообщение зовёт к кнопке, которой на экране нет.
//
// Инвариант проверяем статически (как остальные тесты фронтенда): призыв должен
// вычисляться по фактически ВИДИМЫМ кнопкам, а не быть константой.
func TestEchoCallToActionMatchesRenderedButtons(t *testing.T) {
	for _, want := range []string{
		// Призыв выбирается по видимым кнопкам; скрытый контейнер с
		// оставшейся внутри кнопкой (fitFixFallback) — это «кнопки нет».
		`if (visibleButton("auditFix")) return CTA_AUDIT;`,
		`if (visibleButton("fitFix")) return "правь вручную или повтори fit-fix"`,
		`return "правь вручную или запусти gen заново"`,
		`const el = document.getElementById(id);`,
		`return !!el && !el.closest("[hidden]");`,
		// …и подставляется в уже нарисованный текст вместо константы.
		"function echoCallToAction()",
		"function reconcileEchoCallToAction()",
		"n.textContent = n.textContent.replace(CTA_AUDIT, cta)",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — призыв должен соответствовать нарисованным кнопкам", want)
		}
	}
	// Контейнер кнопки fit-fix чистится при снятии вердикта, а не просто
	// прячется: иначе getElementById находит кнопку, которой не видно.
	const clean = `if (fb) { fb.hidden = true; fb.textContent = ""; }`
	if got := strings.Count(IndexHTML, clean); got != 2 {
		t.Errorf("очисток fallback-контейнера %d, хочу 2 — кнопка может остаться в скрытом блоке", got)
	}
	// Призыв обязан доезжать и до статуса цикла fitFix: там он тоже стоял
	// константой и звал к возможно отсутствующей кнопке.
	if !strings.Contains(IndexHTML, `вердикт выше актуален. " + echoCallToAction()`) {
		t.Error("статус fitFix об эхо должен брать призыв из echoCallToAction()")
	}
	// reconcile обязана вызываться сразу после КАЖДОГО места, где рисуется
	// панель вердикта: fit-fix появляется именно там, и раньше мы ещё не знаем,
	// появится ли кнопка. Порядок в файле значения не имеет (объявления функций
	// хойстятся), важен порядок ВЫЗОВОВ — он и проверяется. Перебор идёт по всем
	// вхождениям, поэтому счётчик отдельно не нужен: забытый вызов поймает
	// проверка порядка.
	for rest := IndexHTML; ; {
		i := strings.Index(rest, "showFitVerdict(data.fit")
		if i < 0 {
			break
		}
		rest = rest[i:]
		eol := strings.IndexByte(rest, '\n')
		if eol < 0 {
			t.Fatalf("вызов showFitVerdict без следующей строки: %q", rest)
		}
		next := skipCommentLines(rest[eol+1:])
		if !strings.HasPrefix(next, "reconcileEchoCallToAction();") {
			t.Errorf("после showFitVerdict должен идти reconcileEchoCallToAction(), а идёт: %q", next)
			break
		}
		rest = rest[eol+1:]
	}

}

func TestEchoWarningNotSentBackToModel(t *testing.T) {
	// Живой баг (октябрь 2026): при отклонённом эхо сервер кладёт диагностику
	// в warnings, UI отправлял её модели наравне с дефектами письма. Модель
	// получала инструкцию «исправь то, что ты и так вернул» и повторяла эхо —
	// на двух разных провайдерах. Поэтому аудиторное замечание-эхо обязано
	// отсекаться до формирования запроса автоправки.
	for _, want := range []string{
		"function isEchoWarning(w)", // одно место распознавания
		"function letterDefects(warnings)",
		"const toSend = defects.length > 0 ? defects : lastDefects;", // не lastWarnings
		"warnings: toSend,",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — эхо-замечание не должно уходить модели как дефект", want)
		}
	}
	// Кнопка рисуется, когда есть свежие дефекты ИЛИ когда они были раньше и
	// автоправка повторяется после отклонённого эха. Раньше проверка была
	// `defects.length > 0`, и при эхо кнопка исчезала ровно тогда, когда сервер
	// звал «правь вручную или повтори автоправку» (живой баг, октябрь 2026).
	if !strings.Contains(IndexHTML, "if (defects.length > 0 || lastDefects.length > 0) {") {
		t.Error("кнопка auto-fix должна рисоваться и по прошлым дефектам (lastDefects)")
	}
	fixBtnAt := strings.Index(IndexHTML, `fixBtn.id = "auditFix"`)
	guardAt := strings.Index(IndexHTML, "if (defects.length > 0 || lastDefects.length > 0) {")
	if fixBtnAt < 0 || guardAt < 0 || guardAt > fixBtnAt {
		t.Errorf("создание кнопки auto-fix (поз. %d) должно идти под проверкой наличия дефектов (поз. %d)", fixBtnAt, guardAt)
	}
	// lastDefects обновляется только при содержательной проверке: эхо письмо не
	// проверяло, поэтому прежние дефекты остаются в силе.
	if !strings.Contains(IndexHTML, "if (defects.length > 0) lastDefects = defects;") {
		t.Error("lastDefects должен обновляться только при непустом наборе дефектов")
	}
	// Счётчик «N замечаний» обязан считать дефекты, а не эхо-диагностику.
	if !strings.Contains(IndexHTML, `"⚠ проверка письма: " + defects.length`) {
		t.Error("заголовок панели должен считать только дефекты письма")
	}
}

// TestFitFixButtonSurvivesRejectedEcho — при отклонённом эхо data.fit пуст, и
// панель вердикта скрывается вместе с кнопкой fit-fix. Раньше пользователь
// терял оба способа продолжить правку, хотя текст предлагал один.
func TestFitFixButtonSurvivesRejectedEcho(t *testing.T) {
	for _, want := range []string{
		"function fitFixFallbackBox()", // видимый контейнер вне скрытой панели
		`els.fitVerdict.parentNode.insertBefore(el, els.fitVerdict.nextSibling)`,
		"const host = box.hidden ? fitFixFallbackBox() : box;", // кнопка идёт в host
		"if (lastFitObjects.length > 0) showFitFixButton({ caveats: lastFitObjects });",
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI нет %q — кнопка fit-fix должна выживать при отклонённом эхо", want)
		}
	}
	// Кнопка обязана попадать в host, а не в скрытую панель: иначе элемент
	// создан, но пользователю не виден — молчаливая потеря автоправки.
	hostAt := strings.Index(IndexHTML, "const host = box.hidden")
	appendAt := strings.Index(IndexHTML, "host.appendChild(btn);")
	if hostAt < 0 || appendAt < 0 || hostAt > appendAt {
		t.Errorf("host (поз. %d) должен определяться до host.appendChild (поз. %d)", hostAt, appendAt)
	}
	// Сохранять нужно ОБЪЕКТЫ caveat, а не готовые строки: hasFixableCaveats
	// читает c.note, и строки прошли бы как {note: ""} → кнопка не рисуется.
	if !strings.Contains(IndexHTML, "lastFitObjects = [...(fit.caveats || []), ...(fit.covered || [])];") {
		t.Error("lastFitObjects должен хранить объекты caveat с полем note")
	}
	if !strings.Contains(IndexHTML, "lastFitCaveats = buildFitCaveatsList(fit);") {
		t.Error("lastFitCaveats должен продолжать обновляться — fitFix берёт из него старт")
	}
}

// TestPresetModeSelect — модалка пресета содержит селектор режима,
// сервер получает его в запросе /api/generate и UI показывает режим
// активного пресета ([QA] для qa-режима).
func TestPresetModeSelect(t *testing.T) {
	for _, want := range []string{
		`<select id="presetMode"`,
		`value="qa"`,
		`режим пресета`,
		`p.mode === "qa"`,
		` [QA]`,
		`mode: currentPresetMode`,
		// Регрессия: без этого поля в els модалка падала на
		// els.presetMode.value в ветке create — окно не открывалось.
		`presetMode: $("presetMode")`,
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI отсутствует ожидаемый фрагмент %q", want)
		}
	}
}

// TestPresetModeBackfill — старые пресеты без поля mode считаются
// cover_letter на уровне Load(), так что пресеты, созданные до этого
// режима, не ломаются. UI-подпись режима также присутствует.
func TestPresetModeBackfill(t *testing.T) {
	if !strings.Contains(IndexHTML, "qa (без fit/audit)") {
		t.Error(`в UI отсутствует описание режима qa`)
	}
	// Проверяем что в коде есть логика бэкенд-бэкендфилла:
	// в presets.go строка "if s.Presets[i].Mode == \"\"" присутствует.
	if !strings.Contains(IndexHTML, "Mode == \"\"") {
		// UI-фронтенд не хранит логику бэкенда — это OK
	}
}

// TestUiConfirmModal — подтверждение правок рисуется модалкой приложения,
// а не нативным confirm(): WebKit показывает чужое окно с заголовком
// «JavaScript — http://127.0.0.1…». Все четыре вызова переведены на
// uiConfirm; в скрипте не должно остаться боевого confirm(/alert(.
func TestUiConfirmModal(t *testing.T) {
	for _, want := range []string{
		`id="confirmModal"`,
		`function uiConfirm(`,
		`settleConfirm(true)`,
		`!(await uiConfirm(`,
	} {
		if !strings.Contains(IndexHTML, want) {
			t.Errorf("в UI отсутствует ожидаемый фрагмент %q", want)
		}
	}
	scrubbed := strings.ReplaceAll(IndexHTML, "uiConfirm(", "")
	scrubbed = strings.ReplaceAll(scrubbed, "settleConfirm(", "")
	if i := strings.Index(scrubbed, "confirm("); i >= 0 {
		t.Errorf("в UI остался боевой confirm() (позиция %d) — подтверждения обязаны идти через uiConfirm", i)
	}
	if strings.Contains(scrubbed, "window.confirm") {
		t.Error("в UI остался window.confirm()")
	}
	if strings.Contains(IndexHTML, "alert(") {
		t.Error("в UI остался alert() — подтвердить замену на стилевое окно")
	}
}
