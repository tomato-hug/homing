# Go / Node 对照：怎么跑，哪些差异是故意留的

切换前，Go 版（`homing serve`）的 `/api/*` 要和 Node 中继（`relay/server.mjs`，127.0.0.1:18787）给手机的内容一致。这份文档记录对照方法和已经确认过、不需要再查的差异。

## 怎么跑

在仓库根目录：

```bash
bash scripts/verify-go.sh
```

脚本依次做：`go build` / `go vet` / `go test` → 把 `~/Library/Application Support/Homing` 拷一份到 `/tmp/homing-parity` → 用这份快照在 127.0.0.1:18788 临时起一个 Go 服务 → 跑 `scripts/parity.sh` 和 `scripts/diag-sessions.sh` → 停掉临时服务。它只读 Node 那一侧，不动真实数据目录。18788 被占用时会直接退出，先关掉手动起的 `homing serve`。

结果在 `.verify/`（已忽略，不进 git）：

| 文件 | 内容 |
|---|---|
| `go-verify.log` | 整次运行的输出 |
| `parity-full.log` | 每个接口的逐字段 diff |
| `diag-sessions.log` | 两边会话列表的差别：只在一边出现的、`is_empty` 不同的、`last_ts` 不同的 |
| `go-serve.log` | 临时 Go 服务的日志 |

对照前先确认 Node 进程是当前代码：`go-verify.log` 里会打印 Node 的启动时间和 `relay/server.mjs` 的修改时间，启动时间更早就先重启（`launchctl kickstart -k gui/$(id -u)/dev.agentpush.relay`）。

对照时自己正在用的 Codex 会话还在写入，`limit=10&include_empty=1` 这类取到边界条目的接口可能出现一次性的 `last_ts` / `title` / `last_preview` 差异。看 `diag-sessions.log` 的「两边都有但 last_ts / event_count 不同」：如果差的只是当前会话，不是代码问题，重跑即可。

## 故意保留的差异

这些项在 `parity.sh` 里仍然显示 DIFF。每一项都查过原因，结论是 Go 的行为正确或更好，不照搬 Node。

| 接口 | 差异 | 原因 |
|---|---|---|
| `/api/sessions` | Go 把一些会话算作非空（`is_empty: false`），Node 算作空并默认隐藏 | Node 的 `isSessionEmptyUncached` 把 `readRecentItems()` 的返回值当数组用，实际是 `{items, turns}`，于是退化成"没有 `last_preview` 就算空"。Go 按注释里的本意判断：有 user / assistant 条目就不算空。切换后手机上会多出这些会话（多数是测试用的短会话） |
| `/api/sessions` | 个别 Codex 会话 Go 有 `cwd`、`last_preview`，Node 是 null | Node 的 rollout 索引没找到这些会话的文件，Go 找到了 |
| `/api/agents` | WorkBuddy 的 `stop`：Go 是 false，Node 是 true | Go 版还没实现 WorkBuddy 的停止，能力位如实报 false。实现后改回 true |
| `…/items`（Codex） | Go 不显示 developer 角色的上下文消息（`<app-context>`、`<multi_agent_mode>` 等），因此 `codex-iN` 的编号比 Node 小 | Node 把非 assistant 的消息一律当作用户消息显示，这些是 Codex 注入的上下文，不是用户说的话 |
| `…/items` | 工具参数摘要（`input_summary`）的键顺序不同 | Go 的 `encoding/json` 按字母序输出 map 的键，Node 保持原顺序。内容相同 |
| `…/items` | 长输出的 `output_tail` 和 `exit_code` 不同 | Node 先把输出截到前 800 字再取尾部和退出码，长输出时取到的是开头、退出码丢失。Go 取真正的尾部 |
| `…/items` | Node 的 `output_tail` 偶尔是 `[object Object]` | Node 把对象直接转成了字符串。Go 输出文本内容 |
| `…/items` | Codex 工具结果没有退出码时，Node 写 `exit_code: null`，Go 省略该字段 | 客户端把缺失和 null 都当作没有退出码；Go 只在真实拿到退出码时输出 |

没有实现、也不挡切换的两个接口：`POST/GET /api/client-log`、`GET /api/sessions/{id}/transcript`。安卓端对它们的 404 有容错。

## 新发现一处差异时

先在 `relay/server.mjs` 里找到对应逻辑，判断是 Go 写漏了还是 Node 本身有问题。Go 写漏了就改 Go；Node 有问题就在上表加一行，写清原因。不要为了让对照变绿去复制 Node 的 bug。
