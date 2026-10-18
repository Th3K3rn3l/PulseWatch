-- Схема PulseWatch. Идемпотентна: применяется при каждом старте сервиса.

CREATE TABLE IF NOT EXISTS targets (
    id               BIGSERIAL   PRIMARY KEY,
    url              TEXT        NOT NULL UNIQUE,
    name             TEXT        NOT NULL DEFAULT '',
    interval_seconds INT         NOT NULL DEFAULT 30,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS checks (
    id          BIGSERIAL   PRIMARY KEY,
    target_id   BIGINT      NOT NULL REFERENCES targets (id) ON DELETE CASCADE,
    checked_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    ok          BOOLEAN     NOT NULL,
    status_code INT         NOT NULL DEFAULT 0,
    latency_ms  INT         NOT NULL,
    error       TEXT        NOT NULL DEFAULT ''
);

-- Основной паттерн доступа: последние проверки конкретной цели
-- и агрегаты за окно времени.
CREATE INDEX IF NOT EXISTS checks_target_checked_at_idx
    ON checks (target_id, checked_at DESC);
