#!/usr/bin/env bash
# 诊断：Node/Go 会话列表差异（只读）。输出写到 .verify/diag-sessions.log
set -u
cd "$(dirname "$0")/.."
mkdir -p .verify
BASE_DIR="${HOMING_DATA_DIR:-$HOME/Library/Application Support/Homing}"
TOKEN="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["token"])' "$BASE_DIR/config.json")"
OUT=.verify/diag-sessions.log
: > "$OUT"
for port in 18787 18788; do
  curl -s -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$port/api/sessions?limit=200&include_empty=1" > ".verify/sess-$port.json"
done
python3 - >> "$OUT" <<'PY'
import json
def load(p):
    return {s["session_id"]: s for s in json.load(open(f".verify/sess-{p}.json"))["sessions"]}
n, g = load(18787), load(18788)
print("node", len(n), "go", len(g))
print("--- 只在 Go 里的会话"); 
for k in g.keys() - n.keys():
    s = g[k]; print(k, s["source"], s["cwd"], s["last_ts"], "empty=", s["is_empty"])
print("--- 只在 Node 里的会话")
for k in n.keys() - g.keys():
    s = n[k]; print(k, s["source"], s["cwd"], s["last_ts"], "empty=", s["is_empty"])
print("--- 两边都有但 is_empty 不同")
for k in n.keys() & g.keys():
    if n[k]["is_empty"] != g[k]["is_empty"]:
        print(k, n[k]["source"], n[k]["cwd"], "node=", n[k]["is_empty"], "go=", g[k]["is_empty"], n[k]["title"])
print("--- 两边都有但 last_ts / event_count 不同")
for k in n.keys() & g.keys():
    a, b = n[k], g[k]
    if a["last_ts"] != b["last_ts"] or a["event_count"] != b["event_count"]:
        print(k, a["source"], "node:", a["last_ts"], a["event_count"], a["status"], "| go:", b["last_ts"], b["event_count"], b["status"])
PY
for id in 3a7f8694-48fc-4825-9786-baef81933fa6; do
  f="$(ls ~/.claude/projects/*/$id.jsonl 2>/dev/null | head -1)"
  echo "--- transcript $id: $f" >> "$OUT"
  [ -n "$f" ] && { wc -c "$f" >> "$OUT"; cut -c1-300 "$f" | head -12 >> "$OUT"; }
  for port in 18787 18788; do
    echo "--- items@$port" >> "$OUT"
    curl -s -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$port/api/sessions/$id/items?limit=12" | cut -c1-600 >> "$OUT"
    echo >> "$OUT"
  done
done
echo done
