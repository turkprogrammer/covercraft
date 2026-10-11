package audit

import (
	"strings"
	"testing"
)

// Регресс живого кейса (октябрь 2026, paste_8): письмо молчало о MariaDB
// из must-have «Ключевые навыки», и требование уходило в hard missing
// (в профиле MariaDB нет). Эталон трактует тот же пробел честно: буллет
// «Адаптация под ваш стек» с переносимостью PostgreSQL/MySQL — и fit
// классифицирует его как honest gap (caveat), а не «опыта нет». Молчание
// хуже честного признания — перверсия стимулов, которую аудит обязан
// ловить детерминированно.
func TestMariaDBGapMustBeNamedInLetter(t *testing.T) {
	vacancy := "Ключевые навыки: PostgreSQL, MySQL, MariaDB, Kafka."
	silent := "Чем могу быть полезен:\n- **Базы данных:** PostgreSQL (глубокое знание), MySQL 8.0.\n\nАдаптация под ваш стек:\n- Kubernetes не эксплуатировал — готов освоить.\n\nСтек: Go, PostgreSQL, MySQL, Kafka"
	gap := "Чем могу быть полезен:\n- **Базы данных:** PostgreSQL (глубокое знание), MySQL 8.0.\n\nАдаптация под ваш стек:\n- MariaDB: PostgreSQL/MySQL покрывают реляционные сценарии; MariaDB — естественное расширение, готов освоить оперативно.\n\nСтек: Go, PostgreSQL, MySQL, Kafka"

	w := strings.Join(Check(silent, vacancy).Warnings, "\n")
	if !strings.Contains(w, "MariaDB") {
		t.Errorf("молчание о MariaDB из must-have вакансии не поймано:\n%s", w)
	}
	w2 := strings.Join(Check(gap, vacancy).Warnings, "\n")
	if strings.Contains(w2, "Потерян факт: MariaDB") {
		t.Errorf("письмо с честным пробелом MariaDB не должно получать замечание:\n%s", w2)
	}
	// Вакансия без MariaDB — обязательство молчит.
	w3 := strings.Join(Check(silent, "Ключевые навыки: PostgreSQL, MySQL, Kafka.").Warnings, "\n")
	if strings.Contains(w3, "MariaDB") {
		t.Errorf("вакансия без MariaDB не должна порождать замечание:\n%s", w3)
	}
}
