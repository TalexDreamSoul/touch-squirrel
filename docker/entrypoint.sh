#!/usr/bin/env bash
set -euo pipefail

export GROK_HOME="${GROK_HOME:-/data}"
export PANEL_ADDR="${PANEL_ADDR:-:8787}"
export GROK_PYTHON="${GROK_PYTHON:-/opt/venv/bin/python}"
export GROK_TURNSTILE_SCRIPT="${GROK_TURNSTILE_SCRIPT:-/opt/grok/turnstile_mint.py}"
export CLOAKBROWSER_SUPPRESS_FONT_WARNING="${CLOAKBROWSER_SUPPRESS_FONT_WARNING:-1}"

mkdir -p "$GROK_HOME" "$GROK_HOME/logs" "$GROK_HOME/outputs"

# Seed config on first boot (never overwrite user edits)
if [[ ! -f "$GROK_HOME/config.env" ]]; then
  if [[ -f /opt/grok/config.env.docker ]]; then
    cp /opt/grok/config.env.docker "$GROK_HOME/config.env"
  else
    cp /opt/grok/config.env.example "$GROK_HOME/config.env"
  fi
  # Runtime defaults are already in config.env.docker. The panel owns later
  # edits, so do not append compose environment overrides here.
  chmod 600 "$GROK_HOME/config.env"
  echo "[entrypoint] seeded $GROK_HOME/config.env"
fi

# Existing volumes created by earlier releases can still reference host loopback
# endpoints. Migrate only the old defaults; custom panel values remain untouched.
migrate_legacy_default() {
  local key="$1" old="$2" replacement="$3" file="$GROK_HOME/config.env" tmp
  [[ -f "$file" ]] || return 0
  grep -Fqx "$key=$old" "$file" || return 0
  tmp="$(mktemp "${file}.XXXXXX")"
  awk -v key="$key" -v old="$old" -v replacement="$replacement" '
    $0 == key "=" old { print key "=" replacement; next }
    { print }
  ' "$file" > "$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$file"
  echo "[entrypoint] migrated $key to Docker service default"
}
migrate_legacy_default "REGISTER_PROXY" "http://127.0.0.1:40080" "http://privoxy:8118"
migrate_legacy_default "FLARESOLVERR_URL" "http://127.0.0.1:8191" "http://flaresolverr:8191"
migrate_legacy_default "HTTP_PROXY" "http://127.0.0.1:40080" "http://privoxy:8118"
migrate_legacy_default "HTTPS_PROXY" "http://127.0.0.1:40080" "http://privoxy:8118"

# Configured clients read proxy settings from config.env. Keep lowercase proxy
# aliases only for third-party subprocesses that require process environment.
if [[ -f "$GROK_HOME/config.env" ]]; then
  HTTP_PROXY="$(awk -F= '$1 == "HTTP_PROXY" { print substr($0, index($0, "=") + 1); exit }' "$GROK_HOME/config.env")"
  HTTPS_PROXY="$(awk -F= '$1 == "HTTPS_PROXY" { print substr($0, index($0, "=") + 1); exit }' "$GROK_HOME/config.env")"
  NO_PROXY="$(awk -F= '$1 == "NO_PROXY" { print substr($0, index($0, "=") + 1); exit }' "$GROK_HOME/config.env")"
fi
if [[ -n "${HTTP_PROXY:-}" ]]; then export http_proxy="$HTTP_PROXY"; fi
if [[ -n "${HTTPS_PROXY:-}" ]]; then export https_proxy="$HTTPS_PROXY"; fi
if [[ -n "${NO_PROXY:-}" ]]; then export no_proxy="$NO_PROXY"; fi

exec /usr/local/bin/squirrel "$@"
