#!/usr/bin/env bash
# test.sh - file-I/O (freopen) with REAL problem data in 1013 format, end to end.
#
# The data dir uses input.name/output.name (number.in / number.out) plus four
# per-case files number1..4.in/.out. The solution only reads/writes number.*,
# so only number1 matches (pass_rate 0.25, final_result WA).
#
# Fixtures (under this script's directory):
#   cat_num.cpp      freopen solution (echoes number.in -> number.out)
#   data/            real 1013-format data (input.name, output.name, number1..4)
set -u

BASE="$(cd "$(dirname "$0")" && pwd)"
REPO="${JUDGE_REPO:-/root/gojudge}"
BIN="$BASE/hustoj"
OJHOME="${OJHOME:-$REPO/extra}"
WORKBASE="$BASE/work"

PASS=0
FAIL=0

fail() { printf 'FAIL: %s\n' "$*" >&2; FAIL=$((FAIL + 1)); }
ok()   { printf 'PASS: %s\n' "$*";          PASS=$((PASS + 1)); }

cd "$BASE" || exit 1

echo "== Building $BIN from $REPO =="
( cd "$REPO" && go build -o "$BIN" . ) || { echo "build failed" >&2; exit 1; }
mkdir -p "$WORKBASE"

( cd "$BASE" && "$BIN" judge \
    --source="$BASE/cat_num.cpp" --lang 1 --data "$BASE/data" \
    --time 1000 --memory 262144 --spj 0 --ojhome "$OJHOME" --workbase "$WORKBASE" \
    >"$BASE/out.json" 2>"$BASE/judge.err" )

python3 - "$BASE/out.json" <<'PYEOF'
import json, sys
d = json.load(open(sys.argv[1]))
assert d["final_result"] == 6, f"want WA(6), got {d['final_result']}"
assert d["pass_rate"] == 0.25, f"want pass_rate 0.25, got {d['pass_rate']}"
cases = d["results"]
assert len(cases) == 4, f"want 4 cases, got {len(cases)}"
assert cases[0]["result"] == 4, f"case1 (number1.in) should be AC, got {cases[0]}"
assert all(c["result"] == 6 for c in cases[1:]), f"cases 2-4 should be WA: {cases}"
print(f"PASS: file-IO 1013 format (final_result=6, pass_rate=0.25, "
      f"number1 AC, time {d['time_ms']}ms, mem {d['memory_kb']}KB)")
PYEOF
[ $? -eq 0 ] && ok "FILE-IO 1013 data" || fail "FILE-IO 1013 data"

echo
echo "== Summary: $PASS passed, $FAIL failed =="
[ "$FAIL" -eq 0 ]