# gonotify

[![Go Reference](https://pkg.go.dev/badge/github.com/assurrussa/gonotify.svg)](https://pkg.go.dev/github.com/assurrussa/gonotify)
[![Go Report Card](https://goreportcard.com/badge/github.com/assurrussa/gonotify)](https://goreportcard.com/report/github.com/assurrussa/gonotify)
[![Go](https://github.com/assurrussa/gonotify/actions/workflows/go.yml/badge.svg)](https://github.com/assurrussa/gonotify/actions/workflows/go.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](../LICENSE)

[English](../README.md) | Русский

Библиотека для Go, предоставляющая неизменяемый движок шаблонов, контракты
транспорта, клиент к шлюзу доставки NotifyHub и обработку задач outbox.

Это русскоязычный обзор контрактов библиотеки. Примеры кода и конфигурации
поддерживаются в [основном README](../README.md); разделы ниже ссылаются на них.
Компилируемый пример доступен в [examples/quickstart/main.go](../examples/quickstart/main.go).

## Возможности

- **Иммутабельные шаблоны**: рендеринг HTML/text с автоматическим экранированием, layout, partials, кешированием (`NewCached`) и предзагрузкой (`Preload()`) через `templates`. Опция `missingkey=error` предотвращает отправку писем с опечатками в переменных.
- **Единый транспорт**: абстрактный контракт `transport.Transport` с типизированными ошибками (`QuotaError`, `RequestError` и др.) и опциональными возможностями (`ReceiptReader`, `HealthChecker`).
- **Клиент NotifyHub**: HTTP-адаптер (`transport/notifyhub`) с авторизацией по токену, запретом небезопасных редиректов, ключами идемпотентности и поддержкой RFC 7231 `Retry-After`.
- **Retry-safe outbox (v2)**: обработчик задач (`interfaces/outbox/notifications`) для предварительно отрендеренных transport-запросов со стабильным ключом идемпотентности downstream-доставки, схемой версии 2 и диспетчеризацией ошибок (`Permanent`, `RetryAt`, `DeferAt`).
- **Внедрение зависимостей**: интеграция с `godi` через пакет `di`.

## Установка

Используйте версию Go и toolchain из [go.mod](../go.mod): Go `1.27.0`
с toolchain `go1.27.1`.

[Команда установки](../README.md#installation) поддерживается в основном README.

## Поддерживаемые пакеты

Стабильная поверхность для потребителей определена в
[reference/externalconsumer/packages.go](../reference/externalconsumer/packages.go):

- `github.com/assurrussa/gonotify` — базовые алиасы типов и ошибки.
- `github.com/assurrussa/gonotify/templates` — иммутабельный движок шаблонов (`Renderer`, `PreloadableRenderer`, `RenderedContent`).
- `github.com/assurrussa/gonotify/transport` — интерфейс транспорта, модели сообщений и типизированные ошибки.
- `github.com/assurrussa/gonotify/transport/notifyhub` — HTTP-клиент транспорта к шлюзу NotifyHub и статусы доставки.
- `github.com/assurrussa/gonotify/di` — сборка зависимостей приложения через `godi`.
- `github.com/assurrussa/gonotify/interfaces/outbox/notifications` — задачи и payload для отложенных уведомлений (schema version 2).

Остальные пакеты относятся к внутренней реализации.

Из корня этого репозитория проверяйте все корни потребителей `../site`:

```bash
go run ./cmd/importpolicy --repo-root ../site --consumers backend,goadmin,goauth,fixtures
```

Для другого приложения указывайте все корни потребителей. Пропущенные
каталоги не проверяются.
Карта пакетов и контракты описаны в [обзоре проекта](project-overview.md).

## Быстрый старт: Шаблоны и NotifyHub

Для современных приложений форматирование отделено от доставки:
1. Рендеринг содержимого без побочных эффектов через `templates.Renderer`.
2. Отправка через шлюз NotifyHub с помощью `transport/notifyhub.Client`, либо постановка в очередь через `interfaces/outbox/notifications`.

> **Семантика подтверждения (Receipt)**: Успешный `Submit` (`err == nil`) означает, что NotifyHub принял и гарантированно сохранил запрос на доставку. Это не означает мгновенную отправку конечному адресату. Статусы этапов доставки (`queued`, `sending`, `accepted`, `delivered`, `bounced`) возвращаются в `Receipt.Deliveries` или запрашиваются через `ReceiptReader.Get(ctx, id)`.

> **Безопасность транспорта**: Обычный HTTP по умолчанию разрешён только для loopback-адресов (`localhost`, `127.0.0.1`, `[::1]`). Для удалённых серверов требуется HTTPS либо явное включение `AllowInsecureHTTP: true` в `notifyhub.Config` во избежание передачи `ProjectKey` в открытом виде.

`ProjectKey` используется без нормализации: конструктор отклоняет внешние пробелы, внутренние ASCII-пробелы и управляющие ASCII-байты, не раскрывая секрет в ошибке. Каждый транспорт обязан соблюдать идемпотентность. Ключи должны быть корректным UTF-8 и иметь длину 8–200 байт, без внешних пробелов и управляющих ASCII-байтов; рекомендуется ASCII.

`Request.ExpiresAt` — исключительная граница отправки и доставки; `nil` означает отсутствие срока. NotifyHub передаёт целые Unix-секунды (дробная часть отбрасывается). Клиент возвращает `ErrExpired` до HTTP-запроса, если этот срок истёк; шлюз обязан соблюдать срок и после приёма уведомления.

[Пример использования](../README.md#quick-start-templates--notifyhub) приведён в основном README.

## Движок шаблонов

Пакет `templates` читает шаблоны из любой файловой системы `fs.FS` (`os.DirFS` или `embed.FS`).
HTML-шаблоны используют `html/template` с автоматическим контекстным экранированием для защиты от XSS,
а текстовые версии и темы писем — `text/template`. Ошибочные/отсутствующие поля данных вызывают ошибку компиляции/рендеринга (`missingkey=error`).

Конструкторы:
- `templates.New(root fs.FS)`: прямое чтение файлов без кеширования.
- `templates.NewCached(root fs.FS)`: потокобезопасный кэш скомпилированных шаблонов.
- `templates.NewDir(dir string)`: прямое чтение из каталога файловой системы ОС (`os.DirFS`).
- `templates.NewCachedDir(dir string)`: кэшированный рендерер из каталога файловой системы ОС (`os.DirFS`).

Предзагрузка: `.Preload()` рекурсивно обходит вложенные каталоги сообщений,
парсит корневые layouts и partials и проверяет ссылки на именованные шаблоны.
Проверки данных выполняются при рендеринге. Ошибки доступа и чтения возвращаются
вызывающему коду; игнорируется только отсутствие необязательного файла.

## Отложенная доставка через outbox

Пакет: `github.com/assurrussa/gonotify/interfaces/outbox/notifications`.

Сообщение рендерится до постановки в очередь и сохраняется в виде готового
`transport.Request` с обязательным ключом `Request.IdempotencyKey`. Это гарантирует,
что при повторных попытках воркера downstream-сервер (NotifyHub)
не создаст дубликатов.

### Рекомендуемый хелпер продюсера: `notificationsjob.Put`

Для безопасной постановки задачи используйте функцию `notificationsjob.Put`, которая выполняет раннюю валидацию запроса, упаковывает его в payload схемы версии 2 и сохраняет в outbox:

```go
jobID, err := notificationsjob.Put(ctx, outboxService, req, time.Now())
```

Хелпер `notificationsjob.Put` обеспечивает идемпотентность downstream-доставки: обязательный `IdempotencyKey` гарантирует, что шлюз отклонит или дедуплицирует повторные попытки отправки при повторах воркера. Если приложению требуется дедупликация на уровне хранилища outbox между конкурирующими продюсерами, используйте `outboxService.PutVersionedUnique(...)`.

При необходимости ручной сериализации:
```go
payload, err := notificationsjob.MarshalPayload(notificationsjob.Payload{Request: req})
if err != nil {
	log.Fatal(err)
}

outboxService.PutVersioned(
	ctx,
	notificationsjob.JobName,       // "notifications_send"
	notificationsjob.SchemaVersion, // 2
	payload,
	time.Now(),
)
```

Задача регистрируется в воркере через:
```go
job := notificationsjob.Must(notificationsjob.NewOptions(client))
```

Обработчик реализует `outbox.VersionedJob` и поддерживает типизированную семантику ошибок:
- `outbox.Permanent` для 400 (`ErrInvalidRequest`), 409 (`ErrIdempotencyConflict`), 413 (`ErrPayloadTooLarge`) и битых payload.
- `outbox.RetryAt` с учётом заголовка `Retry-After` для 429 (`QuotaError`).
- `outbox.DeferAt` для 401 (`ErrUnauthorized`) — задача откладывается без расходования лимита попыток, позволяя оператору исправить учетные данные без падения задачи в DLQ.
- Стандартный retry воркера для сетевых ошибок и 503 (`ErrTemporarilyUnavailable`).
- Корректный просроченный запрос, `ErrExpired` от транспорта или ошибка отправки после истечения срока завершают задачу с `nil` (ack/drop), без retry и DLQ. Уже просроченные задачи не отправляются. Отсрочки 401/429 ограничены `ExpiresAt`; некорректные payload по-прежнему попадают в DLQ.

> [!NOTE]
> **Требования к outbox backend**: `Job.Handle` возвращает `outbox.DeferAt` для ошибок 401 Unauthorized, откладывая задачу без расходования попыток. Выбирайте версии бэкенд-модулей, реализующие `DeferJobsRepository` и совместимые с outbox v0.15. Бэкенды PostgreSQL, MySQL, SQLite и Picodata версионируются как отдельные Go-модули: обновление корневого outbox не обновляет их автоматически. Перед обновлением воркеров проверьте выбранные версии через `go list -m all`.

## Интеграция с DI (godi)

Пакет `di.ModuleBootstrap()` регистрирует провайдеры для:
- `transport.Transport` через `di.ProvideNotifyHubClient`.
- `*notificationsjob.Job` через `di.ProvideOutboxJob`.

## Разработка и верификация

[Основные команды](../README.md#development-and-verification) поддерживаются
в основном README; полный список — в [документации разработки](development.md).

`make check` — полный локальный gate: проверка зависимостей, форматирование,
проверка типов, линтер, тесты, тесты с детектором гонок, trimpath и локальный изолированный потребитель.
