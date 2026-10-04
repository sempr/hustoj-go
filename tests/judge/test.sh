#!/usr/bin/env bash
# test.sh - end-to-end tests for `hustoj judge` (standalone, no database).
#
# Fixtures (all under this script's directory):
#   Main.cpp      accepted C++ solution (lang 1)
#   wrong.cpp     wrong solution -> WA
#   bad.cpp       syntax error    -> CE
#   data/         3 test cases (1.in/1.out, 2.in/2.out, 3.in/3.out)
#   rt/           raw-text judge fixtures (--spj 2)
#
# Requirements: go toolchain (to build hustoj from the repo).
set -u

BASE="$(cd "$(dirname "$0")" && pwd)"
REPO="${JUDGE_REPO:-/root/gojudge}"
ORIG_BIN="${ORIG_BIN:-hustoj}"
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
# $1 = argument string for `judge` (already quoted into an array via eval is unsafe;
#      instead callers pass the args file). We use a simple model: run one invocation
#      per call with source/target given via env.
run_judge() { # $1 = json stdout file, then the rest are judge args
    local out="$1"; shift
    ( cd "$BASE" && "$BIN" judge "$@" >"$out" 2>"$BASE/judge.err" )
}

# Assert stdout is pure JSON and matches expectations.
# $1 = stdout file, $2 = expected final_result, $3 = expected pass_rate, $4 = label
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
    if [ $? -eq 0 ]; then ok "$label (final_result=$2, pass_rate=$3)"; else fail "$label"; fi
}

cd "$BASE" || exit 1
echo "== Building $BIN from $REPO =="
( cd "$REPO" && go build -o "$BIN" . ) || { echo "build failed" >&2; exit 1; }
mkdir -p "$WORKBASE"

echo
echo "== 1. Accepted solution, source from file =="
run_judge "$BASE/out1.json" \
    --source="$BASE/Main.cpp" --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 0 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out1.json" 4 1.0 "AC (file source)"
python3 - "$BASE/out1.json" <<'PYEOF'
import json, sys
d = json.load(open(sys.argv[1]))
cases = d["results"]
assert len(cases) == 3, f"expected 3 results, got {len(cases)}"
assert all(c["result"] == 4 for c in cases), f"not all cases AC: {cases}"
print(f"PASS: AC detail (3 cases, score {d.get('score')}/{d.get('total_score')}, "
      f"time {d['time_ms']}ms, mem {d['memory_kb']}KB)")
PYEOF
[ $? -eq 0 ] && ok "AC detail" || fail "AC detail"

echo
echo "== 2. Accepted solution, source piped via stdin =="
cat "$BASE/Main.cpp" | run_judge "$BASE/out2.json" \
    --source=- --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 0 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out2.json" 4 1.0 "AC (stdin source)"

echo
echo "== 3. Wrong solution -> WA =="
run_judge "$BASE/out3.json" \
    --source="$BASE/wrong.cpp" --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 0 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out3.json" 6 0.0 "WA"

echo
echo "== 4. Broken source -> CE =="
run_judge "$BASE/out4.json" \
    --source="$BASE/bad.cpp" --lang "$LANG_ID" --data "$DATA_DIR" \
    --time 1000 --memory 262144 --spj 0 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out4.json" 11 0.0 "CE"
[ -n "$(python3 -c "import json,sys;print(json.load(open('$BASE/out4.json')).get('compile_error',''))" 2>/dev/null)" ] \
    && ok "CE compile_error field present" || fail "CE compile_error field present"

echo
echo "== 5. Raw-text judge (--spj 2), data.out = qid [score] answer =="
printf '1 [2] red\n2 [3] blue\n' > "$BASE/rt/data.out"
printf 'unused\n' > "$BASE/rt/data.in"
printf '1 red\n2 blue\n' > "$BASE/rt/answer.txt"
run_judge "$BASE/out5.json" \
    --source="$BASE/rt/answer.txt" --lang "$LANG_ID" --data "$BASE/rt" \
    --time 1000 --memory 262144 --spj 2 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out5.json" 4 1.0 "RAWTEXT AC"

echo
echo "== 6. File-I/O (freopen) via input.name/output.name =="
run_judge "$BASE/out6.json" \
    --source="$BASE/fio/sum_freopen.cpp" --lang "$LANG_ID" --data "$BASE/fio/data" \
    --time 1000 --memory 262144 --spj 0 --ojhome "$OJHOME" --workbase "$WORKBASE"
check_json "$BASE/out6.json" 4 1.0 "FILE-IO AC"

echo
echo "== 7. stdout must be pure JSON (no log lines leaked) =="
python3 - "$BASE/out1.json" <<'PYEOF'
import json, sys
json.load(open(sys.argv[1]))
print("PASS: stdout is pure JSON (checked again on out1.json)")
PYEOF
[ $? -eq 0 ] && ok "pure JSON stdout" || fail "pure JSON stdout"

echo
echo "== Summary: $PASS passed, $FAIL failed =="
[ "$FAIL" -eq 0 ]