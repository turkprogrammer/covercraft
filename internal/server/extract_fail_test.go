package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// letterOK — валидное короткое письмо для обеих проверок.
const letterOK = "Здравствуйте!\n\n**Go и highload:** Stable ID — Kafka, 10 000 RPS.\n\nБуду рад обсудить задачи."

func genStreamOK(ctx context.Context, system, user string, onDelta func(string)) (string, error) {
	onDelta(letterOK)
	return letterOK, nil
}

// failures — обе причины, по которым разбор вакансии не даёт требований.
// Живой баг (октябрь 2026): после нескольких итераций автофикса панель
// вердикта исчезала, а кнопка fit-fix оставалась.
func TestExtractionFailureIsExplainedToUser(t *testing.T) {
	cases := []struct {
		name string
		fit  LLMFunc
	}{
		// Провайдер упал/таймаут: err глотался молча, extractOK=false.
		{"ошибка вызова", func(ctx context.Context, system, user string) (string, error) {
			return "", context.DeadlineExceeded
		}},
		// Модель ответила JSON без must_have/nice_to_have: extractOK=true,
		// но Verdict=="" — тот же визуальный баг, плюс nMust=0 → лимит 200.
		{"JSON без требований", func(ctx context.Context, system, user string) (string, error) {
			return `{"role":"go-primary"}`, nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "01-профиль.md"),
				[]byte("## КАРТА ФАКТОВ\n- **Stable ID:** Kafka, 10 000 RPS\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			h := New(Config{
				ContextDir: dir,
				LLMStream:  genStreamOK,
				FitLLM:     tc.fit,
			})
			body, _ := json.Marshal(map[string]any{"vacancy": "Go backend engineer (Telephony & VoIP)"})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(rec, req)

			ev := parseSSE(t, rec.Body.String())
			if ev.Done == nil {
				t.Fatal("нет done-события")
			}
			// Письмо важнее разбора: оно обязано уцелеть.
			if ev.Done.Letter != letterOK {
				t.Errorf("письмо потеряно: %q", ev.Done.Letter)
			}
			// Пользователь должен узнать, почему вердикта нет. Молчаливое
			// исчезновение панели при живой кнопке fit-fix — ровно баг.
			if ev.Done.Fit == nil {
				if !strings.Contains(ev.Done.FitNote, "разбор вакансии") {
					t.Errorf("нет вердикта и нет объяснения: fitNote=%q warnings=%v",
						ev.Done.FitNote, ev.Done.Warnings)
				}
				// Объяснение уходит отдельным полем, а не в warnings:
				// warnings при следующей автоправке уходят модели как
				// дефекты письма — «упал разбор вакансии» дефектом не является.
				for _, w := range ev.Done.Warnings {
					if strings.Contains(w, "разбор вакансии") {
						t.Errorf("причина попала в warnings и станет дефектом письма: %q", w)
					}
				}
			} else {
				t.Errorf("ожидал пустой вердикт, got %q", ev.Done.Fit.Verdict)
			}
			t.Logf("fitNote=%q warnings=%v", ev.Done.FitNote, ev.Done.Warnings)
		})
	}
}
