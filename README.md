# Накопительная система лояльности «Гофермарт» (Gophermart)

Проект представляет собой высокопроизводительный production-ready микросервис накопительной системы бонусов для e-commerce, реализованный на языке **Go**. Сервис управляет профилями пользователей, обрабатывает транзакции списания и начисления баллов лояльности, а также интегрируется с внешней системой расчета бонусов по асинхронной схеме.

Проект разработан по методологии **Clean Architecture (многослойная архитектура)** с жестким разделением ответственности (SOLID / ISP).

---

## 🏗 Архитектурный и Технологический стек

### 🌐 Сетевой слой (Transport & Routing)
*   **Zero-Allocation I/O буферизация:** Чтение входящих HTTP-запросов и потоковый стриминг ответов в сетевые сокеты оптимизированы через `sync.Pool` и кастомные текстовые ограничители, что снижает нагрузку на Garbage Collector до абсолютного минимума (0 B/op на сетевом уровне). Реализовано в модуле [internal/handler/helper.go](./internal/handler/helper.go).
*   **Защита периметра (Security & DoS):** Сетевые сокеты защищены жесткими таймаутами (`ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`) от атак класса Slowloris DoS. Внедрено Middleware, аппаратно разрывающее TCP-сессию на уровне ядра ОС при превышении лимита размера тела. Реализовано в модуле [internal/router/http_server.go](./internal/router/http_server.go).
*   **Drop-in JSON Парсер:** Вместо стандартного `encoding/json` задействован высокопроизводительный ассемблерный кодировщик `goccy/go-json` во всех [транспортных DTO](./internal/handler/contract.go).
*   **Безопасность сессий:** Авторизация реализована через JWT-токены (библиотека `golang-jwt/jwt/v5`) с явной валидацией алгоритма подписи для защиты от уязвимостей типа `alg: none`. На уровне HTTP-кук выставлены флаги безопасности `HttpOnly`, `Secure` и режим `SameSiteLax` внутри [internal/handler/middleware.go](./internal/handler/middleware.go).

### ⚙️ Слой бизнес-логики и фоновых процессов (Domain & Worker)
*   **Гибридный декомпозированный воркер (Event-Driven + Polling):** Асинхронная обработка начислений через буферизированный канал с разделением на изолированные компоненты (`client.go`, `throttler.go`, `service.go`, `worker.go`). Подробнее см. в [internal/worker/accrual/](internal/worker/accrual/).
*   **Защита от CPU Burn и лавины 429 (Backpressure):** Динамическое переключение канала в `nil` и превентивное сглаживание нагрузки (Token Bucket). Настройка в [internal/worker/accrual/worker.go](internal/worker/accrual/worker.go).
*   **Финтех-консистентность и Fault Tolerance:** Изоляция паник через `recover()` и защита транзакций. Описание контура в [internal/worker/accrual/service.go](internal/worker/accrual/service.go).

### 💾 Слой хранения данных (Database Layer)
*   **Нативный бинарный драйвер `pgx/v5`:** Приложение полностью отказалось от ORM в пользу прямого бинарного протокола взаимодействия с PostgreSQL, что исключает накладные расходы на парсинг SQL-строк. Инициализация пула описана в [internal/repository/postgres/postgres.go](./internal/repository/postgres/postgres.go).
*   **Точная финтех-математика:** На всех уровнях полностью исключен тип `float64`. Расчеты ведутся с использованием типа высокой точности `shopspring/decimal` (в СУБД — тип `NUMERIC(12, 2)`). Контракты зафиксированы в интерфейсах [internal/repository/repository.go](./internal/repository/repository.go).
*   **Монолитные миграции (`go:embed`):** SQL-скрипты таблиц и составных B-Tree индексов физически вшиваются в скомпилированный бинарный файл. Сами файлы расположены в папке [internal/repository/postgres/migrations/](./internal/repository/postgres/migrations/).
*   **Шторм-защита облачного деплоя:** На этапе холодного старта подов в Kubernetes применяется неблокирующий замок `pg_try_advisory_lock(42)`. Миграции накатывает строго один под, остальные мгновенно пропускают этот шаг.
*   **Детерминизм транзакций:** Порядок SQL-запросов синхронизирован по вектору `users -> orders` в [internal/repository/postgres/order_accrual.go](./internal/repository/postgres/order_accrual.go). Балансы застрахованы от гонок данных и ухода в минус на уровне ограничений целостности СУБД (`CHECK CONSTRAINT balance >= 0.00`) с прозрачным маппингом кодов ошибок PostgreSQL в доменные статусы внутри [internal/repository/postgres/balance.go](./internal/repository/postgres/balance.go).

---

## 🧪 Стратегия тестирования

Проект защищен развитой многоуровневой системой автоматизированного контроля качества, исключающей регрессии при рефакторинге кодовой базы:

1. **Unit-тестирование (Слой Services & DTO):** Бизнес-логика, криптография и валидация моделей полностью изолированы и покрыты модульными тестами с автоматической генерацией мок-интерфейсов (`mockgen`/`mockery`). Примеры: [internal/handler/balance_test.go](./internal/handler/balance_test.go), [internal/handler/order_test.go](./internal/handler/order_test.go).
2. **Интеграционное тестирование API (Слой Router & Middleware):** Сквозная проверка сетевого периметра реализована в модуле [internal/router/http_server_test.go](./internal/router/http_server_test.go). С помощью встроенных механизмов `net/http/httptest` тестируется вся цепочка прохождения HTTP-пакета (маршруты, gzip-сжатие, авторизационные middleware, DoS-лимитеры).
3. **Интеграционное тестирование СУБД (Слой Postgres DAL + Testcontainers):** Реализовано на базе продвинутых тестовых сьютов (`testify/suite`) с использованием фикстуры **[BasePostgresTestSuite](./internal/repository/postgres/suite_test.go)**. 

### 🛸 Инфраструктурная магия Testcontainers-Go
DAL-тесты не требуют ручного развертывания локальной базы данных или внешних зависимостей. При запуске тестов фикстура `BasePostgresTestSuite` автоматически:
*   Через программное управление Docker-демоном скачивает и запускает изолированный контейнер `postgres:16-alpine` в изолированном сетевом контуре.
*   Применяет механизм **Retry-подключений** (10 попыток с задержкой) для защиты от EOF-сбоев сетевого стека виртуализации (актуально при запуске в Docker Desktop на Windows / macOS).
*   Автоматически накатывает встроенные боевые [SQL-миграции](./internal/repository/postgres/migrations/).
*   **Изолирует тест-кейсы:** Перед каждым тестом вызывается метод `TearDownTest()`, который выполняет быструю атомарную очистку таблиц через `TRUNCATE users CASCADE`, гарантируя отсутствие загрязнения данных (Data Contamination) между проверками.
*   **Утилизирует окружение:** По завершении сьюта метод `Terminate()` полностью тушит и удаляет Docker-контейнер, высвобождая ресурсы хост-системы.

Проверяется атомарность ACID-транзакций при одновременном начислении кэшбека ([internal/repository/postgres/order_accrual_test.go](./internal/repository/postgres/order_accrual_test.go)) и корректность индексов ([internal/repository/postgres/user_test.go](./internal/repository/postgres/user_test.go)).

### Запуск полного цикла тестирования:
Убедитесь, что на вашей локальной машине или в CI/CD агенте запущен **Docker-демон**, и выполните:
```bash
# 1. Автоматическая генерация свежих мок-объектов
go generate ./...

# 2. Запуск unit и всех уровней интеграционных тестов с включенным детектором гонок (Race Detector)
go test -v -race ./...
```

---

## 🚀 Быстрый старт

### 1. Локальное окружение (Docker Compose)
Развертывание СУБД PostgreSQL и готового окружения для интеграционного тестирования:
```bash
docker-compose up -d
```

### 2. Сборка и Запуск приложения
```bash
go build -o cmd/gophermart/gophermart cmd/gophermart/main.go
./cmd/gophermart/gophermart -a :8080 -d "postgres://postgres:pass@localhost:5432/gofermart?sslmode=disable" -r "http://localhost:8081"
```

---

## 📂 Структура проекта
*   [cmd/gophermart/](./cmd/gophermart/) — точка входа, инициализация слоев, запуск фоновых задач и управление жизненным циклом приложения (Graceful Shutdown).
*   [internal/config/](./internal/config/) — парсинг флагов командной строки, чтение переменных окружения и сборка единой неизменяемой структуры конфигурации приложения.
*   [internal/handler/](./internal/handler/) — REST API эндпоинты, высокопроизводительные DTO-структуры и пулы переиспользования памяти (`sync.Pool`).
*   [internal/router/](./internal/router/) — конфигурация маршрутов (роутер `chi`), DoS-лимитеры, логирование (`ulog`) и сетевые таймауты сокетов.
*   [internal/service/](./internal/service/) — ядро бизнес-логики (Use Cases), криптография паролей (`bcrypt`) и управление сессиями.
*   [internal/worker/](./internal/worker/) — фоновый асинхронный демон интеграции с внешней системой начислений бонусов.
*   [internal/repository/](./internal/repository/) — абстракции данных, транзакционные контракты финтех-контура и Postgres-реализация на базе `pgx/v5`.
*   [pkg/luhn/](./pkg/luhn/) — изолированный утилитарный пакет валидации номеров карт и заказов по алгоритму Луна.
