// Package audit — правила аудита, загружаемые из профиля пользователя.
//
// Загружаются из файла .audit-rules.md в каталоге профиля (context/).
// При отсутствии файла или синтаксической ошибке — используются
// встроенные списки, определённые в audit.go: stackForbidden,
// infraDomainTerms, infraNeutralWords, claimableTerms, metricRules,
// obligations. Повторный вызов с другим каталогом сбрасывает
// к предыдущему загруженному файлу; пустой каталог = встроенные.
package audit

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// RulesFilename — имя файла правил профиля. Экспортировано: сервер
// проверяет наличие файла перед загрузкой, чтобы предупредить, что
// профильные проверки аудита выключены.
const RulesFilename = ".audit-rules.md"

// rulesMu защищает глобальные срезы правил: LoadProfileRules пишет под
// Lock, проверки аудита читают под RLock — гонки нет при параллельных
// /api/generate.
var rulesMu sync.RWMutex

// resetDefaults восстанавливает встроенные правила (дефолты из audit.go).
// Вызывается в начале LoadProfileRules и после любой ошибки парсера:
// падение файла не должно оставлять пакет с чужими или пустыми правилами.
func resetDefaults() {
	stackForbidden = defaultStackForbidden()
	metricRules = defaultMetricRules()
	obligations = defaultObligations()
	claimableTerms = defaultClaimableTerms()
	infraDomainTerms = defaultInfraDomainTerms()
	infraNeutralWords = defaultInfraNeutralWords()
}

// LoadProfileRules читает dir/.audit-rules.md; файла нет = встроенные правила.
// Каждый вызов перезагружает правила (без кэша по содержимому): правки файла
// подхватываются без перезапуска приложения. Запись под Lock, читатели
// (Check*/CheckInfraClaims) — под RLock (rulesMu).
func LoadProfileRules(dir string) error {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	resetDefaults()
	raw, err := os.ReadFile(filepath.Join(dir, RulesFilename))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // встроенные правила
		}
		return fmt.Errorf("read rules: %w", err)
	}
	if err := parseRules(raw); err != nil {
		resetDefaults() // падение парсера не оставляет пакет битым
		return err
	}
	return nil
}

var validSections = map[string]bool{
	"obligations":       true,
	"metricRules":       true,
	"stackForbidden":    true,
	"claimableTerms":    true,
	"infraDomainTerms":  true,
	"infraNeutralWords": true,
}

// parseRules разбирает тело .audit-rules.md построчно.
func parseRules(raw []byte) error {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	cur := ""
	var oblRaw, mruRaw, sfItems, ctItems, idItems, inwItems []string

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// «###…» — пометка, а не секция: пропускаем, чтобы одна случайная
		// ### не отбрасывала весь файл в fallback.
		if strings.HasPrefix(line, "###") {
			continue
		}
		// Заголовок секции: ## name (любая «##…» — неизвестная = ошибка).
		if strings.HasPrefix(line, "##") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			if !validSections[name] {
				return fmt.Errorf("unknown section ## %s", name)
			}
			cur = name
			continue
		}
		// Комментарий-строка (один # в начале) — игнорируем.
		if strings.HasPrefix(line, "#") {
			continue
		}
		if cur == "" {
			return fmt.Errorf("line outside any section: %s", line)
		}
		switch cur {
		case "obligations":
			oblRaw = append(oblRaw, line)
		case "metricRules":
			mruRaw = append(mruRaw, line)
		case "stackForbidden":
			sfItems = append(sfItems, line)
		case "claimableTerms":
			ctItems = append(ctItems, line)
		case "infraDomainTerms":
			idItems = append(idItems, line)
		case "infraNeutralWords":
			inwItems = append(inwItems, line)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scanner: %w", err)
	}

	// Парсим вложенную структуру ключ: значение (obligations/metricRules).
	if len(oblRaw) > 0 {
		obl, err := parseObligations(oblRaw)
		if err != nil {
			return err
		}
		obligations = obl
	}
	if len(mruRaw) > 0 {
		mr, err := parseMetricRules(mruRaw)
		if err != nil {
			return err
		}
		metricRules = mr
	}
	// Простые списки строк.
	if len(sfItems) > 0 {
		stackForbidden = sfItems
	}
	if len(ctItems) > 0 {
		nct := make([]claimableTerm, 0, len(ctItems))
		for _, it := range ctItems {
			// «термин» — голое слово (generic-предупреждение) либо
			// «термин: сообщение» — авторский текст предупреждения кандидата
			// (резьба по первому двоеточию — как в obligations).
			pat, msg := it, ""
			if i := strings.IndexByte(it, ':'); i > 0 {
				pat = strings.TrimSpace(it[:i])
				msg = strings.TrimSpace(it[i+1:])
			}
			if msg == "" {
				msg = fmt.Sprintf("«%s» заявлен как опыт — в профиле этого факта нет; не выдумывай защиту, которой не было", pat)
			}
			if pat == "" {
				pat = it
			}
			nct = append(nct, claimableTerm{pat: pat, msg: msg})
		}
		claimableTerms = nct
	}
	if len(idItems) > 0 {
		infraDomainTerms = idItems
	}
	if len(inwItems) > 0 {
		m := make(map[string]bool, len(inwItems))
		for _, w := range inwItems {
			m[strings.ToLower(w)] = true
		}
		infraNeutralWords = m
	}
	return nil
}

// splitKV — "key: value" (по первому двоеточию).
func splitKV(line string) (key, val string, ok bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:i])
	val = strings.TrimSpace(line[i+1:])
	if key == "" || val == "" {
		return "", "", false
	}
	return key, val, true
}

// parseObligations собирает блоки need/has/fact.
func parseObligations(raw []string) ([]obligation, error) {
	var out []obligation
	var cur struct {
		need, has, fact string
	}
	flush := func() error {
		if cur.need == "" || cur.has == "" || cur.fact == "" {
			return fmt.Errorf("obligation block is incomplete: need=%q has=%q fact=%q", cur.need, cur.has, cur.fact)
		}
		needRe, err := regexp.Compile(cur.need)
		if err != nil {
			return fmt.Errorf("obligation need regex: %w", err)
		}
		hasRe, err := regexp.Compile(cur.has)
		if err != nil {
			return fmt.Errorf("obligation has regex: %w", err)
		}
		out = append(out, obligation{need: needRe, has: hasRe, fact: cur.fact})
		cur = struct{ need, has, fact string }{}
		return nil
	}
	for _, line := range raw {
		key, val, ok := splitKV(line)
		if !ok {
			// не-ключевая строка внутри блока — битый формат
			return nil, fmt.Errorf("malformed obligations line: %q", line)
		}
		switch key {
		case "need":
			if cur.need != "" || cur.has != "" || cur.fact != "" {
				// начинается следующий блок — сбросить текущий
				if err := flush(); err != nil {
					return nil, err
				}
			}
			cur.need = val
		case "has":
			cur.has = val
		case "fact":
			cur.fact = val
		default:
			return nil, fmt.Errorf("obligations key %q — only need/has/fact", key)
		}
	}
	if cur.need != "" || cur.has != "" || cur.fact != "" {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// parseMetricRules собирает блоки metric/owners/fact.
func parseMetricRules(raw []string) ([]metricRule, error) {
	var out []metricRule
	var cur struct {
		metric, owners, fact string
	}
	flush := func() error {
		if cur.metric == "" || cur.owners == "" || cur.fact == "" {
			return fmt.Errorf("metricRules block is incomplete: metric=%q owners=%q fact=%q", cur.metric, cur.owners, cur.fact)
		}
		metricRe, err := regexp.Compile(cur.metric)
		if err != nil {
			return fmt.Errorf("metricRules metric regex: %w", err)
		}
		ownerRes := []*regexp.Regexp{}
		// owners — одно или несколько regexp-владельцев через «|» (одна строка).
		re, err := regexp.Compile(cur.owners)
		if err != nil {
			return fmt.Errorf("metricRules owner regex: %w", err)
		}
		ownerRes = append(ownerRes, re)
		out = append(out, metricRule{metric: metricRe, owners: ownerRes, fact: cur.fact})
		cur = struct{ metric, owners, fact string }{}
		return nil
	}
	for _, line := range raw {
		key, val, ok := splitKV(line)
		if !ok {
			return nil, fmt.Errorf("malformed metricRules line: %q", line)
		}
		switch key {
		case "metric":
			if cur.metric != "" || cur.owners != "" || cur.fact != "" {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			cur.metric = val
		case "owners":
			cur.owners = val
		case "fact":
			cur.fact = val
		default:
			return nil, fmt.Errorf("metricRules key %q — only metric/owners/fact", key)
		}
	}
	if cur.metric != "" || cur.owners != "" || cur.fact != "" {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
