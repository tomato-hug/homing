# 通过中国大陆公网中继管理代理网络设备（双栈分流指南）

本文档专门针对中国大陆及跨境混合网络开发者，详细说明如何利用 **一台低成本中国大陆公网云服务器（1 核 1G 即可）** 部署 `homing-server` 端到端加密盲中继，从而在 **开发机挂有代理网络（访问 Claude / Codex / Gemini 等海外模型）** 的前提下，让 **手机端无需安装或开启任何 VPN / 代理软件**，仅凭普通国内 5G / Wi-Fi 网络即可低延迟接管所有桌面与远程开发机 Agent。

---

## 1. 为什么传统远程方案在「代理开发机」场景下体验糟糕？

当你的 Mac、Windows WSL2 或远程 Linux 开发机为了使用 Claude Code、Codex、Cursor、GitHub Copilot 而开启了本地代理（`HTTP_PROXY=http://127.0.0.1:7890` 或 Clash / Mihomo / Surge / Sing-box 的 **TUN 透明代理 / Fake-IP 模式**）时，通常会遇到三大死结：

1. **官方移动端强制手机翻墙**：海外 Agent 官方 App 要求手机也时刻挂着代理，不仅高耗电、拖慢微信/导航/支付等国内应用，而且在地铁、电梯、高铁基站切换时极易断流卡死。
2. **开发机回连国内中继发生「U 型跨国绕行」**：如果开发机上的守护进程继承了系统代理或被 `198.18.0.0/15` TUN Fake-IP 劫持，开发机发往国内云中继的流量会先绕道香港/日本/美国代理节点再回到国内中继，往返延迟暴涨至 `250–600 ms` 且频繁断连。
3. **开发机没有公网 IP**：家庭宽带、公司办公网、校园网或云服务器内网均处于 NAT 之后，无法安全暴露入站端口。

---

## 2. Homing 的「双栈分流（Split-Horizon）」架构原理

Homing 将网络流量严格切分为互不干扰的两个平面：

- **模型流量面（Agent Plane）—— 照常走开发机代理**：
  `Claude Code`、`Codex`、`Gemini CLI` 等进程运行在你原本的终端与系统环境中，继续使用 `127.0.0.1:7890` 或系统 TUN 网卡顺畅访问海外大模型 API；同时 `Kimi`、`Qwen Code`、`DSH` 等国内模型也可并行工作。
- **控制与中继面（Control & Relay Plane）—— 纯国内骨干网直连**：
  1. **环境变量级代理隔离**：`homing` 连接中继时默认启用 `DirectDial`（显式将 Go `http.Transport.Proxy` 置为 `nil`），自动忽略本机 `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY` 环境变量。
  2. **TUN Fake-IP 自动识别与公共 DNS 穿透**：当系统开启 Clash / Mihomo / Surge / Sing-box 的 Fake-IP 模式导致系统 DNS 返回 `198.18.0.0/15`（IPv4）或 `fc00::/18`（IPv6）虚拟地址时，Homing 连接器会自动向国内公共 DNS（`223.5.5.5` / `119.29.29.29`）直接查询中继域名的**真实国内公网 IP** 发起 TCP 直连，同时保留原始域名用于 TLS SNI 握手与证书严格校验。
  3. **端到端零知识加密**：哪怕中继跑在最便宜的入门级公网云主机上，`homing-server` 也只看得到 `XChaCha20-Poly1305` 密文信封，不接触任何明文代码、Prompt、密钥或图片。

---

## 3. 中国大陆公网中继服务器选型与部署

### 3.1 服务器规格建议

由于 `homing-server` **不运行任何大模型、不解密任何报文、不存储大型文件**（仅做 WebSocket 密文转发与 SQLite 小型离线队列），极低配置即可流畅运行：

- **CPU / 内存**：**1 核 1 GB**（或 2 核 2 GB）轻量应用服务器即可支持数十台设备同时在线。
- **峰值带宽**：**3 Mbps – 5 Mbps** 即可满足日常流式对话、代码 Diff 与移动端截图附件秒传。
- **域名与备案**：在中国大陆境内云厂商（腾讯云、阿里云、华为云等）使用 `443` 端口提供标准 `wss://` 服务时，请使用**已完成 ICP 备案的域名**并配置有效 HTTPS 证书（未备案域名在国内云厂商 80/443 端口会被云平台阻断跳转）。

### 3.2 服务端一键部署（Docker 或 systemd）

#### 方式 A：Docker 部署

```sh
git clone https://github.com/tomato-hug/homing.git
cd homing
docker build -f server/Dockerfile -t homing-server .

docker run -d \
  --name homing-server \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v homing-data:/data \
  homing-server

curl -s http://127.0.0.1:8080/health
# {"ok":true,"service":"homing-server","version":1}
```

#### 方式 B：纯静态二进制 ＋ systemd 原子安装脚本（推荐已有 Nginx 的云主机）

在开发机交叉编译 Linux 静态二进制：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o homing-server ./cmd/homing-server
shasum -a 256 homing-server
```

将 `homing-server`、`scripts/install-relay.sh`、`deploy/homing-relay.service` 上传至服务器后执行：

```sh
sudo bash scripts/install-relay.sh ./homing-server <刚才生成的SHA256> "$(git rev-parse HEAD)"
curl --noproxy '*' -s http://127.0.0.1:18789/health
```

> `install-relay.sh` 会将中继安全绑定在 `127.0.0.1:18789`（数据持久化于 `/var/lib/homing-relay`），若健康检查失败会自动回滚。

### 3.3 配置 WSS（TLS + WebSocket）反向代理

- **使用 Caddy（自动申请与续期证书）**：
  ```caddyfile
  homing.your-domain.cn {
      encode zstd gzip
      reverse_proxy 127.0.0.1:18789
  }
  ```
- **使用 Nginx**：直接复用仓库内的 [deploy/nginx-relay-location.conf](../deploy/nginx-relay-location.conf) 放入你的 `443 ssl` `server` 块中（已内置 `Upgrade` / `Connection` 头与长连接 `proxy_read_timeout 3600s`）。

---

## 4. 在装有代理网络的开发机上接入与调优

### 4.1 场景一：系统代理 / 环境变量代理（HTTP_PROXY / SOCKS5）

**零额外配置**。打开开发机控制台 `http://127.0.0.1:18787/console` → **「连接 › 中转与局域网」**：
1. 填入 `wss://homing.your-domain.cn/v1/ws` 点击 **「保存并重连」**。
2. 下方「代理网络与双栈分流」卡片默认处于 **`直连模式 · 已绕过本机代理`**，并会显示 **`已隔离 HTTP_PROXY 环境变量`** 与实时 **`RTT`（国内通常 15–45 ms）**。

### 4.2 场景二：Clash / Mihomo / Surge / Sing-box 开启了 TUN 虚拟网卡模式

- **Fake-IP 模式（默认常见）**：Homing 会自动检测 `198.18.0.0/15` Fake-IP 并通过国内公共 DNS 解析真实中继 IP 直连，控制台将显示 **`已自动绕过 TUN Fake-IP`**。
- **严格全局 L3 路由劫持（如 TUN `auto-route` 强制接管所有非规则 IP）**：如果观察到控制台 `RTT > 150 ms`，只需在控制台点击 **「复制 Clash / Mihomo 规则」** 或 **「复制 Surge 规则」**，将以下两行粘贴至你的代理配置顶部即可实现进程级与域名级 100% 物理网卡直连：

```yaml
# Clash / Mihomo (rules 顶部)
rules:
  - PROCESS-NAME,homing,DIRECT
  - DOMAIN,homing.your-domain.cn,DIRECT
```

```ini
# Surge / Loon ([Rule] 顶部)
PROCESS-NAME,homing,DIRECT
DOMAIN,homing.your-domain.cn,DIRECT
```

```json
// Sing-box (route.rules 顶部)
{
  "process_name": ["homing"],
  "domain": ["homing.your-domain.cn"],
  "outbound": "direct"
}
```

### 4.3 场景三：反向需求——中继部署在海外，开发机需要借道本机代理连中继

如果你的 `homing-server` 部署在海外（例如 Cloudflare / AWS / Fly.io），而开发机在境内必须通过本机代理才能稳定连接该海外中继：
- 在控制台「代理网络与双栈分流」卡片右上角点击 **「切换为走系统代理（海外中继）」**（或设置环境变量 `HOMING_RELAY_USE_PROXY=1`），Homing 即会复用系统代理连接中继。

---

## 5. 手机端（中国大陆 Android ROM）零代理保活建议

1. **无需任何代理软件**：手机在户外直接使用中国移动 / 联通 / 电信 5G 数据流量连接国内中继，延迟低且不消耗任何国际代理流量。
2. **开启后台无限制保活（未关闭 App 时实时收信）**：
   - ColorOS / 一加 / OPPO、澎湃 HyperOS / 小米、OriginOS / vivo、HarmonyOS 对后台长连接管控较严。
   - 在 Homing 左侧抽屉底部点击 **「开启后台无限制保活」** 快捷条，在系统电池设置中允许 Homing「后台无限制运行 / 允许自启动」。
   - 当你**没有划掉关闭 Homing**（仅锁屏或切到微信/浏览器）时，前景守护服务会保持实时重连与会话保留；当你**主动划掉关闭 Homing** 后再次打开时，历史已完成会话会自动净空（电脑后台运行中或待审批会话自动保留），随时可通过「恢复历史」挑会话载入最近 10 条记录。
3. **端到端链路冒烟验证**：上线前可在开发机执行仓库自带的公网盲中继全流程验证测试（自动用随机临时密钥验证挑战签名、配对、加密收发、断线补发与吊销，不触碰真实会话）：
   ```sh
   HOMING_TEST_RELAY_URL=wss://homing.your-domain.cn/v1/ws \
     go test -tags headless ./internal/server -run '^TestPublicRelaySmoke$' -count=1 -timeout=120s -v
   ```
