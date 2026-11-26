package server

import (
	"net/http"
)

func (s *Server) serveAccountPortalHTML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(accountPortalHTML))
}

func (s *Server) serveAdminPortalHTML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(adminPortalHTML))
}

const accountPortalHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Homing 官方中继 · 账号与密钥中心</title>
<style>
  :root {
    --bg: #0b101b; --card: #131b2e; --border: #23304d; --text: #f1f5f9;
    --muted: #94a3b8; --brand: #2dd4bf; --brand-ink: #042f2e; --danger: #f87171; --amber: #fbbf24;
  }
  * { box-sizing: border-box; }
  body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "PingFang SC", "Segoe UI", sans-serif; background: var(--bg); color: var(--text); line-height: 1.55; }
  .wrap { max-width: 760px; margin: 0 auto; padding: 36px 18px 64px; }
  .hero { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 22px; }
  .hero h1 { font-size: 22px; margin: 0; }
  .sub { color: var(--muted); font-size: 13.5px; margin-top: 4px; }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: 14px; padding: 20px; margin-bottom: 16px; }
  .card h2 { font-size: 16px; margin: 0 0 12px; display: flex; align-items: center; gap: 8px; }
  .badge { display: inline-block; padding: 2px 9px; border-radius: 999px; font-size: 12px; font-weight: 600; background: rgba(45,212,191,.15); color: var(--brand); }
  .badge.sponsor { background: rgba(251,191,36,.18); color: var(--amber); }
  .row { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; margin-top: 10px; }
  input { background: #0d1424; border: 1px solid var(--border); color: var(--text); border-radius: 10px; padding: 10px 12px; font-size: 14px; outline: none; flex: 1; min-width: 180px; }
  input:focus { border-color: var(--brand); }
  button, a.btn { cursor: pointer; border: 1px solid transparent; border-radius: 10px; padding: 10px 16px; font-size: 13.5px; font-weight: 600; text-decoration: none; display: inline-flex; align-items: center; justify-content: center; gap: 6px; }
  .btn-primary { background: var(--brand); color: var(--brand-ink); }
  .btn-ghost { background: transparent; border-color: var(--border); color: var(--text); }
  .btn-danger { background: rgba(248,113,113,.14); color: var(--danger); border-color: rgba(248,113,113,.3); padding: 6px 12px; font-size: 12.5px; }
  .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .key-box { background: #090e1a; border: 1px dashed var(--brand); border-radius: 10px; padding: 12px 14px; margin: 12px 0; word-break: break-all; }
  .list-item { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 10px 0; border-bottom: 1px solid rgba(255,255,255,.06); font-size: 13.5px; }
  .list-item:last-child { border-bottom: 0; }
  .bar { height: 8px; background: #090e1a; border-radius: 999px; overflow: hidden; margin-top: 8px; }
  .bar > i { display: block; height: 100%; background: var(--brand); }
  .msg { padding: 10px 14px; border-radius: 10px; font-size: 13px; margin-bottom: 14px; display: none; }
  .msg.err { display: block; background: rgba(248,113,113,.14); color: #fca5a5; border: 1px solid rgba(248,113,113,.35); }
  .msg.ok { display: block; background: rgba(45,212,191,.14); color: #5eead4; border: 1px solid rgba(45,212,191,.35); }
  .divider { text-align: center; color: var(--muted); font-size: 12px; margin: 16px 0; }
</style>
</head>
<body>
<div class="wrap">
  <div class="hero">
    <div>
      <h1>Homing 官方中继 · 账号中心</h1>
      <div class="sub">端到端零知识加密转发（Ed25519 + NaCl Box）· 手机端扫码即用免注册</div>
    </div>
    <button id="logoutBtn" class="btn-ghost" style="display:none">退出登录</button>
  </div>
  <div id="alertBox" class="msg"></div>

  <div id="loginView" class="card" style="display:none">
    <h2>注册 / 登录官方中继</h2>
    <div id="regModeHint" class="sub" style="margin-bottom:12px"></div>
    <div id="inviteRow" class="row" style="display:none;margin-bottom:10px">
      <input id="inviteInput" placeholder="邀请码或赞助兑换码（如 HOMING-XXXX-XXXX）">
    </div>
    <div id="emailSection">
      <div class="row">
        <input id="emailInput" type="email" placeholder="输入邮箱（国内/海外邮箱均可）">
        <button id="sendCodeBtn" class="btn-ghost">发送验证码</button>
      </div>
      <div class="row">
        <input id="codeInput" maxlength="6" placeholder="6 位邮箱验证码">
        <button id="verifyBtn" class="btn-primary">验证并登录</button>
      </div>
    </div>
    <div id="ghSection" style="display:none">
      <div class="divider">— 或使用开发者账号一键登录 —</div>
      <a id="ghBtn" class="btn btn-ghost" style="width:100%" href="/v1/account/github/start">使用 GitHub 登录 / 注册</a>
    </div>
  </div>

  <div id="dashView" style="display:none">
    <div id="newKeyBanner" class="card" style="display:none;border-color:var(--brand)">
      <h2>请保存你的新中继密钥（Relay Key）</h2>
      <div class="sub">出于安全考虑，完整密钥仅显示这一次。复制后粘贴到电脑端 Homing 控制台「连接 › 云中转设置」即可：</div>
      <div id="newKeyValue" class="key-box mono"></div>
      <button id="copyNewKeyBtn" class="btn-primary">复制 Relay Key</button>
    </div>

    <div class="card">
      <h2>账号与今日配额 <span id="tierBadge" class="badge"></span></h2>
      <div id="accMeta" class="sub"></div>
      <div style="margin-top:12px;font-size:13px" id="quotaText"></div>
      <div class="bar"><i id="quotaBar" style="width:0%"></i></div>
      <div style="margin-top:12px;font-size:13px" id="weeklyText"></div>
      <div class="bar" id="weeklyBarWrap"><i id="weeklyBar" style="width:0%"></i></div>
      <div class="row" style="margin-top:14px">
        <span class="sub">连接地址：</span>
        <code id="wsUrlText" class="mono" style="font-size:13px;color:var(--brand)"></code>
        <button id="copyWsBtn" class="btn-ghost" style="padding:5px 10px;font-size:12px">复制地址</button>
      </div>
    </div>

    <div class="card">
      <h2>电脑端中继密钥（Relay Key）</h2>
      <div class="sub">仅需在电脑端填写一次；手机通过电脑生成的二维码配对后自动获得授权，无需填写密钥。</div>
      <div class="row">
        <input id="keyLabelInput" placeholder="备注名称（例如：办公室 MacBook）">
        <button id="createKeyBtn" class="btn-primary">+ 生成新密钥</button>
      </div>
      <div id="keyList" style="margin-top:12px"></div>
    </div>

    <div class="card">
      <h2>已绑定设备</h2>
      <div id="deviceList" class="sub">暂无已连接设备</div>
    </div>

    <div class="card">
      <h2>兑换赞助提额码 ❤️</h2>
      <div class="sub">打赏支持公网中继带宽后获得的兑换码，可在此升级为 Sponsor 额度并点亮鸣谢徽章：</div>
      <div class="row">
        <input id="redeemInput" class="mono" placeholder="HOMING-XXXX-XXXX">
        <button id="redeemBtn" class="btn-ghost">立即兑换</button>
      </div>
    </div>
  </div>
</div>

<script>
const $ = (id) => document.getElementById(id);
function showMsg(text, ok=false) {
  const el = $('alertBox');
  el.textContent = text;
  el.className = 'msg ' + (ok ? 'ok' : 'err');
}
function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1048576).toFixed(1) + ' MB';
}
function fmtMinutes(m) {
  m = m || 0;
  if (m < 60) return m + ' 分钟';
  const h = Math.floor(m / 60), r = m % 60;
  return r ? (h + ' 小时 ' + r + ' 分') : (h + ' 小时');
}
async function api(path, method='GET', body) {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body ? {'Content-Type': 'application/json'} : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok || data.ok === false) throw new Error(data.error || ('HTTP ' + res.status));
  return data;
}
function revealNewKey(k) {
  if (!k) return;
  $('newKeyValue').textContent = k;
  $('newKeyBanner').style.display = 'block';
  $('copyNewKeyBtn').onclick = () => navigator.clipboard.writeText(k).then(() => showMsg('已复制 Relay Key', true));
}
async function loadPortal() {
  const params = new URLSearchParams(location.search);
  if (params.get('error')) showMsg('登录未完成：' + params.get('error'));
  if (location.hash.startsWith('#new_key=')) {
    revealNewKey(decodeURIComponent(location.hash.slice(9)));
    history.replaceState(null, '', '/account');
  }
  const cfg = await api('/v1/account/config');
  if (cfg.auth_mode === 'open') {
    showMsg('当前中继运行在自建免注册模式（AUTH_MODE=open），直接填写 ' + cfg.ws_url + ' 即可使用，无需密钥。', true);
  }
  try {
    const me = await api('/v1/account/me');
    $('loginView').style.display = 'none';
    $('dashView').style.display = 'block';
    $('logoutBtn').style.display = 'inline-flex';
    const acc = me.account;
    const lim = me.limits;
    $('tierBadge').textContent = acc.tier === 'sponsor' ? 'Sponsor 赞助者' : 'Free 基础版';
    $('tierBadge').className = 'badge' + (acc.tier === 'sponsor' ? ' sponsor' : '');
    $('accMeta').textContent = [acc.display_name, acc.email, acc.github_login ? ('GitHub @' + acc.github_login) : ''].filter(Boolean).join(' · ');
    const used = me.traffic.bytes_relayed || 0;
    const pct = Math.min(100, Math.round((used / Math.max(1, lim.daily_quota_bytes)) * 100));
    $('quotaText').textContent = '今日已转发 ' + fmtBytes(used) + ' / ' + fmtBytes(lim.daily_quota_bytes) + '（' + (me.traffic.msg_count || 0) + ' 条加密封套 · 单包上限 ' + fmtBytes(lim.max_msg_bytes) + ' · 电脑上限 ' + lim.max_hosts + ' 台 · 移动设备上限 ' + lim.max_devices + ' 台）';
    $('quotaBar').style.width = pct + '%';
    const wk = me.weekly || {};
    const wkLimit = wk.limit_minutes || 0;
    const wkUsed = wk.active_minutes || 0;
    const resetText = wk.resets_at ? new Date(wk.resets_at).toLocaleString() : '';
    if (wkLimit > 0) {
      $('weeklyText').textContent = '本周手机远程操作时长 ' + fmtMinutes(wkUsed) + ' / ' + fmtMinutes(wkLimit) + '（只统计手机主动发出请求的分钟数；电脑推送与后台保活不计时；' + resetText + ' 重置）';
      $('weeklyBar').style.width = Math.min(100, Math.round(wkUsed / wkLimit * 100)) + '%';
      $('weeklyBarWrap').style.display = 'block';
    } else {
      $('weeklyText').textContent = '本周手机远程操作时长 ' + fmtMinutes(wkUsed) + '（当前账号不限时）';
      $('weeklyBarWrap').style.display = 'none';
    }
    $('wsUrlText').textContent = me.ws_url;
    $('copyWsBtn').onclick = () => navigator.clipboard.writeText(me.ws_url).then(() => showMsg('已复制 WebSocket 地址', true));

    const kl = $('keyList');
    kl.innerHTML = '';
    (me.keys || []).forEach(k => {
      const row = document.createElement('div');
      row.className = 'list-item';
      const info = document.createElement('div');
      info.innerHTML = '<strong></strong> <span class="mono" style="color:var(--muted);margin-left:8px"></span>';
      info.querySelector('strong').textContent = k.label;
      info.querySelector('span').textContent = k.key_prefix;
      const del = document.createElement('button');
      del.className = 'btn-danger';
      del.textContent = '吊销';
      del.onclick = async () => { await api('/v1/account/keys/' + encodeURIComponent(k.id), 'DELETE'); loadPortal(); };
      row.append(info, del);
      kl.appendChild(row);
    });

    const dl = $('deviceList');
    if (!me.devices || me.devices.length === 0) {
      dl.textContent = '暂无已绑定设备；在电脑端配置 Relay Key 连接后将自动显示。';
    } else {
      dl.innerHTML = '';
      me.devices.forEach(d => {
        const row = document.createElement('div');
        row.className = 'list-item';
        const left = document.createElement('span');
        left.textContent = (d.role === 'host' ? '💻 电脑端 ' : '📱 手机端 ') + (d.label || d.device_id.slice(0, 12));
        const right = document.createElement('span');
        right.textContent = d.online ? '● 在线' : '离线';
        right.style.color = d.online ? '#2dd4bf' : '#94a3b8';
        row.append(left, right);
        dl.appendChild(row);
      });
    }
  } catch {
    $('dashView').style.display = 'none';
    $('logoutBtn').style.display = 'none';
    $('loginView').style.display = 'block';
    $('emailSection').style.display = cfg.email_enabled ? 'block' : 'none';
    $('ghSection').style.display = cfg.github_enabled ? 'block' : 'none';
    $('inviteRow').style.display = cfg.registration_mode === 'invite' ? 'flex' : 'none';
    $('regModeHint').textContent = cfg.registration_mode === 'invite'
      ? '当前服务器采用邀请码/赞助码注册，新用户请先填写邀请码；老用户可直接登录。'
      : cfg.registration_mode === 'closed'
        ? '当前仅限已有账号登录。'
        : '支持邮箱验证码或 GitHub 一键注册，注册后立即自动生成电脑端 Relay Key。';
    $('ghBtn').onclick = (e) => {
      const inv = $('inviteInput').value.trim();
      if (inv) {
        e.preventDefault();
        location.href = '/v1/account/github/start?invite=' + encodeURIComponent(inv);
      }
    };
  }
}
$('sendCodeBtn').onclick = async () => {
  try {
    await api('/v1/account/email/send-code', 'POST', { email: $('emailInput').value, invite_code: $('inviteInput').value });
    showMsg('验证码已发送，请查收邮箱（10 分钟内有效）', true);
  } catch (e) { showMsg(e.message); }
};
$('verifyBtn').onclick = async () => {
  try {
    const res = await api('/v1/account/email/verify', 'POST', {
      email: $('emailInput').value,
      code: $('codeInput').value,
      invite_code: $('inviteInput').value,
    });
    if (res.initial_key) revealNewKey(res.initial_key);
    await loadPortal();
  } catch (e) { showMsg(e.message); }
};
$('createKeyBtn').onclick = async () => {
  try {
    const res = await api('/v1/account/keys', 'POST', { label: $('keyLabelInput').value });
    $('keyLabelInput').value = '';
    revealNewKey(res.relay_key);
    await loadPortal();
  } catch (e) { showMsg(e.message); }
};
$('redeemBtn').onclick = async () => {
  try {
    await api('/v1/account/redeem', 'POST', { code: $('redeemInput').value });
    $('redeemInput').value = '';
    showMsg('兑换成功！已升级账号额度', true);
    await loadPortal();
  } catch (e) { showMsg(e.message); }
};
$('logoutBtn').onclick = async () => {
  await api('/v1/account/logout', 'POST');
  location.reload();
};
loadPortal();
</script>
</body>
</html>
`

const adminPortalHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Homing Relay Admin · 官方中继管理后台</title>
<style>
  :root {
    --bg: #090d16; --card: #111827; --border: #1f2937; --text: #f3f4f6;
    --muted: #9ca3af; --brand: #2dd4bf; --brand-ink: #042f2e; --danger: #f87171; --amber: #fbbf24;
  }
  * { box-sizing: border-box; }
  body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "PingFang SC", sans-serif; background: var(--bg); color: var(--text); }
  .wrap { max-width: 1080px; margin: 0 auto; padding: 28px 18px 64px; }
  .top { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
  h1 { font-size: 20px; margin: 0; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; margin-bottom: 18px; }
  .stat { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 14px 16px; }
  .stat .k { color: var(--muted); font-size: 12px; }
  .stat .v { font-size: 22px; font-weight: 700; margin-top: 4px; }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: 14px; padding: 18px; margin-bottom: 16px; }
  .card h2 { font-size: 15.5px; margin: 0 0 12px; }
  .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
  input, select { background: #0b101b; border: 1px solid var(--border); color: var(--text); border-radius: 8px; padding: 8px 11px; font-size: 13.5px; }
  button { cursor: pointer; border: 1px solid var(--border); background: #1f2937; color: var(--text); border-radius: 8px; padding: 8px 13px; font-size: 13px; font-weight: 600; }
  button.primary { background: var(--brand); color: var(--brand-ink); border-color: transparent; }
  button.danger { background: rgba(248,113,113,.15); color: var(--danger); border-color: rgba(248,113,113,.3); }
  table { width: 100%; border-collapse: collapse; font-size: 13px; margin-top: 12px; }
  th, td { text-align: left; padding: 9px 8px; border-bottom: 1px solid var(--border); }
  th { color: var(--muted); font-weight: 500; }
  .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .pill { padding: 2px 8px; border-radius: 999px; font-size: 11.5px; background: rgba(45,212,191,.14); color: var(--brand); }
  .pill.sponsor { background: rgba(251,191,36,.18); color: var(--amber); }
  .pill.banned { background: rgba(248,113,113,.18); color: var(--danger); }
</style>
</head>
<body>
<div class="wrap">
  <div class="top">
    <h1>Homing 官方中继 · 管理后台</h1>
    <button id="logoutBtn" style="display:none">退出管理会话</button>
  </div>
  <div id="msgBox" style="color:var(--brand);font-size:13px;margin-bottom:12px"></div>

  <div id="loginCard" class="card" style="max-width:440px;display:none">
    <h2>输入管理员令牌（HOMING_ADMIN_TOKEN）</h2>
    <div class="row">
      <input id="tokenInput" type="password" placeholder="Admin Token" style="flex:1">
      <button id="loginBtn" class="primary">登录</button>
    </div>
  </div>

  <div id="mainView" style="display:none">
    <div class="grid" id="statsGrid"></div>

    <div class="card">
      <h2>注册策略控制</h2>
      <div class="row">
        <select id="regModeSelect">
          <option value="open">公开注册（邮箱验证码 / GitHub）</option>
          <option value="invite">仅限邀请码 / 赞助兑换码注册</option>
          <option value="closed">关闭新用户注册（仅老用户登录）</option>
        </select>
        <button id="saveRegBtn" class="primary">保存策略</button>
      </div>
    </div>

    <div class="card">
      <h2>生成赞助兑换码 / 邀请码（回馈打赏用户）</h2>
      <div class="row">
        <input id="codeCount" type="number" value="5" min="1" max="50" style="width:90px" title="生成数量">
        <select id="codeTier">
          <option value="sponsor">升级为 Sponsor（8 台电脑 / 2 GB 日流量）</option>
          <option value="free">仅作为注册邀请码（Free 基础额度）</option>
        </select>
        <input id="codeUses" type="number" value="1" min="1" max="1000" style="width:110px" placeholder="可用次数">
        <input id="codeNote" placeholder="备注（如：微信打赏用户批次）" style="flex:1">
        <button id="genCodeBtn" class="primary">批量生成</button>
      </div>
      <div id="codeOutput" class="mono" style="margin-top:10px;font-size:12.5px;color:var(--brand);white-space:pre-wrap"></div>
      <table>
        <thead><tr><th>兑换码</th><th>等级</th><th>使用次数</th><th>备注</th></tr></thead>
        <tbody id="codesTable"></tbody>
      </table>
    </div>

    <div class="card">
      <h2>注册用户与流量管控</h2>
      <div class="row">
        <input id="searchAcc" placeholder="搜索邮箱 / GitHub / ID…" style="flex:1">
        <button id="searchBtn">刷新 / 搜索</button>
      </div>
      <table>
        <thead><tr><th>用户</th><th>等级/状态</th><th>电脑/手机</th><th>今日流量</th><th>本周远程时长</th><th>操作</th></tr></thead>
        <tbody id="accTable"></tbody>
      </table>
    </div>
  </div>
</div>

<script>
const $ = (id) => document.getElementById(id);
function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1048576).toFixed(1) + ' MB';
}
function fmtMinutes(m) {
  m = m || 0;
  if (m < 60) return m + ' 分钟';
  const h = Math.floor(m / 60), r = m % 60;
  return r ? (h + ' 小时 ' + r + ' 分') : (h + ' 小时');
}
async function api(path, method='GET', body) {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body ? {'Content-Type':'application/json'} : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok || data.ok === false) throw new Error(data.error || ('HTTP ' + res.status));
  return data;
}
async function loadAdmin() {
  try {
    const ov = await api('/v1/admin/overview');
    $('loginCard').style.display = 'none';
    $('mainView').style.display = 'block';
    $('logoutBtn').style.display = 'inline-block';
    $('regModeSelect').value = ov.registration_mode;
    const st = ov.stats;
    const items = [
      ['鉴权模式', ov.auth_mode.toUpperCase()],
      ['当前在线设备', ov.online_devices + ' 台 (' + ov.online_sockets + ' 连接)'],
      ['注册总账号 / 赞助者', st.total_accounts + ' / ' + st.sponsor_accounts],
      ['今日转发封套', st.today_messages + ' 条'],
      ['今日出网流量', fmtBytes(st.today_bytes)],
      ['离线待收队列', st.queued_envelopes + ' 条'],
      ['本周活跃账号 / 远程时长', st.week_active_accounts + ' / ' + fmtMinutes(st.week_active_minutes)],
      ['Free 每周时长上限', ov.free_weekly_minutes > 0 ? fmtMinutes(ov.free_weekly_minutes) : '不限'],
    ];
    $('statsGrid').innerHTML = items.map(([k, v]) => '<div class="stat"><div class="k">' + k + '</div><div class="v">' + v + '</div></div>').join('');
    await Promise.all([loadAccounts(), loadCodes()]);
  } catch {
    $('mainView').style.display = 'none';
    $('loginCard').style.display = 'block';
  }
}
async function loadAccounts() {
  const data = await api('/v1/admin/accounts?q=' + encodeURIComponent($('searchAcc').value));
  const tb = $('accTable');
  tb.innerHTML = '';
  (data.accounts || []).forEach(a => {
    const tr = document.createElement('tr');
    const who = document.createElement('td');
    who.textContent = [a.display_name, a.email, a.github_login ? ('@' + a.github_login) : ''].filter(Boolean).join(' · ');
    const st = document.createElement('td');
    st.innerHTML = '<span class="pill ' + (a.status === 'banned' ? 'banned' : a.tier === 'sponsor' ? 'sponsor' : '') + '">' + (a.status === 'banned' ? '已封禁' : a.tier) + '</span>';
    const devs = document.createElement('td');
    devs.textContent = a.host_count + '/' + a.limits.max_hosts + ' 电脑 · ' + a.client_count + '/' + a.limits.max_devices + ' 手机';
    const tf = document.createElement('td');
    tf.textContent = fmtBytes(a.today_traffic.bytes_relayed) + ' (' + a.today_traffic.msg_count + ' 条)';
    const wk = document.createElement('td');
    const wkInfo = a.weekly || {};
    wk.textContent = fmtMinutes(wkInfo.active_minutes) + ' / ' + (wkInfo.limit_minutes > 0 ? fmtMinutes(wkInfo.limit_minutes) : '不限');
    if (wkInfo.limit_minutes > 0 && wkInfo.active_minutes >= wkInfo.limit_minutes) wk.style.color = 'var(--danger)';
    const act = document.createElement('td');
    const tierBtn = document.createElement('button');
    tierBtn.textContent = a.tier === 'sponsor' ? '设为 Free' : '设为 Sponsor';
    tierBtn.style.marginRight = '6px';
    tierBtn.onclick = async () => {
      await api('/v1/admin/accounts/' + encodeURIComponent(a.id), 'PATCH', {
        tier: a.tier === 'sponsor' ? 'free' : 'sponsor',
        sponsor_badge: a.tier !== 'sponsor',
      });
      loadAdmin();
    };
    const banBtn = document.createElement('button');
    banBtn.className = a.status === 'banned' ? '' : 'danger';
    banBtn.textContent = a.status === 'banned' ? '解封' : '封禁';
    banBtn.onclick = async () => {
      await api('/v1/admin/accounts/' + encodeURIComponent(a.id), 'PATCH', {
        status: a.status === 'banned' ? 'active' : 'banned',
      });
      loadAdmin();
    };
    act.append(tierBtn, banBtn);
    tr.append(who, st, devs, tf, wk, act);
    tb.appendChild(tr);
  });
}
async function loadCodes() {
  const data = await api('/v1/admin/redeem-codes');
  const tb = $('codesTable');
  tb.innerHTML = '';
  (data.codes || []).slice(0, 20).forEach(c => {
    const tr = document.createElement('tr');
    ['code', 'tier'].forEach(k => {
      const td = document.createElement('td');
      if (k === 'code') td.className = 'mono';
      td.textContent = c[k];
      tr.appendChild(td);
    });
    const u = document.createElement('td');
    u.textContent = c.used_count + ' / ' + c.max_uses;
    const n = document.createElement('td');
    n.textContent = c.note || '-';
    tr.append(u, n);
    tb.appendChild(tr);
  });
}
$('loginBtn').onclick = async () => {
  try {
    await api('/v1/admin/login', 'POST', { token: $('tokenInput').value });
    $('msgBox').textContent = '';
    loadAdmin();
  } catch (e) { $('msgBox').textContent = e.message; }
};
$('logoutBtn').onclick = async () => { await api('/v1/admin/logout', 'POST'); location.reload(); };
$('saveRegBtn').onclick = async () => {
  await api('/v1/admin/settings', 'PUT', { registration_mode: $('regModeSelect').value });
  $('msgBox').textContent = '已更新注册策略';
};
$('genCodeBtn').onclick = async () => {
  const res = await api('/v1/admin/redeem-codes', 'POST', {
    count: Number($('codeCount').value || 1),
    tier: $('codeTier').value,
    max_uses: Number($('codeUses').value || 1),
    note: $('codeNote').value,
  });
  $('codeOutput').textContent = '新生成兑换码（点击复制发给用户）：\n' + (res.codes || []).map(c => c.code).join('\n');
  loadCodes();
};
$('searchBtn').onclick = () => loadAccounts();
loadAdmin();
</script>
</body>
</html>
`
