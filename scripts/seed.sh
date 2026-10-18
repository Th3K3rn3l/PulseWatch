#!/usr/bin/env bash
# Adds demo targets to a running PulseWatch instance (requires grpcurl).
#
# Usage: ./scripts/seed.sh [host:port]
set -euo pipefail

ADDR="${1:-localhost:50051}"

add() {
  grpcurl -plaintext -d "$1" "$ADDR" pulsewatch.v1.PulseWatch/AddTarget
}

add '{"url":"https://example.com","name":"example.com","interval_seconds":30}'
add '{"url":"https://go.dev","name":"go.dev","interval_seconds":60}'
add '{"url":"https://github.com/Th3K3rn3l","name":"github profile","interval_seconds":120}'
# Always-500 endpoint to demo alerts:
add '{"url":"https://httpbin.org/status/500","name":"flaky (always 500)","interval_seconds":30}'

echo
echo "Done. Try:"
echo "  grpcurl -plaintext $ADDR list pulsewatch.v1.PulseWatch"
echo "  curl -s localhost:2112/metrics | grep pulsewatch"
