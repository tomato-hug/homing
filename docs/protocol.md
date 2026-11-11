# Homing 协议 v1

## 角色与密钥

- 设备分两类：电脑（host，跑 homingd）和手机（client，跑 App）。
- 每台设备首次运行生成一对 Ed25519 身份密钥，私钥只存本机（Mac 钥匙串 / Android Keystore 包裹存储）。加密用 libsodium 从 Ed25519 转出的 X25519 密钥。
- device_id = base32(BLAKE2b-128(ed25519_pub))，小写无填充。指纹 fp = BLAKE2b-64(ed25519_pub) 的十六进制。

## 传输

- 云中转与自建服务器：`wss://<server>/v1/ws`；局域网直连：`ws://<电脑局域网 IP>:18787/v1/ws`（homingd 内置同一套路由，手机端代码不分模式）。
- 连上后服务器发 `{t:"challenge", nonce}`，设备回 `{t:"hello", device_id, pub, ts, sig}`，sig = Ed25519 签名(nonce || ts)。服务器首次见到该 device_id 时记录公钥（device_id 由公钥派生，无法冒用）。
- 心跳 25 s；断线重连指数退避 1 s → 60 s。

## 配对

1. host 发 `{t:"pair.start"}` → 服务器回 `{pair_token(128 位随机), code6, expires_in:300}`。单次有效；code6 每个 IP 最多试 5 次。
2. 二维码内容：`homing://pair?s=<服务器地址>&t=<pair_token>&h=<host device_id>&fp=<host 指纹>`。
3. client 发 `{t:"pair.join", token 或 code6, pub, name, platform}` → 服务器转给 host `{t:"pair.request", client_id, pub, name, platform}`。
4. 扫码路径：client 拿到 host 公钥后核对 fp，不符立即中止。配对码路径：两端各显示 4 个词的校验短语 SAS = 取 BLAKE2b-64(host_pub || client_pub) 的前 44 位，按每 11 位一组得到 4 个下标，查 BIP39 简体中文词表（2048 词），由用户在电脑上确认一致。
5. host 发 `{t:"pair.accept", client_id}`（或 `pair.reject`）→ 服务器记录配对关系并把 host 公钥交给 client。
6. 撤销：任一端发 `{t:"pair.revoke", peer_id}`，服务器删除配对和该对之间的离线队列，双方删除对方公钥。

## 消息信封

`{v:1, t:"msg", id(ULID), from, to, ts, nonce(24 字节 base64), ct(base64)}`

- ct = crypto_box_easy(payload_json, nonce, 收件方 X25519 公钥, 发件方 X25519 私钥)。
- 服务器只在已配对的两端之间转发；收件方离线时入队（每台设备最多 500 条、保留 7 天），收件方回 `{t:"ack", id}` 后删除。
- 服务器数据库里不能出现任何明文：只有 device_id、公钥、时间、密文、推送令牌。

## 加密内容（payload）

- 请求：`{type:"rpc", rid, method, path, body}`，host 收到后在本机调用中继 HTTP 接口（`/api/...`），回 `{type:"rpc_result", rid, status, body}`。
- 事件：`{type:"event", name, data}`，name 与现有 SSE 事件一致（`message`、`reply_status`、`item`、`turn`、`session`、`dispatch`、`collab_result`）。
- 文件：大于 64 KB 的走分块，单文件上限 20 MB。`data` 是这一块的原始字节再做 base64，单块解码后不超过 64 KB，`seq` 从 0 连续递增；接收方必须校验顺序、总大小和 `fid`。

### 文件上传

手机发给电脑的每一块：

```json
{"type":"file.chunk","fid":"01...","seq":0,"last":false,
 "data":"<base64>","path":"/api/upload?event_id=123&name=a.png",
 "content_type":"image/png"}
```

- `path` 和 `content_type` 只在 `seq=0` 带；后续块只带 `fid`、`seq`、`last`、`data`。
- `path` 必须以 `/api/upload` 开头。
- 电脑收齐 `last=true` 后，调用本机上传接口，回：

```json
{"type":"file.result","fid":"01...","status":200,
 "body":{"ok":true,"path":"/tmp/homing-upload-a.png","name":"a.png"}}
```

失败同样是 `file.result`，`status` 为 HTTP 状态，`body.error` 为稳定错误码。发送方在 90 秒内没收到结果就按超时处理；中途断线可整笔重传，不保留半成品。

### 文件下载

手机请求一个 `file.request`，路径只允许现有的 `/f/{capability}/{path...}` 能力链接：

```json
{"type":"file.request","fid":"02...","path":"/f/<token>/a.png"}
```

电脑按上面的 `file.chunk` 格式逐块回传，最后一块 `last=true`。找不到、路径不合规或文件超过 20 MB 时回 `file.result` 错误。手机必须按顺序拼接，不能把 `file.chunk` 当成普通事件交给 UI。

## 推送唤醒

- client 上报 `{t:"push.register", provider:"fcm"|"oppo"|"xiaomi"|"huawei"|"honor"|"vivo", token}`（这是服务器唯一能看到的明文身份信息）。
- 有消息进入离线队列时，服务器给该 client 发不带内容的唤醒推送（只写「有新消息」），同一设备 30 s 内合并为一次；App 被唤醒后连上来拉取、在本机解密再弹本地通知。
