#!/usr/bin/env bash
# OlivePress end-to-end smoke test. Builds the binary, starts the service on a
# loopback port, drives a full fresh-fruit intake task through every stage to a
# cold-press conclusion, then cleans up the process and all temp files. Runs
# without external network access and never shells out to `go test`.
set -euo pipefail

TMP_DIR="$(mktemp -d ./olivepress-smoke.XXXXXX)"
PORT="18080"
BASE="http://127.0.0.1:${PORT}"
BIN="$TMP_DIR/olivepress"
DB="$TMP_DIR/olivepress.db"
PID=""

cleanup() {
    if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
        kill "$PID" 2>/dev/null || true
        wait "$PID" 2>/dev/null || true
    fi
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

echo "==> building binary"
go build -o "$BIN" ./cmd/olivepress

echo "==> starting service on ${BASE}"
"$BIN" -addr "127.0.0.1:${PORT}" -db "$DB" >"$TMP_DIR/server.log" 2>&1 &
PID=$!

# Wait for the health endpoint to come up.
ready=""
for _ in $(seq 1 50); do
    if resp="$(curl -fsS "${BASE}/healthz" 2>/dev/null)"; then
        ready="$resp"
        break
    fi
    sleep 0.1
done
if [[ -z "$ready" ]]; then
    echo "service failed to start" >&2
    cat "$TMP_DIR/server.log" >&2
    exit 1
fi
if ! grep -q '"status":"ok"' <<<"$ready"; then
    echo "unexpected health body: $ready" >&2
    exit 1
fi
echo "    health: ok"

echo "==> reading seeded catalogue plot"
plot="$(curl -fsS "${BASE}/v1/catalog/plots/plot-picual-1")"
rule_digest="$(grep -o '"rule_digest":"[^"]*"' <<<"$plot" | head -1 | cut -d'"' -f4)"
if [[ -z "$rule_digest" ]]; then
    echo "failed to read rule digest" >&2
    exit 1
fi
echo "    rule_digest=${rule_digest}"

post() {
    local path="$1" body="$2"
    curl -fsS -X POST -H 'Content-Type: application/json' -d "$body" "${BASE}${path}"
}

field() {
    local json="$1" name="$2"
    grep -o "\"${name}\":\"[^\"]*\"" <<<"$json" | head -1 | cut -d'"' -f4
}

echo "==> locking task"
lock_body="$(printf '{"operation_no":"smoke-lock","plot_id":"plot-picual-1","cultivar_id":"picual","harvest_at":"2026-10-01T00:00:00Z","intake_batch":"smoke-batch","crate_seals":["smoke-s1","smoke-s2"],"blind_codes":["smoke-b1","smoke-b2"],"thresholds":{"acid":{"scale":2,"min":0,"max":80},"peroxide":{"scale":2,"min":0,"max":2000},"polyphenol":{"scale":0,"min":150,"max":0},"moisture":{"scale":1,"min":0,"max":550},"fruit_temp":{"scale":1,"min":0,"max":350}},"reviewer_ids":["rev-a","rev-b"],"rule_digest":"%s"}' "$rule_digest")"
lock="$(post "/v1/tasks/lock" "$lock_body")"
task_id="$(field "$lock" task_id)"
if [[ -z "$task_id" ]]; then
    echo "lock failed: $lock" >&2
    exit 1
fi
echo "    task_id=${task_id}"

echo "==> advancing through the intake pipeline"
post "/v1/tasks/${task_id}/sample-confirm" '{"operation_no":"smoke-sc","reviewer_a":"rev-a","reviewer_b":"rev-b"}' >/dev/null
post "/v1/tasks/${task_id}/split-samples" '{"operation_no":"smoke-split"}' >/dev/null
post "/v1/tasks/${task_id}/start-resources" '{"operation_no":"smoke-res"}' >/dev/null

post "/v1/tasks/${task_id}/maturity-counts" '{
  "operation_no":"smoke-maturity",
  "total":{"smoke-s1":100,"smoke-s2":100},
  "cells":[
    {"crate_seal":"smoke-s1","color_grade":"green","count":60},
    {"crate_seal":"smoke-s1","color_grade":"turning","count":20},
    {"crate_seal":"smoke-s1","color_grade":"purple-black","count":15},
    {"crate_seal":"smoke-s1","color_grade":"damaged","count":4},
    {"crate_seal":"smoke-s1","color_grade":"moldy","count":1},
    {"crate_seal":"smoke-s2","color_grade":"green","count":65},
    {"crate_seal":"smoke-s2","color_grade":"turning","count":18},
    {"crate_seal":"smoke-s2","color_grade":"purple-black","count":12},
    {"crate_seal":"smoke-s2","color_grade":"damaged","count":3},
    {"crate_seal":"smoke-s2","color_grade":"moldy","count":2}
  ]
}' >/dev/null

post "/v1/tasks/${task_id}/readings" '{"operation_no":"smoke-read","acid":"0.42","peroxide":"12.50","polyphenol":"220","moisture":"32.5","fruit_temp":"21.0"}' >/dev/null
post "/v1/tasks/${task_id}/foreign-matter" '{"operation_no":"smoke-fm","finding":"clear"}' >/dev/null
post "/v1/tasks/${task_id}/reviews" '{"operation_no":"smoke-r1","reviewer_id":"rev-a","role":"primary","decision":"approve"}' >/dev/null
post "/v1/tasks/${task_id}/reviews" '{"operation_no":"smoke-r2","reviewer_id":"rev-b","role":"secondary","decision":"approve"}' >/dev/null

echo "==> finalizing cold-press"
final="$(post "/v1/tasks/${task_id}/finalize" '{"operation_no":"smoke-fin","kind":"cold-press"}')"
final_kind="$(field "$final" final_kind)"
credential="$(field "$final" final_credential)"
if [[ "$final_kind" != "cold-press" || -z "$credential" ]]; then
    echo "finalize failed: $final" >&2
    exit 1
fi
echo "    final=${final_kind} credential=${credential:0:16}..."

echo "==> verifying recovered terminal state"
view="$(curl -fsS "${BASE}/v1/tasks/${task_id}")"
state="$(field "$view" state)"
if [[ "$state" != "cold-pressed" ]]; then
    echo "recovered state = $state, want cold-pressed" >&2
    exit 1
fi

echo "==> smoke test passed"
