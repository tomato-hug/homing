# Homing 公共中继部署 / 升级说明（给服务器管理员）

本目录用于把 `homing-server` 部署或升级成「托管模式」公共中继：用户在 `/account` 注册拿到电脑端密钥（`hrk_...`），管理员在 `/admin`（或 Homing 桌面端的管理台）管理用户、注册策略、额度和兑换码。

后端只监听 `127.0.0.1:18789`，由本机 Nginx 反向代理到 HTTPS 域名（下文以 `relay.example.com` 为例）。

## 一、文件

```text
bin/homing-server-linux-amd64   x86_64 静态二进制
bin/homing-server-linux-arm64   ARM64 静态二进制
deploy/homing-relay.service     systemd 服务单元
deploy/homing-relay.env.example /etc/homing-relay.env 模板
deploy/nginx-relay-location.conf Nginx 反代配置（托管模式必看）
scripts/install-relay.sh        原子升级脚本（校验 SHA256、健康检查、失败自动回滚）
SHA256SUMS.txt                  二进制校验值
```

## 二、准备

```bash
tar -xzf homing-relay-deploy-*.tar.gz && cd homing-relay-deploy-*
uname -m            # x86_64 用 amd64，aarch64 用 arm64
ARCH=amd64          # 按上一行结果填写
# 已有旧数据时先备份
[ -f /var/lib/homing-relay/homing.sqlite ] && sudo cp -a /var/lib/homing-relay/homing.sqlite \
  "/var/lib/homing-relay/homing.sqlite.bak.$(date +%Y%m%d%H%M%S)"
```

## 三、部署步骤

### 1. 环境变量 `/etc/homing-relay.env`

```bash
sudo cp -n deploy/homing-relay.env.example /etc/homing-relay.env
sudo chmod 600 /etc/homing-relay.env && sudo chown root:root /etc/homing-relay.env
openssl rand -hex 32     # 生成管理员令牌
sudo nano /etc/homing-relay.env
```

至少填写：

- `HOMING_AUTH_MODE=managed`
- `HOMING_ADMIN_TOKEN=<上面生成的 64 位字符串>`
- `HOMING_PUBLIC_BASE_URL=https://relay.example.com`
- 登录方式至少一种，否则新用户无法注册：
  - 邮箱验证码：`HOMING_SMTP_HOST / PORT / USER / PASS / FROM`（国内服务器推荐阿里云邮件推送或企业邮箱，端口 465）
  - GitHub 登录：`HOMING_GITHUB_CLIENT_ID / SECRET`，回调地址 `https://relay.example.com/v1/account/github/callback`

额度默认值已经写好，一般不用改：

| 额度 | 免费用户 | 赞助用户 |
| --- | --- | --- |
| 每周远程使用时长 | 20 小时（`HOMING_FREE_WEEKLY_MINUTES=1200`） | 不限 |
| 每日流量 | 64 MB | 2 GB |
| 单条消息 | 512 KB | 25 MB |
| 电脑 / 手机数量 | 2 / 4 | 8 / 20 |

「每周远程使用时长」只统计手机主动发出请求（打开会话、回复、审批）的分钟数；电脑在线、心跳保活和推送到手机都不计时，所以时长用完后手机仍能收到通知，只是不能远程操作，每周一 00:00（北京时间）重置。

> `/etc/homing-relay.env` 由 systemd 以 root 读取后注入进程，`600 root:root` 权限是正常且最安全的。

### 2. 升级二进制

```bash
SHA=$(grep "homing-server-linux-$ARCH" SHA256SUMS.txt | cut -d' ' -f1)
sudo bash scripts/install-relay.sh "bin/homing-server-linux-$ARCH" "$SHA" "$(cat VERSION)"
```

脚本会校验 SHA256、原子切换 `/opt/homing-relay/current`、重启服务并探测 `http://127.0.0.1:18789/health`，失败自动回滚。

如果服务器不是用这个脚本装的（手工运行、Docker、PM2 等），直接用新二进制替换旧文件、注入上述环境变量后重启即可。数据库表会在启动时自动迁移，旧数据不会丢。

### 3. 更新 Nginx（关键）

旧配置只放行了 `/v1/ws` 和 `/health`，不更新的话 `/account`、`/admin` 会一直 404。

把 `deploy/nginx-relay-location.conf` 的内容放进该域名的 HTTPS `server { }` 块，然后：

```bash
sudo nginx -t && sudo systemctl reload nginx
```

## 四、验收

```bash
curl -sS http://127.0.0.1:18789/health
# {"ok":true,"service":"homing-server","version":1,"auth_mode":"managed"}

curl -sS https://relay.example.com/v1/account/config
# 应看到 "auth_mode":"managed"、"free_weekly_minutes":1200，
# 且 email_enabled 或 github_enabled 至少一个为 true

curl -sS -o /dev/null -w "%{http_code}\n" https://relay.example.com/account   # 200
curl -sS -o /dev/null -w "%{http_code}\n" https://relay.example.com/admin     # 200

sudo journalctl -u homing-relay.service -n 40 --no-pager
```

浏览器检查：

1. 打开 `https://relay.example.com/account`，用一个真实邮箱收验证码注册，页面会显示 `hrk_...` 密钥和「本周手机远程操作时长 0 分钟 / 20 小时」。
2. 打开 `https://relay.example.com/admin`，用管理员令牌登录，能看到刚注册的账号、在线设备和「本周远程时长」一列。

## 五、回滚

```bash
ls -la /opt/homing-relay/releases/
sudo ln -sfn /opt/homing-relay/releases/<旧版本目录> /opt/homing-relay/current
sudo systemctl restart homing-relay.service
```

只想临时关闭注册/密钥校验：把 `HOMING_AUTH_MODE` 改为 `open` 后重启。

## 六、安全提示

- 管理员令牌错误 10 次会锁定该 IP 15 分钟；同一 IP 每小时最多发送 10 封验证码邮件。
- 不要在云防火墙开放 18789 端口。
- 部署完成后，把以下信息私下交给项目负责人：`/v1/account/config` 的返回结果、已启用的登录方式、管理员令牌（只走私密渠道，不要发群或提交到 Git）。
