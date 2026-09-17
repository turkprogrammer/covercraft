// Command covercraft — генератор сопроводительных писем: нативное окно
// (WebKit2GTK) + встроенный HTTP-сервер на 127.0.0.1 и один HTML-файл.
//
// Design: KISS, YAGNI. Один бинарник, UI зашит через embed, настройки
// хранит Go в ~/.config/covercraft/settings.json (не LocalStorage —
// случайный порт меняет origin при каждом запуске). LLM — любой
// OpenAI-совместимый endpoint (Ollama, OpenAI, OpenRouter).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/turkprogrammer/covercraft/internal/server"
	"github.com/turkprogrammer/covercraft/internal/settings"
	"github.com/turkprogrammer/covercraft/internal/window"
)

func main() {
	// Движок фита задаётся в settings.json (fitEngine: "llm" — гибридная
	// разметка покрытия моделью). Ошибка чтения настроек не фатальна: тогда
	// работает детерминированный матчер, как и раньше.
	engine := ""
	if s, err := settings.Load(); err == nil {
		engine = s.FitEngine
	}
	addr, err := server.ListenAndServeRandomPort(server.New(server.Config{
		ContextDir: contextDir(),
		FitEngine:  engine,
	}))
	if err != nil {
		fail(err)
	}
	// Для отладки: где UI живёт на самом деле.
	fmt.Fprintln(os.Stderr, "covercraft:", addr)

	if err := window.Run(addr, "CoverCraft", "covercraft", 1100, 800); err != nil {
		fail(err)
	}
}

// contextDir ищет папку context/*.md: сначала рядом с бинарником
// (распакованный архив), затем в текущей папке (go run / разработка).
func contextDir() string {
	exe, err := os.Executable()
	if err == nil {
		d := filepath.Join(filepath.Dir(exe), "context")
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return "context"
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ошибка:", err)
	os.Exit(1)
}
