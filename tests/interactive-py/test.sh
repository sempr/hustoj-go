#!/usr/bin/env bash
# test.sh - test the interactive judge (spj=3) with a PYTHON player and the
# ORIGINAL C++ interactor, end to end, without a database.
#
# Fixtures (under this script's directory):
#   interactive_spj.cpp   ORIGINAL interactor source (kept unchanged)
#   data/spj              interactor binary (g++ -static -O2 build)
#   data/1.in, 2.in       per-case secrets (42, 73); interactor reads argv[1]
#   main_ac.py            correct python player (binary search over [1,1000]) -> AC
#   main_wa.py            python player always guessing 1                   -> WA
#   main_hang.py          python busy loop                                  -> TL
#
# Requirements: go toolchain + g++ (static build for the interactor).
set -u

BASE="$(cd "$(dirname "$0")" && pwd)"
REPO="${JUDGE_REPO:-/root/gojudge}"
BIN="$BASE/hustoj"
OJHOME="${OJHOME:-$REPO/extra}"
WORKBASE="$BASE/work"
LANG_ID=6
DATA_DIR="$BASE/data"

PASS=0
FAIL=0

fail() { printf 'FAIL: %s\n' "$*" >&2; FAIL=$((FAIL + 1)); }
ok()   { printf 'PASS: %s\n' "$*";          PASS=$((PASS + 1)); }

run_judge() { # $1 = json stdout file, then the rest are judge args
    local out="$1"; shift
    ( cd "$BASE" && "$BIN" judge "$@" >"$out" 2>"$BASE/judge.err" )
}

check_json() {
    local out="$1" want_res="$2" want_rate="$3" label="$4"
    python3 - "$out" "$want_res" "$want_rate" "$label" <<'PYEOF'
import json, sys
out, want_res, want_rate, label = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
try:
    data = json.load(open(out))
except Exception as e:
    print(f"stdout is not pure JSON ({e})", file=sys.stderr); sys.exit(1)
if data["final_result"] != int(want_res):
    print(f"final_result={data['final_result']}, want {want_res}", file=sys.stderr); sys.exit(1)
if data["pass_rate"] != float(want_rate):
    print(f"pass_rate={data['pass_rate']}, want {want_rate}", file=sys.stderr); sys.exit(1)
PYEOF
    if [ $? -eq 0 ]; then ok "$label (final_result=$2)"; else fail "$label"; fi
}

cd "$BASE" || exit 1

echo "== Building $BIN from $REPO =="
( cd "$REPO" && go build -o "$BIN" . ) || { echo "build failed" >&2; exit 1; }

echo "== Building original interactor data/spj (C++, unchanged) =="
mkdir -p "$DATA_DIR"
g++ -static -O2 -o "$DATA_DIR/spj" "$BASE/interactive_spj.cpp" || { echo "interactor build failed" >&2; exit 1; }

mkdir -p "$WORKBASE"

echo
echo "== 1. Correct python player -> AC (two interactive sessions) =="
run_judge "$BASE/out1.json" \
    --source="$BASE/main_ac.py" --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 3 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out1.json" 4 1.0 "AC (original C++ interactor + python player)"
python3 - "$BASE/out1.json" <<'PYEOF'
import json, sys
d = json.load(open(sys.argv[1]))
cases = d.get("results") or []
assert len(cases) == 2, f"expected 2 interactive sessions, got {len(cases)}"
assert all(c["result"] == 4 for c in cases), f"not all sessions AC: {cases}"
print(f"PASS: AC detail (2 sessions AC, time {d['time_ms']}ms, mem {d['memory_kb']}KB)")
PYEOF
[ $? -eq 0 ] && ok "AC detail" || fail "AC detail"

echo
echo "== 2. Wrong python player -> WA =="
run_judge "$BASE/out2.json" \
    --source="$BASE/main_wa.py" --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 3 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out2.json" 6 0.0 "WA"

echo
echo "== 3. Hanging python player -> TL =="
run_judge "$BASE/out3.json" \
    --source="$BASE/main_hang.py" --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 3 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out3.json" 7 0.0 "TL"

echo
echo "== 4. stdout must be pure JSON =="
python3 - "$BASE/out1.json" <<'PYEOF'
import json, sys
json.load(open(sys.argv[1]))
print("PASS: stdout is pure JSON")
PYEOF
[ $? -eq 0 ] && ok "pure JSON stdout" || fail "pure JSON stdout"

echo
echo "== Summary: $PASS passed, $FAIL failed =="
[ "$FAIL" -eq 0 ]