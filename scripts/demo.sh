#!/usr/bin/env bash
# D16 live demo through the API (the same steps can be clicked in the UI, see README).
# Uses two demo codes from data/C16/demo_codes.csv that the system has not seen yet,
# so the script can be re-run on the same database.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

wait_ready
login prod
login retail
login qc

mock_mode() {
  curl -sS --noproxy '*' -o /dev/null -X POST "$MOCK/admin/mode" \
    -H 'Content-Type: application/json' -d "{\"mode\":\"$1\"}"
}
trap 'mock_mode normal || true; rm -rf "$WORK"' EXIT

# Operation times: the server default (now, seconds precision) would put several scripted
# operations on one code into the same second -> IDEMPOTENCY_CONFLICT by (code, ts).
# So the script sends explicit times one minute apart, starting an hour ago.
TS_BASE=$(($(date +%s) - 3600))
TS_FILE="$WORK/ts_n"
echo 0 >"$TS_FILE"
next_ts() {
  local n t
  n=$(($(cat "$TS_FILE") + 1))
  echo "$n" >"$TS_FILE"
  t=$((TS_BASE + n * 60))
  date -u -r "$t" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$t" +%Y-%m-%dT%H:%M:%SZ
}

# op <user> <op_type> <code> [item_code] -> prints the response JSON
op() {
  local user="$1" type="$2" code="$3" item="${4:-}"
  post_json "$user" /operations \
    "{\"op_type\":\"$type\",\"code\":\"$code\",\"item_code\":\"$item\",\"ts\":\"$(next_ts)\"}"
}

expect_op() { # expect_op <label> <response> <decision> [reason]
  local d r
  d=$(jget "$2" '.decision')
  r=$(jget "$2" '.reason')
  if [[ "$d" == "$3" && "${4:-}" == "$r" ]]; then
    ok "$1 -> $d${r:+ ($r)}; статус: $(jget "$2" '.code.status')"
  else
    bad "$1 -> decision=$d reason=$r, ожидалось $3 ${4:-}; ответ: $2"
  fi
}

say "Выбор двух неиспользованных демо-кодов"
picked=()
while IFS=, read -r code item; do
  [[ "$code" == code ]] && continue
  req qc GET "$API/codes/$code" >/dev/null
  if [[ "$(status)" == 404 ]]; then picked+=("$code,$item"); fi
  [[ ${#picked[@]} -ge 2 ]] && break
done <"$ROOT/data/C16/demo_codes.csv"
if [[ ${#picked[@]} -lt 2 ]]; then
  echo "свободные демо-коды закончились — выполните make reset && make up" >&2
  exit 1
fi
C1=${picked[0]%%,*}; I1=${picked[0]#*,}
C2=${picked[1]%%,*}; I2=${picked[1]#*,}
info "код 1: $C1 (товар $I1)"
info "код 2: $C2 (товар $I2)"

say "Производство (prod, A1): эмиссия и ввод в оборот"
expect_op "issue $C1" "$(op prod issue "$C1" "$I1")" accepted
expect_op "issue $C2" "$(op prod issue "$C2" "$I2")" accepted
expect_op "introduce $C1" "$(op prod introduce "$C1")" accepted
expect_op "introduce $C2" "$(op prod introduce "$C2")" accepted

say "Розница (retail, A2): продажа и возврат"
expect_op "sell $C1" "$(op retail sell "$C1")" accepted
expect_op "sell $C2" "$(op retail sell "$C2")" accepted
expect_op "return $C2" "$(op retail return "$C2")" accepted

say "Нарушения на форме"
expect_op "повторная продажа $C1 (sold -> sell)" "$(op retail sell "$C1")" rejected TRANSITION_NOT_ALLOWED
op prod sell "$C1" >/dev/null
expect "prod пытается продать (роль A1)" "$(status)" 403

say "История кода $C2"
h=$(req qc GET "$API/codes/$C2")
expect "HTTP" "$(status)" 200
expect "текущий статус" "$(jget "$h" '.current.status')" returned
expect "операций в истории" "$(jget "$h" '.operations | length')" 4
info "цепочка: $(jget "$h" '[.operations[].op_type] | join(" -> ")')"

say "ЦРПТ недоступен: mock в режиме error"
mock_mode error
r=$(op retail sell "$C2")
expect_op "sell $C2 при mock=error" "$r" rejected MOCK_UNAVAILABLE
info "сообщение: $(jget "$r" '.message')"

say "ЦРПТ отвечает мусором: mock в режиме garbage"
mock_mode garbage
expect_op "sell $C2 при mock=garbage" "$(op retail sell "$C2")" rejected MOCK_UNAVAILABLE

say "Mock снова normal — та же операция проходит"
mock_mode normal
expect_op "sell $C2 при mock=normal" "$(op retail sell "$C2")" accepted

say "Проверка кода в ЦРПТ (/api/verify)"
v=$(post_json qc /verify "{\"code\":\"$C1\"}")
expect "verify $C1" "$(jget "$v" '.is_valid')" true
v=$(post_json qc /verify "{\"code\":\"$C1\",\"gtin\":\"4601234567893\"}")
expect "verify $C1 с чужим GTIN: is_valid" "$(jget "$v" '.is_valid')" false
expect "verify $C1 с чужим GTIN: reason" "$(jget "$v" '.reason')" GTIN_MISMATCH
v=$(post_json qc /verify '{"code":"01046000000000002ABCDEF"}')
expect "verify кривого кода: reason" "$(jget "$v" '.reason')" INVALID_FORMAT

say "KPI после демо"
k=$(req qc GET "$API/kpi")
info "K1=$(jget "$k" '.k1.percent')% K2=$(jget "$k" '.k2.percent')% K3=$(jget "$k" '.k3.rows_created') K4=$(jget "$k" '.k4.last_ms')ms K5=$(jget "$k" '.k5.percent')%"
expect "K2 %" "$(jget "$k" '.k2.percent')" 100

finish
