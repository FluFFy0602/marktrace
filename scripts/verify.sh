#!/usr/bin/env bash
# Golden verification on a FRESH database (make verify recreates the stack first):
# import T1 -> golden compare 100% -> KPI -> re-import idempotency -> auditor 403 -> web via nginx.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

wait_ready

say "Вход qc (A3)"
login qc
ok "qc вошёл"

existing=$(req qc GET "$API/imports")
if [[ "$(jget "$existing" 'length')" != 0 ]]; then
  info "ВНИМАНИЕ: в БД уже есть сеансы импорта; эталон рассчитан на чистую БД (make verify делает reset)"
fi

say "Импорт T1 (items + codes + operations)"
s1=$(import_t1 qc)
expect "HTTP" "$(status)" 200
expect "status" "$(jget "$s1" '.status')" completed
for e in items codes operations; do
  info "$e: $(jget "$s1" ".counters.$e")"
done
info "rows_created=$(jget "$s1" '.rows_created') snapshot_violations=$(jget "$s1" '.snapshot_violations')"

say "Сверка с эталоном (golden)"
g=$(req qc GET "$API/golden/compare")
expect "status_summary.match" "$(jget "$g" '.status_summary.match')" true
expect "violations.match" "$(jget "$g" '.violations.match')" true
expect "percent" "$(jget "$g" '.percent')" 100
info "status_summary: $(jget "$g" '.status_summary.actual_rows') строк, violations: $(jget "$g" '.violations.actual_rows') строк"

say "KPI"
k=$(req qc GET "$API/kpi")
expect_ge "K1 %" "$(jget "$k" '.k1.percent')" 99
info "K1 accepted=$(jget "$k" '.k1.accepted') rejected=$(jget "$k" '.k1.rejected')"
expect "K2 %" "$(jget "$k" '.k2.percent')" 100
expect "K5 %" "$(jget "$k" '.k5.percent')" 100

v_before=$(jget "$(req qc GET "$API/violations?limit=1")" '.total')

say "Повторный импорт тех же файлов (K3)"
s2=$(import_t1 qc)
expect "status" "$(jget "$s2" '.status')" completed
expect "rows_created" "$(jget "$s2" '.rows_created')" 0
for e in items codes operations; do
  expect "$e.accepted" "$(jget "$s2" ".counters.$e.accepted")" 0
  expect "$e.rejected" "$(jget "$s2" ".counters.$e.rejected")" 0
done
v_after=$(jget "$(req qc GET "$API/violations?limit=1")" '.total')
expect "нарушений после повтора" "$v_after" "$v_before"
k=$(req qc GET "$API/kpi")
expect "K3 rows_created" "$(jget "$k" '.k3.rows_created')" 0
g=$(req qc GET "$API/golden/compare")
expect "golden после повтора" "$(jget "$g" '.percent')" 100

say "Аудитор (A4): все записи запрещены"
login auditor
post_json auditor /operations '{"op_type":"issue","code":"010460000000000021AAAAAA","item_code":"X"}' >/dev/null
expect "POST /operations" "$(status)" 403
body=$(req auditor POST "$API/imports" -F "items=@$T1/items.csv")
expect "POST /imports" "$(status)" 403
expect "error" "$(jget "$body" '.error')" AUTH_DENIED
vid=$(jget "$(req auditor GET "$API/violations?limit=1")" '.rows[0].violation_id')
post_json auditor "/violations/$vid/resolutions" '{"action":"comment","comment":"x"}' >/dev/null
expect "POST /violations/{id}/resolutions" "$(status)" 403
req auditor GET "$API/codes?limit=1" >/dev/null
expect "GET /codes (чтение)" "$(status)" 200

say "Веб через nginx ($WEB)"
html=$(req web GET "$WEB/")
expect "GET /" "$(status)" 200
if grep -q 'id="root"' <<<"$html"; then ok "SPA index.html отдан"; else bad "в / нет <div id=\"root\">"; fi
html=$(req web GET "$WEB/codes")
expect "GET /codes (deep link)" "$(status)" 200
if grep -q 'id="root"' <<<"$html"; then ok "deep link отдаёт index.html"; else bad "deep link не отдаёт index.html"; fi
req web GET "$WEB/api/auth/me" >/dev/null
expect "GET /api/auth/me без cookie" "$(status)" 401
req web POST "$WEB/api/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"qc","password":"qc"}' >/dev/null
expect "POST /api/auth/login через nginx" "$(status)" 200
me=$(req web GET "$WEB/api/auth/me")
expect "GET /api/auth/me с cookie" "$(jget "$me" '.role')" A3

finish
