package fit

import (
	"testing"
)

// Baseline зафиксирован на Intel i3-10100 @ 3.60GHz, go1.26, linux/amd64
// (count=6, разброс <5%). Правки кода, замедляющие фит, должны быть видны здесь.
//
//	BenchmarkEvaluateSingle   ~3.67 ms/op   172 KB/op   2856 allocs/op
//	BenchmarkEvaluate        ~41.6  ms/op   2.00 MB/op  34718 allocs/op  (10 кейсов)
//	BenchmarkDefaultConcepts ~1.75  ms/op   1.72 MB/op  10490 allocs/op
//
// Известные кандидаты на оптимизацию (замерены, но НЕ тронуты — выигрыш
// неощутим для пользователя на фоне генерации письма через LLM):
//
//   - Evaluate: 83% CPU в regexp.backtrack, из них 32% в unicode.SimpleFold.
//     understandingRe несёт (?i) и гоняется на тексте, который в шести из
//     девяти мест вызова уже приведён к lower. Снятие (?i) на нормализованном
//     тексте даёт 112 мкс → 33 мкс (3.4x) на этой регулярке. Снимать точечно
//     нельзя: sentences() не lowercasing, поэтому три вызова идут на сырой
//     текст. Нужна нормализация на границе Evaluate; эквивалентность вердиктов
//     проверена на всех 10 golden-кейсах (Verdict и Score совпали побитово).
//
//   - DefaultConcepts: 126 regexp.MustCompile на каждый вызов. sync.Once
//     неприменим — наружу уходит изменяемый слайс. Корректно: пакетные var
//     + копия под синхронизацией, либо отказ от пересборки на горячем пути.
//
// Сознательно не включено: копии регулярок ради A/B-замера (?i). Дубликат
// продакшн-регулярки в тесте молча разошёлся бы с оригиналом при правке
// fit.go и продолжил показывать несуществующий выигрыш. Числа выше — в этом
// же коммите.

// BenchmarkEvaluate — замер горячего пути фита на реальных golden-кейсах.
// Один вызов Evaluate на каждую итерацию: ровно столько делает сервер после
// генерации письма (server.go: fitVerdict → fit.Evaluate).
func BenchmarkEvaluate(b *testing.B) {
	c := DefaultConcepts()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range cases {
			reqs := mustReqs(k.MustHave, k.NiceToHave, k.Role)
			Evaluate(c, reqs, k.Profile, k.Letter, k.Vacancy)
		}
	}
}

// BenchmarkEvaluateSingle — изоляция одного кейса: 10 кейсов в цикле
// смазывают вклад конкретного, этот даёт чистое время на одно письмо.
func BenchmarkEvaluateSingle(b *testing.B) {
	c := DefaultConcepts()
	k := cases[0]
	reqs := mustReqs(k.MustHave, k.NiceToHave, k.Role)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Evaluate(c, reqs, k.Profile, k.Letter, k.Vacancy)
	}
}

// BenchmarkDefaultConcepts — стоимость пересборки концептов. Вызывается на
// каждый запрос; если тут аллокации, виновник найден.
func BenchmarkDefaultConcepts(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = DefaultConcepts()
	}
}
