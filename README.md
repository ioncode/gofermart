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
*   **Гибридный событийный воркер (Event-Driven + Polling):** Начисление бонусов обрабатывается асинхронно через буферизированный канал оперативной памяти на 1000 элементов. В качестве fallback-стратегии развернут фоновый тикер, пуллящий промежуточные статусы заказов из базы данных. Подробности в [internal/worker/accrual.go](./internal/worker/accrual.go).
*   **Отказоустойчивость (Fault Tolerance):** Все итерации воркеров изолированы через механизмы `recover()`, предотвращая падение веб-сервера при паниках.
*   **Умный воркер (Backoff):** Воркер умеет цивилизованно читать заголовки `Retry-After` от внешних Rate-Limiter'ов системы начислений, динамически переводя горутины в управляемый сон с поддержкой плавного завершения (`Graceful Shutdown`) внутри [cmd/gophermart/main.go](./cmd/gophermart/main.go).

### 💾 Слой хранения данных (Database Layer)
*   **Нативный бинарный драйвер `pgx/v5`:** Приложение полностью отказалось от ORM в пользу прямого бинарного протокола взаимодействия с PostgreSQL, что исключает накладные расходы на парсинг SQL-строк. Инициализация пула описана в [internal/repository/postgres/postgres.go](./internal/repository/postgres/postgres.go).
*   **Точная финтех-математика:** На всех уровнях полностью исключен тип `float64`. Расчеты ведутся с использованием типа высокой точности `shopspring/decimal` (в СУБД — тип `NUMERIC(12, 2)`). Контракты зафиксированы в интерфейсах [internal/repository/repository.go](./internal/repository/repository.go).
*   **Монолитные миграции (`go:embed`):** SQL-скрипты таблиц и составных B-Tree индексов физически вшиваются в скомпилированный бинарный файл. Сами файлы расположены в папке [internal/repository/postgres/migrations/](./internal/repository/postgres/migrations/).
*   **Шторм-защита облачного деплоя:** На этапе холодного старта подов в Kubernetes применяется неблокирующий замок `pg_try_advisory_lock(42)`. Миграции накатывает строго один под, остальные мгновенно пропускают этот шаг.
*   **Детерминизм транзакций:** Порядок SQL-запросов синхронизирован по вектору `users -> orders` в [internal/repository/postgres/order_accrual.go](./internal/repository/postgres/order_accrual.go). Балансы застрахованы от гонок данных и ухода в минус на уровне ограничений целостности СУБД (`CHECK CONSTRAINT balance >= 0.00`) с прозрачным маппингом кодов ошибок PostgreSQL в доменные статусы внутри [internal/repository/postgres/balance.go](./internal/repository/postgres/balance.go).

---

## 🧪 Стратегия тестирования

Проект защищен двухуровневой системой автоматизированного контроля качества:
1. **Unit-тестирование (Слой Handlers & Services):** HTTP-контроллеры и бизнес-кейсы покрыты изолированными тестами с автоматической генерацией мок-интерфейсов пакетами `mockgen` и `mockery`. Примеры реализации: [internal/handler/balance_test.go](./internal/handler/balance_test.go) и [internal/handler/order_test.go](./internal/handler/order_test.go).
2. **Интеграционное тестирование (Слой Postgres DAL):** Критические финансовые операции и стейт-машина заказов покрыты интеграционными тестами в пакете `internal/repository/postgres/`. 

Интеграционные тесты объединены в продвинутые тестовые сьюты (`testify/suite`) и запускаются на базе реального пула СУБД PostgreSQL с автоматическим накатом схемы встроенных миграций с помощью фикстуры **[BasePostgresTestSuite](./internal/repository/postgres/suite_test.go)**. Это гарантирует 100% проверку:
* Атомарности ACID-транзакций при одновременном начислении кэшбека и изменении статусов ([internal/repository/postgres/order_accrual_test.go](./internal/repository/postgres/order_accrual_test.go)).
* Корректности работы составных B-Tree индексов и сортировки исторических данных СУБД ([internal/repository/postgres/user_test.go](./internal/repository/postgres/user_test.go)).
* Аппаратных рубежей защиты целостности данных (`CHECK CONSTRAINT` и ограничений уникальности `unique_violation`).

### Запуск полного цикла тестирования:
```bash
# 1. Генерация свежих мок-объектов
go generate ./...

# 2. Запуск unit и интеграционных тестов с детектором состояния гонки (Race Detector)
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
