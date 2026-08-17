#!/usr/bin/env bash
# Run the Next.js panel with API proxying and restart the Go host after Go changes.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

FRONTEND_PORT="${FRONTEND_PORT:-3007}"
BACKEND_ADDR="${BACKEND_ADDR:-127.0.0.1:8787}"
PANEL_TOKEN="${PANEL_TOKEN:-}"
GROK_HOME="${GROK_HOME:-${SQUIRREL_HOME:-$ROOT/.grok-home}}"
DEV_TMP="${DEV_TMP:-$ROOT/.tmp/dev}"
BACKEND_BIN="$DEV_TMP/squirrel"
BACKEND_PID_FILE="$DEV_TMP/backend.pid"

if [[ -f "$ROOT/.env" ]]; then
  set -a
  # shellcheck source=/dev/null
  source "$ROOT/.env"
  set +a
fi
PANEL_TOKEN="${PANEL_TOKEN:-local-dev-token}"

resolve_go() {
  local candidate
  for candidate in \
    "$(command -v go 2>/dev/null || true)" \
    "$HOME/.local/share/mise/installs/go/latest/bin/go" \
    "$HOME/.local/share/mise/installs/go/1.26.5/bin/go" \
    /usr/local/go/bin/go \
    "$HOME/go/bin/go"; do
    if [[ -n "$candidate" && -x "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  return 1
}

listener_pid() {
  lsof -tiTCP:"$1" -sTCP:LISTEN 2>/dev/null | head -n 1 || true
}

port_from_addr() {
  printf '%s\n' "${1##*:}"
}

snapshot_go_sources() {
  find cmd internal web plugins -type f -name '*.go' -print0 \
    | xargs -0 stat -f '%m %N' \
    | LC_ALL=C sort \
    | shasum \
    | awk '{print $1}'
}

GO_BIN="$(resolve_go || true)"
if [[ -z "$GO_BIN" ]]; then
  echo "错误: 未找到 Go。请执行 mise install go@1.26.5 后重试。" >&2
  exit 1
fi

backend_port="$(port_from_addr "$BACKEND_ADDR")"
if pid="$(listener_pid "$FRONTEND_PORT")"; [[ -n "$pid" ]]; then
  echo "错误: 前端端口 $FRONTEND_PORT 已被 PID $pid 占用。" >&2
  exit 1
fi
if pid="$(listener_pid "$backend_port")"; [[ -n "$pid" ]]; then
  echo "错误: 后端端口 $backend_port 已被 PID $pid 占用。" >&2
  exit 1
fi

mkdir -p "$DEV_TMP" "$GROK_HOME"
BACKEND_PID=""
WATCHER_PID=""
FRONTEND_PID=""

stop_backend() {
  local pid="$BACKEND_PID"
  if [[ -f "$BACKEND_PID_FILE" ]]; then
    pid="$(cat "$BACKEND_PID_FILE")"
  fi
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -f "$BACKEND_PID_FILE"
  BACKEND_PID=""
}

cleanup() {
  trap - EXIT INT TERM
  if [[ -n "$FRONTEND_PID" ]] && kill -0 "$FRONTEND_PID" 2>/dev/null; then
    kill "$FRONTEND_PID" 2>/dev/null || true
  fi
  if [[ -n "$WATCHER_PID" ]] && kill -0 "$WATCHER_PID" 2>/dev/null; then
    kill "$WATCHER_PID" 2>/dev/null || true
  fi
  stop_backend
}
trap cleanup EXIT INT TERM

build_backend() {
  echo "[*] building Go host..."
  "$GO_BIN" build -o "$BACKEND_BIN.next" ./cmd/grok
  mv "$BACKEND_BIN.next" "$BACKEND_BIN"
}

start_backend() {
  build_backend
  echo "[*] backend: http://$BACKEND_ADDR"
  GROK_HOME="$GROK_HOME" PANEL_ADDR="$BACKEND_ADDR" PANEL_TOKEN="$PANEL_TOKEN" \
    "$BACKEND_BIN" panel --addr "$BACKEND_ADDR" --token "$PANEL_TOKEN" &
  BACKEND_PID=$!
  printf '%s\n' "$BACKEND_PID" > "$BACKEND_PID_FILE"
}

watch_backend() {
  local before after
  before="$(snapshot_go_sources)"
  while true; do
    sleep 1
    after="$(snapshot_go_sources)"
    if [[ "$before" == "$after" ]]; then
      continue
    fi
    before="$after"
    echo "[*] Go source changed; rebuilding backend..."
    if build_backend; then
      stop_backend
      GROK_HOME="$GROK_HOME" PANEL_ADDR="$BACKEND_ADDR" PANEL_TOKEN="$PANEL_TOKEN" \
        "$BACKEND_BIN" panel --addr "$BACKEND_ADDR" --token "$PANEL_TOKEN" &
      BACKEND_PID=$!
      printf '%s\n' "$BACKEND_PID" > "$BACKEND_PID_FILE"
    else
      echo "[!] backend build failed; the previous backend remains running" >&2
    fi
  done
}

start_backend
watch_backend &
WATCHER_PID=$!

echo "[*] frontend: http://127.0.0.1:$FRONTEND_PORT"
echo "[*] API proxy: http://$BACKEND_ADDR"
echo "[*] data home: $GROK_HOME"
cd "$ROOT/panel"
API_PROXY_TARGET="http://$BACKEND_ADDR" npm run dev -- --port "$FRONTEND_PORT" &
FRONTEND_PID=$!
wait "$FRONTEND_PID"
