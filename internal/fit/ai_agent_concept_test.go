package fit

import (
	"testing"
)

// TestAIAgentConceptClosesAnonBullet — анонимизированный ИИ-буллет в письме
// закрывает требование про AI-агентов. Живой кейс (октябрь 2026): вакансия
// Backend Engineer (Go, AI Agents) требовала «выстраивать рабочий процесс с
// AI-агентами», в письме был ИИ-буллет («ИИ-инструменты — ежедневная
// практика (постановка задач, генерация, ревью, отладка)… архитектурный
// контроль сохраняется за инженером»), но концепт «агентские системы и Tool
// Use» требовал сигналы «агент|agent|tool use|function call» — в анон-письме
// их нет (честно обезличено), и требование уходило в caveat. Фикс: сигналы
// расширены анонимизированными маркерами агентного процесса — «постановка
// задач», «ежедневная практика», «контроль…инженер», «проходит…review».
func TestAIAgentConceptClosesAnonBullet(t *testing.T) {
	reqs := &Requirements{
		MustHave: []Requirement{
			{Text: "Умение выстраивать рабочий процесс с использованием AI-агентов и инструментов так, чтобы они брали на себя значительную часть рутины и ускоряли результат", Kind: "must"},
			{Text: "Выстраивать и оптимизировать рабочие процессы с использованием AI-агентов и инструментов", Kind: "must"},
		},
	}
	profile := "ИИ-инструменты в разработке — активное использование: для написания кода Go и PHP (рефакторинг, тесты, диагностика багов), проектирования и анализа."
	letter := `Здравствуйте!

- **AI-агенты и инструменты: ИИ-инструменты — ежедневная практика (постановка задач, генерация, ревью, отладка); весь ИИ-код проходит code review и тесты до merge; архитектурный контроль сохраняется за инженером; RAG-инструменты CoverCraft (LLM-генерация, гибридный вердикт).**
- **Go и highload: Stable ID (Kafka, 10 000 RPS), Fraud Engine (Random Forest, 92% F1), concurrency, race detector — чист.**

Буду рад обсудить.`
	f := Evaluate(DefaultConcepts(), *reqs, profile, letter, "Backend Engineer (Go, AI Agents)")
	for _, r := range f.Covered {
		for _, c := range reqs.MustHave {
			if r.Text == c.Text {
				t.Logf("закрыто: %s", r.Text)
			}
		}
	}
	if len(f.Missing) != 0 || len(f.Caveats) != 0 {
		t.Fatalf("анон-буллет не закрыл AI-агентные требования: missing=%d caveats=%d (score=%d)",
			len(f.Missing), len(f.Caveats), f.Score)
	}
	for _, r := range f.Covered {
		ok := false
		for _, c := range reqs.MustHave {
			if r.Text == c.Text {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("закрыт посторонний факт: %s", r.Text)
		}
	}
}
