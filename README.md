# Homing

**Every coding agent, one pocket inbox.**

Homing 把电脑上的 AI 编程会话带到 Android 手机：接收消息、阅读连续上下文、回复、处理授权，以及在会议室中协调多个 Agent。电脑运行 Go 服务，Android 使用 Kotlin / Compose；云端中继转发设备之间的端到端加密消息。

项目当前处于 Beta。可用功能、安装包区别和已知限制见 [Beta 指南](docs/beta-release.md)。不同 Agent 的能力取决于本机安装、登录状态和适配器；在控制台的 Agent 页检查实际能力。

## 连接方式

```text
Android APK ← WSS / 加密消息 → homing-server ← WSS / 加密消息 → Mac / Linux 上的 homing
                                                                        ↓
                                                                本机 Agent 与项目
```

同一局域网也可以直接连接。公网使用时，电脑服务与手机都连接你选择的中继；电脑无需开放公网入站端口。电脑必须保持运行并联网。Mac 管理窗口可以关闭，`homing serve` 或常驻服务需要继续运行。

## 当前功能

- 收件箱、会话、项目和会话搜索；同一电脑、同一 Agent、同一会话合并展示连续消息。
- 手机从首次收到的消息开始展示上下文；按需载入更早的 10 轮对话，并缓存已经载入的历史。
- 清空收件箱、移除会话及撤销只影响手机本地显示，不删除电脑上的会话。
- Markdown、代码、附件与应用内 HTTPS 浏览器。
- 按 Agent 能力提供回复、停止、授权、模型选择与任务派发。
- 多 Agent 会议室、点名、暂停 / 恢复、纪要与需要确认的行动项派发。
- 局域网 / 公网配对、多电脑切换、断线重连和分页补同步。

## 从源码开始

准备 Go 1.26、已安装并登录的目标 Agent，以及 Git。构建控制台源代码另需 Node.js；仓库已包含可嵌入的控制台产物。

```sh
git clone https://github.com/tomato-hug/homing.git
cd homing
go build -o homing ./cmd/homing
./homing serve --addr 127.0.0.1:18787
```

打开电脑上的 `http://127.0.0.1:18787/console`，检查 Agent 接入状态。Linux 或不需要桌面窗口时：

```sh
CGO_ENABLED=0 go build -tags headless -o homing ./cmd/homing
```

`homing`（无子命令）在受支持的桌面构建中打开窗口与托盘；`homing serve` 提供同一套服务和网页控制台。不要让两个实例同时占用同一端口或写入同一个数据目录。

### Android APK

准备 JDK 17、Android SDK 35 与 Build Tools 35.0.0，设置 `ANDROID_HOME` 或在 `android/local.properties` 指定 SDK 路径：

```sh
cd android
./gradlew :app:testDebugUnitTest :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

最低 Android 8.0 / API 26。手机允许安装相应来源的 APK，并按需授予通知权限。后续升级必须使用同一签名；开发调试包和正式签名包不能互相覆盖安装，详情见 [Beta 指南](docs/beta-release.md)。

### 通过公网中继配对

1. 部署 `homing-server`，在前面配置受信任的 TLS 证书及 WebSocket 转发，参见 [自托管指南](docs/self-host.md)。公网地址形如 `wss://homing.example.com/v1/ws`。
2. 在电脑控制台「连接 › 中转与局域网」设置该地址，等待中继连接成功。
3. 在另一终端执行 `./homing pair --serve http://127.0.0.1:18787`，在手机扫描二维码或输入配对码。
4. 核对两端的四个验证词 / 主机指纹，并在电脑确认配对。
5. 手机关闭 Wi-Fi、使用移动数据，检查消息、回复与重连。只验证 `/health` 并不能证明手机链路完整可用。

局域网直连需要让服务监听手机可达的地址，例如 `--addr 0.0.0.0:18787`，并允许本地防火墙访问。公网中继仅公开其 HTTPS/WSS 入口；不要把电脑的 `/api` 或管理控制台直接转发到公网。

## 文档与贡献

- [Beta 安装、升级和限制](docs/beta-release.md)
- [自托管中继、TLS、配对与备份](docs/self-host.md)
- [贡献指南与验证命令](CONTRIBUTING.md)
- [安全边界与漏洞报告](SECURITY.md)
- [更新记录](CHANGELOG.md)
- [Go 架构](docs/go-architecture.md)、[协议](docs/protocol.md)、[适配器](docs/adapters.md)

`cmd/homing` 是电脑端入口，`cmd/homing-server` 是中继入口；`internal/` 为 Go 实现，`android/` 为手机端，`console/` 为控制台源码。`relay/`、`server/`、`desktop/` 中保留早期 Node / Tauri 实现与兼容测试，新部署优先使用 Go 入口。

Homing 采用 [Apache-2.0](LICENSE) 许可。第三方字体与依赖保留各自许可证，见 [第三方说明](NOTICE)。
