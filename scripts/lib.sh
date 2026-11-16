# Shared helpers for MarkTrace scripts. Source it: . "$(dirname "$0")/lib.sh"
# shellcheck shell=bash

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API="${API:-http://localhost:8081/api}"
WEB="${WEB:-http://localhost:8080}"
MOCK="${MOCK:-http://localhost:8082}"
T1="${T1:-$ROOT/data/C16/T1}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

FAILS=0

say()  { printf '\n\033[1;34m== %s\033[0m\n' "$*"; }
ok()   { printf '  \033[32mOK\033[0m   %s\n' "$*"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$*"; FAILS=$((FAILS + 1)); }
info() { printf '       %s\n' "$*"; }

# JSON field getter: jget '<json>' '.a.b[0].c'. Uses jq, falls back to python3.
if command -v jq >/dev/null 2>&1; then
  jget() { printf '%s' "$1" | jq -rc "$2 | if . == null then empty else . end"; }
else
  jget() {
    python3 -I -c '
import json, re, sys
v = json.loads(sys.argv[1])
for part in re.findall(r"[^.\[\]]+", sys.argv[2]):
    if v is None:
        break
    v = v[int(part)] if isinstance(v, list) else v.get(part)
if v is None:
    print("")
elif isinstance(v, bool):
    print("true" if v else "false")
elif isinstance(v, (dict, list)):
    print(json.dumps(v, ensure_ascii=False))
else:
    print(v)
' "$1" "$2"
  }
fi

# curl without proxies; prints body, stores HTTP status in $WORK/status.
# Usage: req <cookie-jar-name> <METHOD> <url> [curl args...]
req() {
  local jar="$WORK/$1.jar" method="$2" url="$3"
  shift 3
  curl -sS --noproxy '*' -b "$jar" -c "$jar" -X "$method" -o "$WORK/body" \
    -w '%{http_code}' "$url" "$@" >"$WORK/status" || echo 000 >"$WORK/status"
  cat "$WORK/body"
}
status() { cat "$WORK/status"; }

login() { # login <user> (password = login)
  local body
  body=$(req "$1" POST "$API/auth/login" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"$1\"}")
  if [[ "$(status)" != 200 ]]; then
    echo "login as $1 failed: $(status) $body" >&2
    exit 1
  fi
}

post_json() { # post_json <user> <path> <json>
  req "$1" POST "$API$2" -H 'Content-Type: application/json' -d "$3"
}

import_t1() { # import_t1 <user>
  req "$1" POST "$API/imports" \
    -F "items=@$T1/items.csv;type=text/csv" \
    -F "codes=@$T1/codes.csv;type=text/csv" \
    -F "operations=@$T1/operations.csv;type=text/csv"
}

wait_ready() {
  local i
  for i in $(seq 1 90); do
    if curl -fsS --noproxy '*' -o /dev/null "$API/healthz" 2>/dev/null &&
       curl -fsS --noproxy '*' -o /dev/null "$MOCK/healthz" 2>/dev/null &&
       curl -fsS --noproxy '*' -o /dev/null "$WEB/" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  echo "stack did not become ready (backend $API, mock $MOCK, web $WEB)" >&2
  exit 1
}

# expect <name> <actual> <expected>
expect() {
  if [[ "$2" == "$3" ]]; then ok "$1 = $2"; else bad "$1 = '$2', ожидалось '$3'"; fi
}

# expect_ge <name> <actual> <min> (decimal compare)
expect_ge() {
  if awk -v a="$2" -v b="$3" 'BEGIN { exit !(a != "" && a + 0 >= b + 0) }'; then
    ok "$1 = $2 (>= $3)"
  else
    bad "$1 = '$2', ожидалось >= $3"
  fi
}

finish() {
  if [[ $FAILS -eq 0 ]]; then
    printf '\n\033[1;32mВСЁ ПРОШЛО\033[0m\n'
  else
    printf '\n\033[1;31mПРОВАЛОВ: %d\033[0m\n' "$FAILS"
    exit 1
  fi
}
