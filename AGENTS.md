# 给参与开发的 agent

Homing 主要由几个 coding agent 一起开发（Codex、WorkBuddy、DSH），共用这台 Mac 上的同一个工作目录。动手前先读这份和 docs/go-architecture.md。

## 目录归属

见 docs/go-architecture.md §3。只改自己负责的目录；要改别人的目录或 internal/agent/agent.go 的接口，先在汇报里说明，由用户协调。

## 提交

- 只 `git add` 自己负责的路径；不要切分支、不要 stash、不要 reset、不要 `git add -A`。
- 加了依赖就把 go.mod / go.sum 和代码一起提交；提交前 `go build ./...` 和自己包的 `go test` 要过（别人目录里的半成品编不过时，在汇报里说，不要替别人改）。
- 提交前跑 gitleaks，确认没有令牌、密钥和真实会话内容；测试数据要明显是造的（例如 demo-session-1），不要伪装成真实数据。
- 推送：`https_proxy=http://127.0.0.1:7890 http_proxy=http://127.0.0.1:7890 git push origin main`。

## 不要做的事

- 不往用户真实的 WorkBuddy、Claude、Codex 会话里发消息；测试回复只用 /tmp 下新建的 Codex 测试线程。
- 不停 8787 端口上的 ResetWatch；不改 Codex hooks.json 的信任状态；不改 Claude 自己的配置。
- 不停 18787 上的 Node 中继（手机正在用），切换由用户确认后进行。
- 安卓发布签名的密码只在钥匙串里；不要把发布签名的 APK 装到用户手机上（签名和调试版不同）。
- 汇报里不贴令牌和会话内容。

## 常用命令

```sh
go build ./...                       # 全部编译
go test ./internal/<你的包>/...       # 只测自己的包
go run ./cmd/homing serve --addr 127.0.0.1:18788 --data-dir /tmp/homing-dev
curl -s 127.0.0.1:18788/health
```

## 踩过的坑

记在 LESSONS.md（没有就新建），一条一行，写清楚现象和解决办法。
