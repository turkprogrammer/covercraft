package cover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/turkprogrammer/covercraft/internal/prompt"
)

func writeContext(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBuildUserPromptIncludesContextAndVacancy(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-profile.md":  "# Профиль\nGo-разработчик, 5 лет опыта.",
		"02-projects.md": "# Проекты\nvpnctl — менеджер VPN на Go.",
		"notes.txt":      "не .md — игнорируется",
		"README.md":      "# Резюме-заметки\nУмею в GTK.",
	})

	got := BuildUserPrompt(dir, "Вакансия: senior Go developer в банке.", nil, nil)

	for _, want := range []string{
		"Go-разработчик, 5 лет опыта.",
		"vpnctl — менеджер VPN на Go.",
		"Умею в GTK.",
		"Вакансия: senior Go developer в банке.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("промпт не содержит %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "не .md") {
		t.Error("файлы не .md не должны попадать в промпт")
	}
	// Контекст идёт раньше вакансии: модель сперва видит профиль.
	if strings.Index(got, "Go-разработчик") > strings.Index(got, "Вакансия") {
		t.Error("контекст должен идти раньше описания вакансии")
	}
}

func TestBuildUserPromptWithoutContextDir(t *testing.T) {
	got := BuildUserPrompt(filepath.Join(t.TempDir(), "нет-такой-папки"), "Вакансия X.", nil, nil)
	if !strings.Contains(got, "Вакансия X.") {
		t.Errorf("без папки контекста промпт должен содержать вакансию: %q", got)
	}
}

func TestBuildUserPromptIsStable(t *testing.T) {
	// Файлы сортируются по имени — промпт детерминирован.
	dir := writeContext(t, map[string]string{
		"b.md": "B-контент",
		"a.md": "A-контент",
	})
	got1 := BuildUserPrompt(dir, "V", nil, nil)
	got2 := BuildUserPrompt(dir, "V", nil, nil)
	if got1 != got2 {
		t.Error("повторный вызов должен давать тот же промпт")
	}
	if strings.Index(got1, "A-контент") > strings.Index(got1, "B-контент") {
		t.Error("файлы должны идти в порядке имён (a.md раньше b.md)")
	}
}

// TestBuildUserPromptChecklist — непустой список must-have идёт отдельной
// секцией-чек-листом перед вакансией; пустой — секции нет вовсе.
func TestBuildUserPromptChecklist(t *testing.T) {
	dir := t.TempDir()

	withChecklist := BuildUserPrompt(dir, "V", []string{"Go 3+ лет", "PostgreSQL", "  "}, nil)
	if !strings.Contains(withChecklist, "### Обязательный чек-лист") {
		t.Errorf("с непустым musts секции чек-листа нет:\n%s", withChecklist)
	}
	for _, want := range []string{"- Go 3+ лет\n", "- PostgreSQL\n"} {
		if !strings.Contains(withChecklist, want) {
			t.Errorf("чек-лист не содержит %q:\n%s", want, withChecklist)
		}
	}
	if strings.Contains(withChecklist, "- \n") || strings.Contains(withChecklist, "- \n\n") {
		t.Errorf("пустые элементы не должны попадать в чек-лист:\n%s", withChecklist)
	}
	// Чек-лист между контекстом и вакансией, с запретом на выдумки.
	iHead := strings.Index(withChecklist, "### Обязательный чек-лист")
	iVac := strings.Index(withChecklist, "### Вакансия")
	if !(iHead >= 0 && iVac > iHead) {
		t.Errorf("чек-лист должен идти перед вакансией:\n%s", withChecklist)
	}
	// Правила письма — в системном промпте (settings.DefaultSystemPrompt).
	// В user-промпте остаётся только чек-лист фактов: дублирующие инструкции
	// конфликтовали с v4 (тот запрещает начинать пробел с «не применял»).
	if !strings.Contains(withChecklist, "Закрой в письме каждое обязательное требование") {
		t.Errorf("чек-лист должен требовать закрытия каждого требования:\n%s", withChecklist)
	}
	if strings.Contains(withChecklist, "готов освоить") {
		t.Error("формула пробела «X не применял — есть Y, готов освоить» противоречит v4 и должна жить только в промпте")
	}
	if strings.Contains(withChecklist, "БЕЗ названия технологий из фактов") {
		t.Error("запрет называть технологии из фактов противоречит v4 (мост строится ИМЕННО на названном факте)")
	}

	without := BuildUserPrompt(dir, "V", nil, nil)
	if strings.Contains(without, "чек-лист") {
		t.Errorf("пустой musts — секции быть не должно:\n%s", without)
	}
}

func TestDropSectionsRemovesExactBlock(t *testing.T) {
	raw := "### преамбула\n\n## Go-проекты\n\nпо факту\n\n### вложенный\n\n## PHP\n\nphp-факт\n"
	got := DropSections(raw, []prompt.Drop{{Heading: "## Go-проекты"}})
	for _, want := range []string{"### преамбула", "## PHP", "php-факт"} {
		if !strings.Contains(got, want) {
			t.Errorf("после вырезания нет %q в:\n%s", want, got)
		}
	}
	if strings.Contains(got, "по факту") || strings.Contains(got, "### вложенный") {
		t.Error("вложенный ### обязан уйти вместе с родительским ##")
	}
}

func TestDropSectionsKeepsNeighbourHeading(t *testing.T) {
	raw := "## A\n\na\n\n## B\n\nb\n"
	got := DropSections(raw, []prompt.Drop{{Heading: "## A"}})
	if !strings.Contains(got, "## B") || !strings.Contains(got, "\nb\n") {
		t.Errorf("соседний ##B не должен съедаться:\n%s", got)
	}
}

func TestDropSectionsNotFoundIsByteIdentical(t *testing.T) {
	raw := "## ФАКТЫ-ОГРАНИЧИТЕЛИ (не выдумывать сверх)\n\n- не выдумывай\n"
	if got := DropSections(raw, []prompt.Drop{{Heading: "## Нет такого"}}); got != raw {
		t.Errorf("ненайденный заголовок → побайтово вход:\n%q\n%q", raw, got)
	}
	if got := DropSections(raw, nil); got != raw {
		t.Error("пустые drops → побайтово вход")
	}
}

func TestDropSectionsMultipleBlocksOneFile(t *testing.T) {
	raw := "## A\n\na\n\n## B\n\nb\n\n## C\n\nc\n"
	got := DropSections(raw, []prompt.Drop{{Heading: "## A"}, {Heading: "## C"}})
	if strings.Contains(got, "a\n") || strings.Contains(got, "c\n") {
		t.Errorf("оба блока должны уйти:\n%s", got)
	}
	if !strings.Contains(got, "## B") || !strings.Contains(got, "b\n") {
		t.Errorf("##B обязан остаться:\n%s", got)
	}
}

func TestProfileSectionsListsHeadingsQualified(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"02-b.md":   "## B1\nтекст\n### B1.1\n\n## B2\n",
		"01-a.md":   "## A1\nтекст\n",
		"notes.txt": "## Не считается\n",
	})
	got := ProfileSections(dir)
	want := []prompt.Section{
		{File: "01-a.md", Heading: "## A1", Excerpt: "текст"},
		{File: "02-b.md", Heading: "## B1", Excerpt: "текст"},
		{File: "02-b.md", Heading: "## B2"},
	}
	if len(got) != len(want) {
		t.Fatalf("секций = %d, хочу %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("секция[%d] = %+v, хочу %+v", i, got[i], want[i])
		}
	}
	// Уровень ## отбирается, ### внутри секции — нет (это не ##-заголовок).
	for _, s := range got {
		if strings.Contains(s.Heading, "B1.1") || strings.Contains(s.Excerpt, "B1.1") {
			t.Errorf("### не должен попадать ни в заголовок, ни в excerpt: %+v", s)
		}
	}
}

// TestProfileSectionsExcerptSlicing — выжимка фактов секции: первые 3 непустые
// строки текста (строки-заголовки пропускаются), конкатенация через "; ",
// ≤240 байт по границе руны с «…»; защищённые и пустые секции — без excerpt.
func TestProfileSectionsExcerptSlicing(t *testing.T) {
	// "A" сдвигает байтовую границу: срез 240 попадает в середину кириллической
	// руны — проверяем, что откат до границы руны сработал (utf8.ValidString).
	long := "A" + strings.Repeat("ы", 200) // 401 байт
	dir := writeContext(t, map[string]string{
		"01.md": "## Много\nстрока1\n### вложенный\nстрока2\n\nстрока3\nстрока4\n",
		"02.md": "## Длинный\n" + long + "\n",
		"03.md": "## КОНТАКТЫ\ntelegram: @secret\n",
		"04.md": "## Пустой\n",
	})

	byHeading := map[string]prompt.Section{}
	for _, s := range ProfileSections(dir) {
		byHeading[s.Heading] = s
	}

	if got, want := byHeading["## Много"].Excerpt, "строка1; строка2; строка3"; got != want {
		t.Errorf("excerpt = %q, хочу %q", got, want)
	}

	longExcerpt := byHeading["## Длинный"].Excerpt
	if !strings.HasSuffix(longExcerpt, "…") {
		t.Errorf("длинный excerpt обязан обрезаться с «…»: %q", longExcerpt)
	}
	if !utf8.ValidString(longExcerpt) {
		t.Errorf("обрезка разрезала руну: %q", longExcerpt)
	}
	if len(longExcerpt) > sectionExcerptMaxBytes+len("…") {
		t.Errorf("excerpt %d байт превышает лимит %d", len(longExcerpt), sectionExcerptMaxBytes)
	}

	if got := byHeading["## КОНТАКТЫ"].Excerpt; got != "" {
		t.Errorf("защищённый раздел не должен получать excerpt: %q", got)
	}
	if got := byHeading["## Пустой"].Excerpt; got != "" {
		t.Errorf("пустая секция не должна получать excerpt: %q", got)
	}
}

// TestBuildUserPromptDropsScopedToFile — дропы последнего ##-блока файла не
// задевают синтетический разделитель «### имя» и содержимое соседнего файла.
func TestBuildUserPromptDropsScopedToFile(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-a.md": "# A\n\n## Хвостовый\n\nхвост-а\n",
		"02-b.md": "# B\n\n## Нужно\n\nб-контент\n",
	})
	got := BuildUserPrompt(dir, "V", nil, []prompt.Drop{{File: "01-a.md", Heading: "## Хвостовый"}})
	if strings.Contains(got, "хвост-а") {
		t.Errorf("хвост 01-a.md должен вырезаться:\n%s", got)
	}
	for _, want := range []string{"### 01-a", "### 02-b", "## Нужно", "б-контент", "### Вакансия"} {
		if !strings.Contains(got, want) {
			t.Errorf("после дропа нет %q:\n%s", want, got)
		}
	}
	// Дроп на файл, которого нет в списке, ничего не делает.
	noOp := BuildUserPrompt(dir, "V", nil, []prompt.Drop{{File: "03-нет.md", Heading: "## Нужно"}})
	if !strings.Contains(noOp, "б-контент") {
		t.Errorf("дроп чужого файла не должен трогать чужой контент:\n%s", noOp)
	}
}

// TestBuildFitFixPromptSelectsRelevantSections — живой баг (октябрь 2026):
// fit-fix отправлял модели ВЕСЬ профиль (85.7 КБ context/*.md) вместе с
// письмом на каждую итерацию. Модель тонет в объёме и отвечает эхом промпта,
// после чего правки откатываются — «автофикс не может исправить даже через
// несколько попыток». В fit-fix должны уходить только секции профиля,
// релевантные caveat-строкам, плюс вакансия и чек-лист.
func TestBuildFitFixPromptSelectsRelevantSections(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-profile.md": "# Профиль\n\n## Go и highload\n" +
			"Stable ID: Kafka, 10 000 RPS, at-least-once, идемпотентность через ClickHouse Upsert.\n\n" +
			"## Маркетинг и SEO\n" +
			"Собрал воронку, настроил таргетированную рекламу, курил контент-план.\n",
		"02-projects.md": "# Проекты\n\n## vpnctl\nМенеджер VPN на Go.\n",
	})
	caveats := []string{"Highload-требование: Kafka at-least-once идемпотентность — впиши в письмо"}

	got := BuildFitFixUserPrompt(dir, "Go backend engineer", nil, caveats, nil)

	if !strings.Contains(got, "Stable ID: Kafka, 10 000 RPS") {
		t.Errorf("релевантная секция профиля не попала в fit-fix промпт:\n%s", got)
	}
	if strings.Contains(got, "воронку") || strings.Contains(got, "таргетированную") {
		t.Errorf("нерелевантная секция (маркетинг) не должна попадать в fit-fix:\n%s", got)
	}
	if !strings.Contains(got, "Go backend engineer") {
		t.Errorf("вакансия должна остаться в промпте:\n%s", got)
	}
}

// TestBuildFitFixPromptFallsBackToFullProfile — отбор не должен обнулять
// промпт: если ни одна секция не совпала с caveat (формулировки бывают
// далеки от текста профиля), модель получает полный профиль, как раньше, —
// иначе автоправка не найдёт факт и начнёт выдумывать.
func TestBuildFitFixPromptFallsBackToFullProfile(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-profile.md": "# Профиль\n## Разное\nУмею чинить принтеры и варить кофе.\n",
	})
	caveats := []string{"Oпыт работы с квантовыми вычислениями — впиши в письмо"}

	got := BuildFitFixUserPrompt(dir, "Go engineer", nil, caveats, nil)

	if !strings.Contains(got, "чинить принтеры") {
		t.Errorf("при нулевом отборе нужен фоллбэк на полный профиль:\n%s", got)
	}
}

// TestBuildFitFixPromptBounded — промпт fit-fix обязан быть существенно меньше
// полного профиля: потолок зафиксирован константой, а не «как получится».
func TestBuildFitFixPromptBounded(t *testing.T) {
	big := "# Профиль\n"
	for i := 0; i < 60; i++ {
		big += fmt.Sprintf("\n## Раздел %d\nУникальная реализация №%d: Raft-консенсус, snapshot, compaction.\n", i, i)
	}
	dir := writeContext(t, map[string]string{"01-profile.md": big})
	caveats := []string{"Raft-консенсус snapshot compaction — впиши в письмо"}

	got := BuildFitFixUserPrompt(dir, "Go engineer", nil, caveats, nil)

	if len(got) > fitFixProfileMaxBytes {
		t.Errorf("промпт fit-fix %d байт превышает лимит %d", len(got), fitFixProfileMaxBytes)
	}
	// Релевантный раздел обязан быть в отборе при лимите.
	if !strings.Contains(got, "Raft-консенсус") {
		t.Errorf("релевантный раздел потерялся при усечении:\n%.500s", got)
	}
}

// TestAppliedDrops — AppliedDrops возвращает только реально вырезанные дропы.
// UI обязан отличать «compose попросил вырезать N» от «вырезано M»: дроп по
// заголовку, которого в профиле нет, молча ничего не вырезает, и без этой
// проверки UI рапортовал бы «вырезано N» при полном профиле в модели.
func TestAppliedDrops(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-a.md": "# Профиль\n\n## Хвостовый\n\nпрочь\n\n## Нужный\n\nstay\n",
		"02-b.md": "# Проект\n\n## Второй\n\nalso\n",
	})

	tests := []struct {
		name  string
		drops []prompt.Drop
		want  []string // "file / heading" реально применённых
	}{
		{"точный дроп", []prompt.Drop{{File: "01-a.md", Heading: "## Хвостовый"}}, []string{"01-a.md / ## Хвостовый"}},
		// Пробелы нормализуются, регистр заголовка — нет (как в DropSections);
		// регистронезависим только ИМЯ ФАЙЛА (EqualFold в dropsFor).
		{"нормализация пробелов", []prompt.Drop{{File: "01-a.md", Heading: "##   Хвостовый  "}}, []string{"01-a.md / ##   Хвостовый  "}},
		{"регистр заголовка не списывается", []prompt.Drop{{File: "01-a.md", Heading: "## хвостовый"}}, nil},
		{"регистр файла", []prompt.Drop{{File: "01-A.MD", Heading: "## Нужный"}}, []string{"01-A.MD / ## Нужный"}},
		{"два дропа", []prompt.Drop{
			{File: "01-a.md", Heading: "## Хвостовый"},
			{File: "02-b.md", Heading: "## Второй"},
		}, []string{"01-a.md / ## Хвостовый", "02-b.md / ## Второй"}},
		{"нет такого файла", []prompt.Drop{{File: "99-нет.md", Heading: "## Хвостовый"}}, nil},
		{"нет такого заголовка", []prompt.Drop{{File: "01-a.md", Heading: "## Исчезнувший"}}, nil},
		{"пустой заголовок", []prompt.Drop{{File: "01-a.md", Heading: "   "}}, nil},
		{"пустой вход", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppliedDrops(dir, tt.drops)
			var names []string
			for _, d := range got {
				names = append(names, d.File+" / "+d.Heading)
			}
			if strings.Join(names, "|") != strings.Join(tt.want, "|") {
				t.Errorf("AppliedDrops = %v, хочу %v", names, tt.want)
			}
		})
	}
}

// TestAppliedDropsAgreesWithDropSections — инвариант: AppliedDrops не может
// сосчитать дроп применённым, если вырезание им фактически не изменило текст.
// Это связывает функцию с реальным поведением DropSections, а не только с
// наличием заголовка в файле.
func TestAppliedDropsAgreesWithDropSections(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-a.md": "# Профиль\n\n## Хвостовый\n\nпрочь\n\n## Нужный\n\nstay\n",
	})
	present := prompt.Drop{File: "01-a.md", Heading: "## Хвостовый"}
	absent := prompt.Drop{File: "01-a.md", Heading: "## Исчезнувший"}

	if len(AppliedDrops(dir, []prompt.Drop{present})) != 1 {
		t.Error("существующий заголовок обязан быть применённым")
	}
	if len(AppliedDrops(dir, []prompt.Drop{absent})) != 0 {
		t.Error("несуществующий заголовок обязан быть отброшен")
	}
	if BuildUserPrompt(dir, "V", nil, []prompt.Drop{absent}) != BuildUserPrompt(dir, "V", nil, nil) {
		t.Error("дроп по отсутствующему заголовку обязан оставить промпт прежним")
	}
	if len(AppliedDrops(writeContext(t, map[string]string{}), []prompt.Drop{present})) != 0 {
		t.Error("в пустом каталоге применённых дропов быть не может")
	}
}

// TestAuditFixPromptSelectsRelevantSections — автоправка по замечаниям аудита
// отправляет модели только релевантные секции профиля, а не весь context/.
// Живой баг (октябрь 2026): audit-fix шёл через BuildUserPrompt и гнал модели
// все 85 КБ на каждую итерацию — модель тонула и отвечала эхом, поэтому
// «повтори автоправку» не помогало (воспроизведено на двух провайдерах).
func TestAuditFixPromptSelectsRelevantSections(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-profile.md": "# Профиль\n\n## Очереди\n" +
			"Spring Cloud Stream на RabbitMQ, подтверждение и ретраи, DLQ.\n\n" +
			"## Маркетинг\nСобрал воронку, настроил таргетированную рекламу.\n",
	})
	warns := []string{"«RabbitMQ» одновременно в буллетах и в пробелах — оставь только одно"}

	got := BuildAuditFixUserPrompt(dir, "PHP/Symfony", nil, warns, nil)

	if !strings.Contains(got, "RabbitMQ") {
		t.Errorf("релевантная секция профиля не попала в промпт автоправки:\n%s", got)
	}
	if strings.Contains(got, "воронку") {
		t.Errorf("нерелевантная секция (маркетинг) не должна попадать в автоправку:\n%s", got)
	}
	if !strings.Contains(got, "PHP/Symfony") {
		t.Errorf("вакансия должна остаться в промпте:\n%s", got)
	}
}

// TestAuditFixPromptShrinksVsFullProfile — главный эффект правки: объём
// промпта автоправки должен быть кратно меньше полного профиля. На живом
// профиле это 86 607 → 6 406 байт (92.6%).
func TestAuditFixPromptShrinksVsFullProfile(t *testing.T) {
	// Одна релевантная секция среди многих: именно этот случай и есть на живом
	// профиле (86 607 → 6 406 байт). Если ключевое слово встречается в каждой
	// секции, отбирается всё и сокращения не будет — такому профилю тест не
	// подходит.
	big := "# Профиль\n\n## Очереди\nRabbitMQ, подтверждение, ретраи, DLQ.\n"
	for i := 0; i < 60; i++ {
		big += fmt.Sprintf("\n## Раздел %d\nДетали реализации №%d: воронка, таргет, контент-план.\n", i, i)
	}
	dir := writeContext(t, map[string]string{"01-profile.md": big})
	warns := []string{"«RabbitMQ» одновременно в буллетах и в пробелах"}

	full := BuildUserPrompt(dir, "Go engineer", nil, nil)
	got := BuildAuditFixUserPrompt(dir, "Go engineer", nil, warns, nil)

	if len(got) >= len(full)/2 {
		t.Errorf("промпт автоправки %d байт не сокращён относительно полного профиля %d", len(got), len(full))
	}
	if len(got) > fitFixProfileMaxBytes {
		t.Errorf("промпт автоправки %d байт превышает лимит %d", len(got), fitFixProfileMaxBytes)
	}
	if !strings.Contains(got, "RabbitMQ") {
		t.Errorf("релевантная секция потерялась:\n%.400s", got)
	}
	if strings.Contains(got, "воронка") {
		t.Errorf("нерелевантные секции попали в промпт автоправки:\n%.400s", got)
	}
}

// TestAuditFixPromptFallsBackOnStructuralWarning — структурные замечания
// («нет обязательной секции…») имени факта не содержат, ключей не дают. Отбор
// пуст — обязан сработать фоллбэк на полный профиль, иначе модель получит
// промпт без профиля и начнёт выдумывать факты.
func TestAuditFixPromptFallsBackOnStructuralWarning(t *testing.T) {
	dir := writeContext(t, map[string]string{
		"01-profile.md": "# Профиль\n## Разное\nУмею чинить принтеры и варить кофе.\n",
	})
	warns := []string{"нет обязательной секции «Честно о пробелах» — v4 §2.4"}

	got := BuildAuditFixUserPrompt(dir, "Go engineer", nil, warns, nil)

	if !strings.Contains(got, "чинить принтеры") {
		t.Errorf("при нулевом отборе нужен фоллбэк на полный профиль:\n%s", got)
	}
}

// TestAuditWarningKeywords — ключи отбора берутся только из содержимого
// «ёлочек»: служебные слова замечания встречаются в каждом и отбирали бы
// случайные секции.
func TestAuditWarningKeywords(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"факт без кавычек не даёт ключа", []string{"Потерян факт: Symfony 6.4"}, nil},
		{"имя в кавычках", []string{"«Redis» одновременно в буллетах"}, []string{"redis"}},
		// «под» (3), «ваш» (3) и «стек» (4) короче порога в символах.
		{"несколько слов в кавычках", []string{"«Адаптация под ваш стек»"}, []string{"адаптация"}},
		{"короткое имя отсекается", []string{"«PHP» в строке стека"}, nil},
		{"дедуп между замечаниями", []string{"«Symfony» потерян", "«Symfony» в пробелах"}, []string{"symfony"}},
		{"незакрытая кавычка", []string{"«Redis в пробелах"}, []string{"redis", "пробелах"}},
		{"пусто", nil, nil},
	}
	for _, c := range cases {
		got := auditWarningKeywords(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}
