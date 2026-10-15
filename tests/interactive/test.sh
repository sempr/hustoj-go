#!/usr/bin/env bash
# test.sh - end-to-end tests for `hustoj judge --spj 3` (interactive, no DB).
#
# Fixtures (all under this script's directory):
#   interactive_spj.cpp   sample interactor source (secret read from data<N>.in)
#   data/spj              interactor binary (g++ -static -O2)
#   data/1.in, 2.in       scenario inputs (contain the secret)
#   ac_guess.cpp          correct player  -> AC
#   wa_guess.cpp          wrong player    -> WA
#   hang.cpp              infinite loop   -> TL
#
# Requirements: go toolchain + g++ (static build for the interactor).
set -u

BASE="$(cd "$(dirname "$0")" && pwd)"
REPO="${JUDGE_REPO:-/root/gojudge}"
BIN="$BASE/hustoj"
OJHOME="${OJHOME:-$REPO/extra}"
WORKBASE="$BASE/work"
LANG_ID=1
DATA_DIR="$BASE/data"

PASS=0
FAIL=0

fail() { printf 'FAIL: %s\n' "$*" >&2; FAIL=$((FAIL + 1)); }
ok()   { printf 'PASS: %s\n' "$*";          PASS=$((PASS + 1)); }

# Run the judge, capturing stdout only (logs must go to stderr).
run_judge() { # $1 = json stdout file, then the rest are judge args
    local out="$1"; shift
    ( cd "$BASE" && "$BIN" judge "$@" >"$out" 2>"$BASE/judge.err" )
}

# Assert stdout is pure JSON and matches final_result / pass_rate.
check_json() {
    local out="$1" want_res="$2" want_rate="$3" label="$4"
    python3 - "$out" "$want_res" "$want_rate" "$label" <<'PYEOF'
import json, sys
out, want_res, want_rate, label = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
try:
    data = json.load(open(out))
except Exception as e:
    print(f"stdout is not pure JSON ({e})", file=sys.stderr); sys.exit(1)
res, rate = data["final_result"], data["pass_rate"]
if res != int(want_res):
    print(f"final_result={res}, want {want_res}", file=sys.stderr); sys.exit(1)
if rate != float(want_rate):
    print(f"pass_rate={rate}, want {want_rate}", file=sys.stderr); sys.exit(1)
PYEOF
    if [ $? -eq 0 ]; then ok "$label (final_result=$2)"; else fail "$label"; fi
}

cd "$BASE" || exit 1

echo "== Building $BIN from $REPO =="
( cd "$REPO" && go build -o "$BIN" . ) || { echo "build failed" >&2; exit 1; }

echo "== Building interactor data/spj =="
mkdir -p "$DATA_DIR"
g++ -static -O2 -o "$DATA_DIR/spj" "$BASE/interactive_spj.cpp" || { echo "interactor build failed" >&2; exit 1; }
printf '731\n' > "$DATA_DIR/1.in"
printf '42\n' > "$DATA_DIR/2.in"
mkdir -p "$WORKBASE"

echo
echo "== 1. Correct player -> AC (two interactive sessions) =="
run_judge "$BASE/out1.json" \
    --source="$BASE/ac_guess.cpp" --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 3 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out1.json" 4 1.0 "AC"
python3 - "$BASE/out1.json" <<'PYEOF'
import json, sys
d = json.load(open(sys.argv[1]))
cases = d.get("results") or []
assert d["final_result"] == 4, f"want AC, got {d['final_result']}"
assert len(cases) == 2, f"expected 2 interactive sessions, got {len(cases)}"
assert all(c["result"] == 4 for c in cases), f"not all sessions AC: {cases}"
print(f"PASS: detail (2 sessions, time {d['time_ms']}ms, mem {d['memory_kb']}KB)")
PYEOF
[ $? -eq 0 ] && ok "AC detail" || fail "AC detail"

echo
echo "== 2. Wrong player -> WA =="
run_judge "$BASE/out2.json" \
    --source="$BASE/wa_guess.cpp" --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 3 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out2.json" 6 0.0 "WA"

echo
echo "== 3. Hanging player -> TL =="
run_judge "$BASE/out3.json" \
    --source="$BASE/hang.cpp" --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 3 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out3.json" 7 0.0 "TL"

echo
echo "== 4. stdout must be pure JSON (no log lines leaked) =="
python3 - "$BASE/out1.json" <<'PYEOF'
import json, sys
json.load(open(sys.argv[1]))
print("PASS: stdout is pure JSON on interactive judge")
PYEOF
[ $? -eq 0 ] && ok "pure JSON stdout" || fail "pure JSON stdout"

echo
echo "== Summary: $PASS passed, $FAIL failed =="
[ "$FAIL" -eq 0 ]