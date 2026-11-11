# Homing agent 接入套件

这份目录放的是**可复制的接入模板**，不是由 `relay/server.mjs` 自动加载的运行时代码。
目标是让新 agent 不用改 Homing 源码就能接进来：

| 方式 | 目录/接口 | 适合 |
|---|---|---|
| 生命周期钩子 | `/hook/generic` | 能执行命令或发 HTTP 的 agent |
| 事件上报 | `/api/ingest` + Bearer 令牌 | 独立进程、脚本、远程开发机；可登记回复回调 URL |
| 清单 | `~/.homing/agents/*.yaml` | 会话在 JSONL / JSON / SQLite 文件里的 agent |
| 脚本适配器 | `~/.homing/adapters/*.mjs` | 格式特殊、需要自己解析的 agent |

Go 服务默认监听 `0.0.0.0:18787`（本机 hook 用 `127.0.0.1:18787`）；如果运行在别的端口，把模板里的端口一起改掉。
所有钩子和 ingest 都只接受本机回环来源，不会把本机 agent 配置暴露到局域网。

控制台的 Agent 页还会读 `GET /api/agent-catalog` 的统一目录：已核实格式的 agent 可以一键安装
`~/.homing/agents/<id>.yaml`；没有会话读取能力的 agent 只会标成「原生钩子」或「通用钩子」，
不会伪装成已支持会话扫描。

## 每家 Agent 的接入套件

Agent 页的「快速接入」会调用 `POST /api/agent-catalog/<id>/quick`：按需安装清单、
生成该 Agent 的接入套件，并打开 Homing 自己的接收入口。它不会执行第三方 Agent 的
`npm` / `curl` 安装命令，也不会改该 Agent 的真实配置。

想只准备文件、不打开接入时，使用「只生成套件」调用
`POST /api/agent-catalog/<id>/kit`，在 `~/.homing/integrations/<id>/` 生成：

| 文件 | 用途 | 是否会被自动加载 |
|---|---|---|
| `hook.sh` / `hook.mjs` | 从 stdin 收 JSON，转给 `/hook/generic`；失败也退出 0，不阻塞 Agent | 否，由 Agent 的钩子命令调用 |
| `homing-run.sh` | 包装任意 CLI，退出时发送 `done` / `error` | 否，手工用它启动命令 |
| `manifest.draft.yaml` | 会话清单骨架 | 否，`.draft.yaml` 不在 `~/.homing/agents` |
| `adapter.draft.mjs` | 脚本适配器骨架 | 否，`.draft.mjs` 不在 `~/.homing/adapters` |
| `README.md` | 该 Agent 的路径、配置和接线说明 | 否 |
| `aider-notify.sh` | 仅 Aider：给 `--notifications-command` 用 | 否 |
| `kilo-plugin.js` | 仅 Kilo Code：原生 OpenCode 风格插件 | 否，需手工复制到 `~/.config/kilo/plugin/` |

生成器是幂等的：目录已存在时只补缺失文件，不覆盖用户改过的内容。套件不会改该 Agent 自己的配置，也不会把未核实的 draft 放进自动加载目录。
「移除接入套件」调用 `DELETE /api/agent-catalog/<id>/kit`，目录会改名成
`<id>.deleted-<时间戳>` 留在原处，不会递归删除用户改过的文件。

这套设计覆盖三类接入：

1. **能挂命令钩子**：用 `hook.mjs` / 原生特化脚本；
2. **只能从终端启动**：用 `homing-run.sh` 包装命令；
3. **能读本地文件**：先核实一份样本，再改 draft 的 `glob` / parser，手工移到 `~/.homing/agents` 或 `~/.homing/adapters`。

目录里 57 家的 `hook_support`、`sessions`、`reply` 分别表示能力，不互相替代。尤其 `sessions=false` 的发现条目，即使可以生成事件钩子，也不会在会话页出现伪造数据。

## 钩子

- `hooks/codex-hooks-snippet.json`：Codex `~/.codex/hooks.json` 的 `PermissionRequest` 片段。**Homing 不会自动写 Codex 的 hooks.json**，因为你需要在 Codex 里重新确认信任状态。
- `hooks/claude-settings-snippet.json`：Claude Code / WorkBuddy 风格的 `settings.json` 片段。控制台的「安装钩子」会在备份后自动合并，手工安装时也可以复制。
- `hooks/homing-hook.sh`、`hooks/homing-hook.mjs`：从 stdin 读取一段完整 JSON，原样 POST 到 `/hook/generic`。命令失败不会阻塞 agent。

通用钩子的最小 JSON：

```json
{
  "source": "my-agent",
  "session_id": "demo-session-1",
  "kind": "needs_reply",
  "title": "等待确认",
  "text": "要现在发布吗？",
  "cwd": "/tmp/demo-project"
}
```

`source` 只允许 1–64 位小写字母、数字、点、下划线、连字符；`kind` 可用
`reply`、`done`、`needs_reply`、`needs_input`、`permission`、`approval`、`error`、`message`。

`homing-hook.sh` / `homing-hook.mjs` 都可以用 `HOMING_AGENT`（或第一个参数）
补 `source`，用 `HOMING_KIND` 补 `kind`。Node 版还会把常见的
`hook_event_name`、`thread-id`、`last_assistant_message` 等字段转换成 Homing
字段；服务端也直接接受这些别名，所以很多 agent 的原始 hook JSON 不用改写。

## 清单与脚本

- `manifests/generic-jsonl.yaml`：一行一条 JSON 的 agent。
- `manifests/generic-json.yaml`：单个 JSON 对象或 JSON 数组、含 `messages` 的 agent。
- `adapters/generic-adapter.mjs`：脚本适配器最小骨架。

清单保存到 `~/.homing/agents/`，脚本保存到 `~/.homing/adapters/`。
清单里的 `reply.command` 是**参数数组**，由 Go 直接 `spawn`，不经过 shell。

## 还没有做的

- 适配器市场、签名、版本与自动更新。
- 对 Markdown / 纯文本日志的通用解析器；这类 agent 先导出 JSONL，或写脚本适配器。
- 脚本网络隔离目前是 `HOMING_ADAPTER_NO_NET=1` 约定，Node 权限模型还没有强制封网。
