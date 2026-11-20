#!/usr/bin/env bash
# Live smoke test against the real TickTick API.
# Requires: TICKTICK_TOKEN env (or ~/.config/ticktick/token), built ./ticktick.
# Safety: creates scratch projects "tt-cli-smoke-*", mutates only tasks inside
# them, and deletes them at the end (also on failure via trap).
set -euo pipefail
cd "$(dirname "$0")/.."

BIN=./ticktick
[ -x "$BIN" ] || { echo "build first: make build"; exit 1; }

# jget: extract a dotted path from JSON on stdin (node, no jq dependency).
jget() {
  node -e 'let d="";process.stdin.on("data",c=>d+=c);process.stdin.on("end",()=>{let j=JSON.parse(d);const p=process.argv[1];if(p){for(const k of p.split(".")){j=j?.[k]}}console.log(typeof j==="object"&&j!==null?JSON.stringify(j):(j??""))})' "${1:-}"
}

step() { printf '\n== %s\n' "$*"; }

step "preference"
$BIN preference --json

step "project list (read-only)"
$BIN project list --json | jget '' | head -c 300; echo

step "tag list / group list / habit list / countdown list / habit sections (read-only)"
$BIN tag list --json >/dev/null
$BIN group list --json >/dev/null
$BIN habit list --json >/dev/null
$BIN countdown list --json >/dev/null
$BIN habit sections --json >/dev/null
echo ok

step "focus list (read-only, last 30 days, both types)"
$BIN focus list --from "$(date -u -d '-29 days' +%Y-%m-%d)" --to "$(date -u +%Y-%m-%d)" --type 0 --json >/dev/null
$BIN focus list --from "$(date -u -d '-29 days' +%Y-%m-%d)" --to "$(date -u +%Y-%m-%d)" --type 1 --json >/dev/null
echo ok

NAME="tt-cli-smoke-$RANDOM$RANDOM"
PID=""
PID2=""
cleanup() {
  for p in "$PID2" "$PID"; do
    if [ -n "$p" ]; then
      step "cleanup: delete scratch project $p"
      $BIN project delete "$p" --json || true
    fi
  done
}
trap cleanup EXIT

step "create scratch project $NAME"
PID=$($BIN project create --name "$NAME" --color '#F18181' --json | jget id)
[ -n "$PID" ] || { echo "FAIL: no project id"; exit 1; }
echo "PID=$PID"

step "create two tasks"
T1=$($BIN task create --project "$PID" --title "smoke task 1" --priority 3 --tag smoke --json | jget id)
T2=$($BIN task create --project "$PID" --title "smoke task 2" --due "$(date -u +%Y-%m-%d)" --json | jget id)
echo "T1=$T1 T2=$T2"

step "get task"
$BIN task get "$T1" --project "$PID" --json | jget title

step "update task"
$BIN task update "$T1" --project "$PID" --title "smoke task 1 updated" --json | jget title

step "add comments"
C1=$($BIN task comment-add "$T1" --project "$PID" --text "comment A" --json | jget id)
$BIN task comment-add "$T1" --project "$PID" --text "comment B" --json >/dev/null
$BIN task comment-add "$T2" --project "$PID" --text "comment C" --json >/dev/null
echo "C1=$C1"

step "list comments of T1"
$BIN task comments "$T1" --project "$PID" --json

step "BATCH comments (workers=4) — map[id][]comments"
BATCH=$($BIN task comments-batch --project "$PID" --ids "$T1,$T2" --workers 4 --json)
echo "$BATCH"
echo "$BATCH" | node -e 'let d="";process.stdin.on("data",c=>d+=c);process.stdin.on("end",()=>{const j=JSON.parse(d);const [a,b]=process.argv.slice(1);if(!Array.isArray(j[a])||!Array.isArray(j[b]))throw new Error("batch map missing keys");if(j[a].length!==2||j[b].length!==1)throw new Error("unexpected comment counts: "+j[a].length+"/"+j[b].length);console.log("batch OK: "+a+"="+j[a].length+", "+b+"="+j[b].length)})' "$T1" "$T2"

step "search / filter / undone"
$BIN task search "smoke" --projects "$PID" --json | jget '' | head -c 200; echo
$BIN task filter --projects "$PID" --status 0 --json >/dev/null
$BIN task undone --start "$(date -u +%Y-%m-%d)" --end "$(date -u -d '+7 days' +%Y-%m-%d)" --projects "$PID" --json >/dev/null
echo ok

step "project data / members"
$BIN project data "$PID" --json >/dev/null
ME=$($BIN project members "$PID" --json | jget '0.username')
echo "me=$ME"

step "assign / unassign T1"
$BIN task assign "$T1" --project "$PID" --username "$ME" --json | jget assigneeUsername
$BIN task unassign "$T1" --project "$PID" --json >/dev/null
echo ok

step "batch add T3 + update T1"
B=$($BIN task batch --data "{\"add\":[{\"title\":\"smoke task 3\",\"projectId\":\"$PID\"}],\"update\":[{\"id\":\"$T1\",\"projectId\":\"$PID\",\"title\":\"smoke task 1 batched\"}]}" --json)
echo "$B" | jget status
T3=$(echo "$B" | node -e 'let d="";process.stdin.on("data",c=>d+=c);process.stdin.on("end",()=>{const j=JSON.parse(d);const it=(j.items||[]).find(x=>x.operation==="add");if(!it)throw new Error("no add item");console.log(it.taskId)})')
echo "T3=$T3"

step "move T3 to second scratch project and back"
PID2=$($BIN project create --name "$NAME-2" --json | jget id)
$BIN task move --from "$PID" --to "$PID2" --ids "$T3" --json | jget '0.id'
$BIN task move --from "$PID2" --to "$PID" --ids "$T3" --json | jget '0.id'
echo ok

step "focus create / get / delete on T1"
DAY=$(date -u +%Y-%m-%d)
F=$($BIN focus create --type 1 --task "$T1" --note "smoke focus" --start "$DAY 06:00" --end "$DAY 06:25" --duration 1500 --json)
FID=$(echo "$F" | jget id)
$BIN focus get "$FID" --type 1 --json | jget id
$BIN focus delete "$FID" --type 1 --json | jget id
echo ok

step "complete-many T3, complete T1, list completed"
$BIN task complete-many --ids "$T3" --project "$PID" --json
$BIN task complete "$T1" --project "$PID" --json
$BIN task completed --projects "$PID" --json | jget '' | head -c 200; echo

step "delete comment C1"
$BIN task comment-delete "$T1" --project "$PID" "$C1" --json

step "delete T2"
$BIN task delete "$T2" --project "$PID" --json

step "help contains example API response"
$BIN task create --help | grep -q "Example API response" && echo "help example OK"

step "SMOKE OK"