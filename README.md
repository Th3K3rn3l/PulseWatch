# PulseWatch

Сервис на Go, который периодически проверяет HTTP-эндпоинты и складывает результаты в PostgreSQL. Целями и историей проверок можно управлять через gRPC, метрики отдаются в Prometheus, а после серии ошибок в лог пишется алерт.

Это пет-проект. Делал его, чтобы разобраться с gRPC, пулом воркеров на горутинах и PostgreSQL — и заодно собрать вокруг нормальную обвязку: метрики, graceful shutdown, Docker, CI.

## Что умеет

- Добавлять, удалять и перечислять цели через gRPC. Повторный `AddTarget` с тем же URL обновляет имя и интервал.
- Проверять цели по расписанию: на каждую цель свой тикер, проверки выполняет общий пул воркеров.
- Хранить историю проверок и отдавать последние через `ListChecks`.
- Считать uptime, среднюю и p95 задержку за окно времени.
- Отдавать метрики Prometheus: проверки, длительность, состояние целей, алерты, дропы очереди и gRPC-запросы.
- Писать `ALERT` после N подряд неудачных проверок (по умолчанию 3) и `target recovered`, когда цель снова отвечает. На один инцидент — один алерт.
- Завершаться по SIGTERM, не обрывая текущие запросы.

## Как устроено

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

На каждую цель менеджер запускает горутину с тикером. Она кладёт задание в буферизованный канал, а воркеры разбирают его: делают HTTP-запрос, пишут результат в базу, обновляют метрики и передают проверку алертеру.

Очередь ограничена. Если воркеры не успевают, проверка пропускается, а `pulsewatch_jobs_dropped_total` растёт. Планировщик при этом не блокируется: лучше потерять одну проверку и увидеть это в метрике, чем копить горутины за спиной.

## Запуск

С Docker:

```bash
make up
make seed
```

`make up` поднимает PostgreSQL, сервис и Prometheus. Порты: gRPC — 50051, метрики — 2112, Prometheus — 9090, PostgreSQL — 5433 на хосте (чтобы не конфликтовать с локальным). `make seed` добавляет несколько целей через grpcurl.

Без Docker нужны Go 1.27+ и PostgreSQL:

```bash
export PULSEWATCH_POSTGRES_DSN='postgres://pulsewatch:pulsewatch@localhost:5432/pulsewatch?sslmode=disable'
go run ./cmd/pulsewatch
```

Схема создаётся при старте, отдельно накатывать миграции не нужно.

### Настройки

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

Reflection включён, поэтому grpcurl работает без локальных proto-файлов:

```bash
grpcurl -plaintext localhost:50051 list

grpcurl -plaintext -d '{"url":"https://example.com","name":"example","interval_seconds":30}' \
  localhost:50051 pulsewatch.v1.PulseWatch/AddTarget

grpcurl -plaintext -d '{"target_id":1}' localhost:50051 pulsewatch.v1.PulseWatch/ListChecks
grpcurl -plaintext -d '{"target_id":1,"window_hours":24}' localhost:50051 pulsewatch.v1.PulseWatch/GetStats
grpcurl -plaintext -d '{"id":1}' localhost:50051 pulsewatch.v1.PulseWatch/RemoveTarget
```

`GetStats` возвращает `total_checks`, `failed_checks`, `uptime_percent`, `avg_latency_ms`, `p95_latency_ms`. Интервал проверки ограничен диапазоном 5–3600 секунд.

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

Пара запросов для Prometheus:

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

Сервис считает неудачные проверки подряд. После `PULSEWATCH_ALERT_THRESHOLD` ошибок пишется один `ALERT: target is down`, а `pulsewatch_alerts_total` увеличивается на единицу. Повторные ошибки в том же инциденте алерт не дублируют. Когда цель снова отвечает, пишется `target recovered`, состояние сбрасывается.

```
level=WARN msg="ALERT: target is down" target=http://127.0.0.1:8099/ consecutive_failures=3
level=INFO msg="target recovered" target=http://127.0.0.1:8099/
```

В проде вместо логов был бы Alertmanager или webhook, но для пет-проекта логов достаточно.

## Тесты

```bash
make test              # unit-тесты
make test-integration  # против реального PostgreSQL (нужен make up)
```

Unit-тесты не требуют базы: хранилище скрыто за интерфейсом, HTTP-проба подменяется фейком. Проверяются границы HTTP-статусов, таймаут, лимит параллелизма пула, дропы при полной очереди и срабатывание алерта. Интеграционный тест (build tag `integration`) проверяет схему, идемпотентный upsert, вставку проверок и SQL-агрегации.

## Почему сделано так

**pgx вместо `database/sql`.** Нужен пул соединений и конкурентные вставки из воркеров. pgx даёт это без дополнительных обёрток.

**Агрегация в PostgreSQL.** uptime и p95 считаются одним запросом через `FILTER` и `PERCENTILE_CONT`. Индекс `(target_id, checked_at DESC)` закрывает выборку по окну. Пока данных немного, этого хватает; при росте стоит партиционировать `checks` по времени.

**Пул воркеров и ограниченная очередь.** Число одновременных запросов не растёт вместе с числом целей. При переполнении проверки дропаются, и это видно в метрике — деградация не превращается в тихое накопление горутин.

**Схема через `embed`.** Для пет-проекта достаточно идемпотентного SQL при старте. Если бы над проектом работала команда, взял бы golang-migrate или goose с версиями миграций.

**URL в лейбле метрики.** Так удобно смотреть в Prometheus, но для большого числа целей это плохо: кардинальность растёт. В проде лучше лейбл по id и отдельный маппинг.

## Ограничения

- Рассчитан на один экземпляр. Две реплики начнут проверять одни и те же цели; для нескольких нужен лидер-лок или распределённый планировщик.
- Алерты только в лог, без webhook и Alertmanager.
- Нет retention и партиционирования: таблица `checks` растёт бесконечно.
- Нет аутентификации и TLS — сервис предполагается запускать локально.

## Планы

- Дашборд Grafana
- Retention и партиционирование `checks`
- Батчевые вставки результатов
- Webhook-уведомления
- Kubernetes-манифесты

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
