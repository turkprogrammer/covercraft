# Contributing

Спасибо за интерес к CoverCraft! PR-ы приветствуются — процесс короткий.

## Быстрый старт

```bash
# dev-заголовки (рантайм уже есть в Ubuntu 22.04+):
sudo apt install build-essential pkg-config libwebkit2gtk-4.1-dev

git clone https://github.com/turkprogrammer/covercraft
cd covercraft
go build -o covercraft .
./covercraft
```

## Сборка и тесты

Перед отправкой PR:

```bash
go build ./...
go vet ./...
go test ./...
```

Всё должно быть зелёным. Тесты есть у всех пакетов с логикой:
settings (roundtrip, битый JSON, права 0600), llm-клиент (httptest:
запрос/ответ/ошибки), cover (сборка промпта), server (эндпоинты,
включая 400/502), frontend (embed-проверки).

## Правила

- KISS/YAGNI: один HTML-файл UI без бандлеров, минимальные абстракции.
- Новая фича → тест в том же PR.
- Ломающее изменение API `/api/*` — отдельно и с обоснованием в PR.
- Коммиты: короткое сообщение в императиве («Add», «Fix», а не
  «Added», «Fixes»).
- UI-правки не должны ломать тёмную/светлую темы (обе через
  `prefers-color-scheme`).

## PR-процесс

1. Форк → ветка `feature/...` или `fix/...`.
2. Один PR = одна задача.
3. Опишите, что и зачем; скриншот — если UI менялся.
4. Мелкие PR-ы (до ~400 строк diff) быстрее всего проходят review.

## Чего не хватает (хорошие первые задачи)

- CI на GitHub Actions (build + test).
- Flatpak/Snap-упаковка.
- Экспорт письма в .txt по кнопке.
- Локализация UI (сейчас ru/en вперемешку).
