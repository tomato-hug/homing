#!/usr/bin/env bash
# Homing · Node/Go 逐字段对照
#
# 同时请求 18787（Node 中继，仍在给手机用）和 18788（`homing serve`，Go 版），
# 把两边响应规范化后逐字段 diff。规范化只做两件事：
#   1) 去掉每毫秒都在变的时间戳字段；
#   2) 把数组按稳定键排序（两边的排序规则一致，但同 last_ts 的先后可能不同）。
# 其余字段原样比较，diff 一行为一处不一致。
#
# 用法：
#   scripts/parity.sh                 # 默认比 18787 / 18788
#   scripts/parity.sh 18787 18888     # 自定义端口
#   HOMING_TOKEN=xxx scripts/parity.sh
#
# 只读：脚本只发 GET，不改任一边的状态。PATCH/回复类接口不在这里测
# （那会写 events.jsonl / session-state.json，得用临时数据目录单独跑）。
set -u

NODE_PORT="${1:-18787}"
GO_PORT="${2:-${GO_PORT:-18788}}"
BASE_DIR="${HOMING_DATA_DIR:-$HOME/Library/Application Support/Homing}"

if [[ -z "${HOMING_TOKEN:-}" ]]; then
  HOMING_TOKEN="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["token"])' "$BASE_DIR/config.json" 2>/dev/null)"
fi
if [[ -z "${HOMING_TOKEN:-}" ]]; then
  echo "拿不到 token：把 HOMING_TOKEN 设一下，或确认 $BASE_DIR/config.json 存在" >&2
  exit 2
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# 规范化：删掉变化的时间戳，数组排序，键按字典序输出。
read -r -d '' NORMALIZE <<'PY'
import json, sys

DROP = {
    "generated_at", "last_event_at", "running_since", "ts", "at",
    "last_seen_ms", "updated_at", "next_cursor",
}
# 列表的稳定排序键：有 session_id 的先按 source+session_id，再按 event/reply id。
KEYS = ["source", "session_id", "reply_id", "event_id", "id", "kind", "level", "cwd"]

def scrub(value):
    if isinstance(value, dict):
        return {k: scrub(v) for k, v in value.items() if k not in DROP}
    if isinstance(value, list):
        items = [scrub(v) for v in value]
        if all(isinstance(i, dict) for i in items):
            def sort_key(item):
                return tuple(str(item.get(k, "")) for k in KEYS)
            items.sort(key=sort_key)
        return items
    return value

data = json.load(sys.stdin)
json.dump(scrub(data), sys.stdout, ensure_ascii=False, indent=1, sort_keys=True)
PY

fetch() { # port path outfile
  curl -sS --noproxy '*' -m 30 -H "Authorization: Bearer $HOMING_TOKEN" \
    "http://127.0.0.1:$1$2" -o "$3" || return 1
  [[ -s "$3" ]] || { echo "空响应：$1$2" >&2; return 1; }
}

compare() { # path
  local path="$1" tag node go rc
  tag="$(echo "$path" | tr '/?=&' '____' | tr -d '%')"
  node="$TMP/node-$tag.json"
  go="$TMP/go-$tag.json"
  if ! fetch "$NODE_PORT" "$path" "$node"; then
    echo "SKIP  $path  （Node 侧取不到）"
    return 0
  fi
  if ! fetch "$GO_PORT" "$path" "$go"; then
    echo "FAIL  $path  （Go 侧取不到）"
    return 1
  fi
  python3 -c "$NORMALIZE" < "$node" > "$TMP/node-$tag.norm" 2>/dev/null
  python3 -c "$NORMALIZE" < "$go" > "$TMP/go-$tag.norm" 2>/dev/null
  if diff -q "$TMP/node-$tag.norm" "$TMP/go-$tag.norm" >/dev/null; then
    echo "OK    $path"
    return 0
  fi
  echo "DIFF  $path"
  diff -u "$TMP/node-$tag.norm" "$TMP/go-$tag.norm" | sed -n '1,400p' | sed 's/^/      /'
  return 1
}


# items 专用投影：Node 的条目带 tool/files/plan/model 等富字段，Go 的 agent.Item 只有
# id/role/kind/seq/at/text/error（接口在 internal/agent/agent.go，本轮没改）。
# 这里把两边投影到共同语义再比，差异就会被看见；富字段的缺口在汇报里说明。
read -r -d '' PROJECT_ITEMS <<'PYPROJ'
import json, sys
raw = json.load(sys.stdin)
items = raw.get("items") or []
out = []
for it in items:
    # 只比结构与顺序：id / role / kind / seq。
    # 文本不比，因为 Node 的条目把文字分在 text / command / tool.output_tail 三处，
    # Go 的 agent.Item 只有一个 text 字段——要完全一致得先扩接口（见汇报）。
    out.append({
        "id": it.get("id"),
        "role": it.get("role"),
        "kind": it.get("type") or it.get("kind"),
        "seq": it.get("seq"),
    })
print(json.dumps({"source": raw.get("source"), "count": len(out), "items": out},
                 ensure_ascii=False, indent=1, sort_keys=True))
PYPROJ

compare_items() { # session_id
  local sid="$1" tag node go
  tag="$(echo "$sid" | tr -d '%' | tr '/?=&' '____')"
  node="$TMP/node-items-$tag.json"
  go="$TMP/go-items-$tag.json"
  if ! fetch "$NODE_PORT" "/api/sessions/$sid/items?limit=30" "$node"; then
    return 0
  fi
  if ! fetch "$GO_PORT" "/api/sessions/$sid/items?limit=30" "$go"; then
    echo "FAIL  items $sid （Go 侧取不到）"
    return 1
  fi
  python3 -c "$PROJECT_ITEMS" < "$node" > "$TMP/node-items-$tag.norm" 2>/dev/null
  python3 -c "$PROJECT_ITEMS" < "$go" > "$TMP/go-items-$tag.norm" 2>/dev/null
  if diff -q "$TMP/node-items-$tag.norm" "$TMP/go-items-$tag.norm" >/dev/null; then
    echo "OK    items $sid"
    return 0
  fi
  echo "DIFF  items $sid"
  diff -u "$TMP/node-items-$tag.norm" "$TMP/go-items-$tag.norm" | sed -n '1,200p' | sed 's/^/      /'
  return 1
}

PATHS=(
  "/api/agents"
  "/api/projects"
  "/api/inbox"
  "/api/sessions?limit=50"
  "/api/sessions?source=codex&limit=20"
  "/api/sessions?source=claude&limit=20"
  "/api/sessions?source=workbuddy&limit=20"
  "/api/sessions?limit=10&include_empty=1"
  "/api/replies"
  "/api/events?since=0"
)

echo "Node  : http://127.0.0.1:$NODE_PORT"
echo "Go    : http://127.0.0.1:$GO_PORT"
echo "数据  : $BASE_DIR"
echo

fail=0
for path in "${PATHS[@]}"; do
  compare "$path" || fail=$((fail + 1))
done

# 会话条目：每个 source 取 Node 侧最近的 2 个会话，逐个比 items
echo
echo "--- 会话条目 (/api/sessions/<id>/items) ---"
for src in codex claude workbuddy; do
  curl -sS --noproxy '*' -m 30 -H "Authorization: Bearer $HOMING_TOKEN" \
    "http://127.0.0.1:$NODE_PORT/api/sessions?source=$src&limit=2" -o "$TMP/ids-$src.json" 2>/dev/null
  for sid in $(python3 -c '
import json,sys
try:
    d=json.load(open(sys.argv[1]))
except Exception:
    sys.exit(0)
for s in (d.get("sessions") or []):
    print(s.get("session_id",""))
' "$TMP/ids-$src.json" 2>/dev/null); do
    [ -n "$sid" ] || continue
    compare_items "$sid" || fail=$((fail + 1))
  done
done


echo
if [[ $fail -eq 0 ]]; then
  echo "全部一致（$(( ${#PATHS[@]} )) 个接口）"
else
  echo "$fail / ${#PATHS[@]} 个接口有差异"
fi
exit $(( fail > 0 ? 1 : 0 ))
