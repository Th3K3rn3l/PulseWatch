GO      ?= go
DSN     ?= postgres://pulsewatch:pulsewatch@localhost:5433/pulsewatch?sslmode=disable
PROTO    := api/proto/pulsewatch/v1/pulsewatch.proto
GEN_DIR  := gen

.PHONY: proto build run test test-integration vet fmt lint coverage up down logs seed clean

## Generate gRPC code from proto files (protoc + protoc-gen-go plugins required).
proto:
	protoc \
		-I api/proto \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		$(PROTO)

build:
	$(GO) build -o bin/pulsewatch ./cmd/pulsewatch

run:
	$(GO) run ./cmd/pulsewatch

test:
	$(GO) test ./...

## Integration tests need a running PostgreSQL (see `make up`).
test-integration:
	PULSEWATCH_TEST_DSN="$(DSN)" $(GO) test -tags=integration -v ./internal/storage

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

lint:
	golangci-lint run

coverage:
	$(GO) test -cover ./...

## Bring up postgres + pulsewatch + prometheus.
up:
	cd deploy && docker compose up -d --build

down:
	cd deploy && docker compose down -v

logs:
	cd deploy && docker compose logs -f pulsewatch

## Add demo targets over gRPC (requires grpcurl).
seed:
	./scripts/seed.sh

clean:
	rm -rf bin
