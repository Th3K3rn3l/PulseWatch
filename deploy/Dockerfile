# Multi-stage build: compile a static binary, ship it on a minimal image.
FROM golang:1.27-alpine AS builder

WORKDIR /src

# Cache dependencies separately from the source code.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pulsewatch ./cmd/pulsewatch

FROM alpine:3.20

RUN adduser -D -H -u 10001 app

COPY --from=builder /out/pulsewatch /usr/local/bin/pulsewatch

USER app
EXPOSE 50051 2112

ENTRYPOINT ["pulsewatch"]
