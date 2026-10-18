# PulseWatch

Сервис мониторинга доступности HTTP-сервисов на Go: gRPC API, планировщик проверок с пулом воркеров, история в PostgreSQL, метрики Prometheus и алерты.

Pet-проект для прокачки Go, gRPC, PostgreSQL и инфраструктурных практик: Docker, метрики, структурное логирование, graceful shutdown, CI.

## Возможности

- **gRPC API**: `AddTarget`, `RemoveTarget`, `ListTargets`, `ListChecks`, `GetStats`; включены health-check и reflection.
- **Планировщик проверок**: по горутине-тикеру на каждую цель, выполнение — ограниченным пулом воркеров.
- **Backpressure**: при переполнении очереди проверки пропускаются и учитываются в метрике `pulsewatch_jobs_dropped_total`; планировщик не блокируется на медленных целях.
- **История в PostgreSQL**: таблицы `targets` и `checks`, индекс под выборку последних проверок цели.
- **Агрегация в SQL**: uptime, средняя и p95 задержка одним запросом (`FILTER` + `PERCENTILE_CONT`).
- **Метрики Prometheus**: проверки, длительности, состояние целей, алерты, дропы очереди, gRPC-запросы.
- **Алерты**: серия из N подряд неудачных проверок → один алерт на инцидент + сообщение о восстановлении.
- **Graceful shutdown** по SIGTERM/SIGINT, конфигурация из переменных окружения, логи через `log/slog`.
- **Docker**: multi-stage сборка, docker-compose с PostgreSQL и Prometheus.
- **CI**: GitHub Actions (build + vet + test).

## Архитектура

```mermaid
flowchart LR
    A[(targets)] --> B[Manager<br/>тикер на цель]
    B --> C[jobs chan<br/>буфер]
    C --> D[Worker pool<br/>N воркеров]
    D --> E[HTTP-проба]
    E --> F[(checks)]
    D --> G[Prometheus<br/>:2112/metrics]
    D --> H[Alerter]
    I[gRPC API<br/>:50051] --> A
    I --> B
```

Поток проверки: `manager` держит по горутине-тикеру на каждую цель → задание попадает в буферизованный канал → воркер выполняет HTTP-пробу, пишет результат в PostgreSQL, обновляет метрики и передаёт результат в `alerter`.

## Быстрый старт (Docker)

```bash
make up      # postgres + pulsewatch + prometheus
make seed    # демо-цели через grpcurl
```

- gRPC: `localhost:50051`
- метрики: http://localhost:2112/metrics
- Prometheus: http://localhost:9090
- PostgreSQL: `localhost:5433` (пользователь/пароль/БД: `pulsewatch`)

Остановить и удалить данные: `make down`.

## Локальный запуск

Нужны Go 1.27+, PostgreSQL и (только для `make proto`) protoc с плагинами `protoc-gen-go`, `protoc-gen-go-grpc`.

```bash
export PULSEWATCH_POSTGRES_DSN='postgres://pulsewatch:pulsewatch@localhost:5433/pulsewatch?sslmode=disable'
go run ./cmd/pulsewatch
```

Схема создаётся автоматически при старте: идемпотентный SQL встроен в бинарник через `embed`.

### Конфигурация

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `PULSEWATCH_POSTGRES_DSN` | — (обязательна) | DSN PostgreSQL |
| `PULSEWATCH_GRPC_ADDR` | `:50051` | адрес gRPC-сервера |
| `PULSEWATCH_METRICS_ADDR` | `:2112` | адрес HTTP-сервера метрик |
| `PULSEWATCH_WORKERS` | `8` | число воркеров в пуле |
| `PULSEWATCH_QUEUE_SIZE` | `128` | размер буфера очереди заданий |
| `PULSEWATCH_ALERT_THRESHOLD` | `3` | неудач подряд до алерта |
| `PULSEWATCH_CHECK_TIMEOUT` | `5s` | таймаут одной HTTP-пробы |
| `PULSEWATCH_SHUTDOWN_TIMEOUT` | `10s` | таймаут graceful shutdown |
| `PULSEWATCH_LOG_LEVEL` | `info` | уровень логов (`debug`, `info`, `warn`, `error`) |

## gRPC API

Reflection включён, поэтому `grpcurl` работает без локальных proto-файлов:

```bash
grpcurl -plaintext localhost:50051 list

grpcurl -plaintext -d '{"url":"https://example.com","name":"example","interval_seconds":30}' \
  localhost:50051 pulsewatch.v1.PulseWatch/AddTarget

grpcurl -plaintext -d '{"target_id":1}' localhost:50051 pulsewatch.v1.PulseWatch/ListChecks
grpcurl -plaintext -d '{"target_id":1,"window_hours":24}' localhost:50051 pulsewatch.v1.PulseWatch/GetStats
grpcurl -plaintext -d '{"id":1}' localhost:50051 pulsewatch.v1.PulseWatch/RemoveTarget
```

`GetStats` возвращает `total_checks`, `failed_checks`, `uptime_percent`, `avg_latency_ms`, `p95_latency_ms`.
`AddTarget` идемпотентен по URL: повторный вызов обновляет имя и интервал. Интервал ограничивается диапазоном 5–3600 секунд.

## Метрики

| Метрика | Тип | Описание |
|---|---|---|
| `pulsewatch_checks_total{target,result}` | counter | число проверок (`ok` / `failed`) |
| `pulsewatch_check_duration_seconds{target}` | histogram | длительность проверок |
| `pulsewatch_target_up{target}` | gauge | `1`, если последняя проверка успешна |
| `pulsewatch_alerts_total{target}` | counter | число сработавших алертов |
| `pulsewatch_jobs_dropped_total` | counter | проверки, пропущенные из-за полной очереди |
| `pulsewatch_grpc_requests_total{method,code}` | counter | gRPC-запросы по методам и кодам |
| `pulsewatch_grpc_request_duration_seconds{method}` | histogram | длительность gRPC-запросов |

Примеры PromQL:

```promql
# цели, которые сейчас лежат
pulsewatch_target_up == 0

# частота неуспешных проверок по целям за 5 минут
sum by (target) (rate(pulsewatch_checks_total{result="failed"}[5m]))

# p95 длительности проверок
histogram_quantile(0.95, sum by (le) (rate(pulsewatch_check_duration_seconds_bucket[5m])))

# проверки, потерянные из-за переполнения очереди
rate(pulsewatch_jobs_dropped_total[5m])
```

## Алерты

Сервис считает неудачные проверки подряд. После `PULSEWATCH_ALERT_THRESHOLD` ошибок логируется один `ALERT: target is down` и инкрементится `pulsewatch_alerts_total`; повторные ошибки в рамках того же инцидента алерт не дублируют. После успешной проверки пишется `target recovered`, состояние сбрасывается. В проде вместо логов — Alertmanager или webhook.

```
level=WARN msg="ALERT: target is down" target=http://127.0.0.1:8099/ consecutive_failures=3
level=INFO msg="target recovered" target=http://127.0.0.1:8099/
```

## Тесты

```bash
make test              # unit-тесты: prober, worker pool, alerter
make test-integration  # против реального PostgreSQL (нужен make up)
```

Unit-тесты не требуют БД: `storage` скрыт за интерфейсом, `Prober` подменяется фейком. Покрыты границы HTTP-статусов, таймаут пробы, лимит параллелизма пула, дропы при полной очереди, срабатывание алерта один раз на инцидент. Интеграционный тест (build tag `integration`) проверяет схему, идемпотентный upsert, вставку проверок и SQL-агрегации.

## Решения и компромиссы

- **Пул воркеров вместо горутины на проверку.** Число одновременных запросов ограничено, пики не плодят тысячи горутин.
- **Буферизованная очередь с дропами.** Если воркеры не успевают, проверка пропускается и считается в `pulsewatch_jobs_dropped_total`: планировщик не блокируется, деградация видна в метриках.
- **Агрегация в PostgreSQL.** `FILTER` и `PERCENTILE_CONT` считают uptime/avg/p95 одним запросом; индекс `(target_id, checked_at DESC)` покрывает выборку по окну. При росте данных — партиционирование `checks` по времени.
- **pgx вместо `database/sql`.** Нативный пул соединений и конкурентные вставки из воркеров без обёрток.
- **Кардинальность метрик.** Лейбл `target` — URL; для больших инсталляций стоит перейти на id + отдельный маппинг.
- **Схема через `embed`, без миграционного фреймворка.** Для пет-проекта достаточно идемпотентного SQL на старте; в проде — golang-migrate или goose.

## Структура

```
cmd/pulsewatch/   main: сборка компонентов, graceful shutdown
api/proto/        protobuf-контракт
gen/              сгенерированный gRPC-код
internal/
  config/         конфигурация из env
  model/          доменные типы
  storage/        PostgreSQL: targets, checks, агрегации
  checker/        HTTP-проба, планировщик, пул воркеров
  alerter/        алерты по серии неудач
  metrics/        инструменты Prometheus
  server/         gRPC-хендлеры и интерсепторы
migrations/       SQL-схема (embed)
deploy/           Dockerfile, docker-compose, prometheus.yml
scripts/seed.sh   демо-цели
```

## Roadmap

- [ ] Дашборд Grafana (dashboard as code)
- [ ] Партиционирование `checks` по времени
- [ ] Батчевые вставки результатов проверок
- [ ] Уведомления через webhook вместо логов
- [ ] Kubernetes-манифесты
