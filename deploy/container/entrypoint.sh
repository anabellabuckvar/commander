#!/bin/sh
# Start agent-mail (its token is generated here, never baked into the image),
# then exec cmdrd. Everything lives in the home volume.
set -eu
mkdir -p "$HOME/.config/mcp-agent-mail"
env_file="$HOME/.config/mcp-agent-mail/config.env"
if ! grep -q '^HTTP_BEARER_TOKEN=.' "$env_file" 2>/dev/null; then
  umask 077
  printf 'HTTP_BEARER_TOKEN=%s\n' "$(openssl rand -hex 32)" > "$env_file"
fi
set -a; . "$env_file"; set +a
am serve-http --host 127.0.0.1 --port 8765 --no-tui >"$HOME/am.log" 2>&1 &
i=0
until curl -fsS http://127.0.0.1:8765/health/liveness >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -gt 30 ]; then echo "agent-mail did not start:" >&2; cat "$HOME/am.log" >&2; exit 1; fi
  sleep 1
done
exec cmdrd "$@"
