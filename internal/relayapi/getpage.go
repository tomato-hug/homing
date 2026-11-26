package relayapi

import (
	"net/http"

	"github.com/tomato-hug/homing/internal/api"
)

// GET /get is what the phone's camera opens after scanning the console QR.
// One code covers both cases: no app yet -> download the APK (local build or
// GitHub Release fallback); app installed -> hand the pairing link (kept in the
// URL fragment, so it never reaches a server log) to Homing.
const getPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<meta name="color-scheme" content="dark">
<title>Homing · 安装与配对 / Install &amp; Pair</title>
<style>
:root{--bg:#0b1220;--card:#111a2e;--line:#1f2a44;--text:#f8fafc;--body:#cbd5e1;--muted:#94a3b8;--brand:#2dd4bf;--ink:#042f2e;--amber:#fbbf24}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;background:var(--bg);color:var(--text);font:15px/1.6 -apple-system,BlinkMacSystemFont,"PingFang SC","Noto Sans SC","Segoe UI",sans-serif;padding:24px 18px calc(28px + env(safe-area-inset-bottom))}
main{max-width:440px;margin:0 auto}
.top{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:20px}
.brand{display:flex;align-items:center;gap:12px}
.brand b{font-size:22px;letter-spacing:.2px}
.tile{width:44px;height:44px;border-radius:12px;background:#0f172a;display:grid;place-items:center;border:1px solid var(--line)}
.lang{background:var(--card);border:1px solid var(--line);color:var(--body);border-radius:999px;padding:5px 12px;font-size:12.5px;cursor:pointer}
h1{font-size:20px;margin:0 0 6px;line-height:1.35}
p{margin:0;color:var(--muted);font-size:14px}
.banner{background:rgba(251,191,36,.1);border:1px solid rgba(251,191,36,.35);color:#fde68a;border-radius:14px;padding:12px 14px;margin-top:14px;font-size:13.5px}
.step{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:18px;margin-top:14px}
.num{display:inline-grid;place-items:center;width:24px;height:24px;border-radius:50%;background:#134e4a;color:var(--brand);font-size:13px;font-weight:700;margin-right:8px;flex:none}
.title{font-weight:600;font-size:16px;display:flex;align-items:center;margin-bottom:8px}
a.btn{display:flex;justify-content:center;align-items:center;min-height:50px;border-radius:13px;margin-top:12px;font-weight:600;font-size:15.5px;text-decoration:none}
a.primary{background:var(--brand);color:var(--ink)}
a.ghost{border:1px solid var(--line);color:var(--text);background:rgba(255,255,255,.02)}
a.off{opacity:.45;pointer-events:none}
.tip{margin-top:10px;font-size:12.5px;color:var(--muted)}
.code{font:600 26px/1.2 ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:6px;color:var(--text);margin-top:12px;padding:10px 12px;background:#0f172a;border:1px solid var(--line);border-radius:10px;text-align:center}
.foot{margin-top:18px;font-size:12.5px;color:#94a3b8}
</style>
</head>
<body>
<main>
  <div class="top">
    <div class="brand">
      <span class="tile"><svg width="32" height="32" viewBox="0 0 120 120" aria-hidden="true"><g transform="translate(4 6)"><path fill="#E2E8F0" d="M62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C82 44 70 42 62 48 Z"/><path fill="#E2E8F0" d="M66 38 C65 28 77 24 84 29 C86 31 87 34 87 37 L96 41 L88 43 C86 47 80 50 73 50 C68 49 66 44 66 38 Z"/><circle cx="78" cy="34" r="2.4" fill="#0F172A"/><path fill="#2DD4BF" d="M63 44 C70 50 80 51 86 49 L87 55 C79 57 68 55 61 50 Z"/><path fill="#94A3B8" d="M36 71 C50 61 66 61 76 68 C64 80 48 82 36 71 Z"/></g></svg></span>
      <b>Homing</b>
    </div>
    <button id="lang" class="lang" type="button">English</button>
  </div>
  <h1 id="h1">在手机上管理电脑里的 Agent</h1>
  <p id="lead">两步完成：装好 App，再回到这里点「打开 Homing 配对」。</p>
  <div id="ios-banner" class="banner" hidden></div>

  <section class="step" id="step1">
    <div class="title"><span class="num">1</span><span id="s1-title">安装 Homing 安卓版</span></div>
    <p id="s1-desc">优先从这台电脑直传，若本机未含安装包则自动跳转 GitHub Release 下载。已安装可跳过。</p>
    <a id="dl" class="btn ghost" href="/apk" download="homing.apk">下载安装包</a>
    <div id="s1-tip" class="tip">若系统提示「未知来源应用」，请允许浏览器完成本次安装。</div>
  </section>

  <section class="step">
    <div class="title"><span class="num">2</span><span id="s2-title">打开 Homing 完成配对</span></div>
    <p id="hint">点下面的按钮会跳到 Homing；在电脑上核对四个校验词一致后点击接受即可。</p>
    <a id="open" class="btn primary off" href="#">打开 Homing 配对</a>
    <div id="code" class="code" hidden></div>
  </section>

  <div id="foot" class="foot">配对链接仅在 5 分钟内有效且端到端加密。若已过期，请在电脑控制台点「换一个」。</div>
</main>
<script>
(function(){
  var params = new URLSearchParams(location.hash.replace(/^#/, ""));
  var link = params.get("p") || "";
  var code = params.get("c") || "";
  var hasLink = link.indexOf("homing://pair?") === 0;
  var hasCode = /^[0-9]{6}$/.test(code);
  var isIOS = /iPhone|iPad|iPod/i.test(navigator.userAgent || "");
  var lang = /^zh/i.test(navigator.language || "zh") ? "zh" : "en";

  var copy = {
    zh: {
      toggle: "English",
      h1: "在手机上管理电脑里的 Agent",
      lead: "两步完成：装好 App，再回到这里点「打开 Homing 配对」。",
      ios: "检测到 iOS 设备：Homing 原生客户端目前仅提供 Android 版（APK）。请使用 Android 手机扫码安装。",
      s1t: "安装 Homing 安卓版",
      s1d: "优先从这台电脑直传，若本机未含安装包则自动跳转 GitHub Release 下载。已安装可跳过。",
      dl: "下载安装包 (.apk)",
      s1tip: "若系统提示「未知来源应用」，请允许浏览器完成本次安装。",
      s2t: "打开 Homing 完成配对",
      open: "打开 Homing 配对",
      hintOk: "点下面的按钮会跳到 Homing；在电脑上核对四个校验词一致后点击接受即可。",
      hintCode: "点按钮跳到 Homing；若未自动跳转，可在 App 内选「配对码」输入下方 6 位数字。",
      hintMissing: "当前链接不含配对信息，请在电脑的 Homing 控制台重新扫码。",
      foot: "配对链接仅在 5 分钟内有效且端到端加密。若已过期，请在电脑控制台点「换一个」。"
    },
    en: {
      toggle: "简体中文",
      h1: "Manage Desktop Agents from Your Phone",
      lead: "Two quick steps: install the Android app, then tap 'Open Homing to Pair' below.",
      ios: "iOS detected: the native Homing app is currently available for Android (.apk) only. Please scan with an Android device.",
      s1t: "Install Homing for Android",
      s1d: "Downloads directly from this computer or falls back to the official GitHub Release. Skip if already installed.",
      dl: "Download APK",
      s1tip: "If prompted by Android, allow your browser to install unknown apps for this package.",
      s2t: "Open Homing & Pair",
      open: "Open Homing to Pair",
      hintOk: "Tap below to launch Homing, then verify the 4 safety words on your computer.",
      hintCode: "Tap below to launch Homing. If it doesn't open, enter this 6-digit code in the app.",
      hintMissing: "No pairing payload found in this link. Please rescan the QR code on your computer.",
      foot: "Pairing links expire in 5 minutes and use end-to-end encryption. Click 'Refresh' on your computer if expired."
    }
  };

  var openEl = document.getElementById("open");
  var codeEl = document.getElementById("code");
  if (hasLink) {
    openEl.href = link;
    openEl.classList.remove("off");
  }
  if (hasCode) {
    codeEl.hidden = false;
    codeEl.textContent = code.slice(0, 3) + " " + code.slice(3);
  }

  function render() {
    var t = copy[lang];
    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
    document.getElementById("lang").textContent = t.toggle;
    document.getElementById("h1").textContent = t.h1;
    document.getElementById("lead").textContent = t.lead;
    document.getElementById("s1-title").textContent = t.s1t;
    document.getElementById("s1-desc").textContent = t.s1d;
    document.getElementById("dl").textContent = t.dl;
    document.getElementById("s1-tip").textContent = t.s1tip;
    document.getElementById("s2-title").textContent = t.s2t;
    openEl.textContent = t.open;
    document.getElementById("foot").textContent = t.foot;
    var iosEl = document.getElementById("ios-banner");
    if (isIOS) {
      iosEl.hidden = false;
      iosEl.textContent = t.ios;
    }
    document.getElementById("hint").textContent = !hasLink ? t.hintMissing : (hasCode ? t.hintCode : t.hintOk);
  }

  document.getElementById("lang").addEventListener("click", function() {
    lang = lang === "zh" ? "en" : "zh";
    render();
  });
  render();
})();
</script>
</body>
</html>`

func serveGetPage(w http.ResponseWriter, r *http.Request) {
	if !isLocalOrLAN(r) {
		api.WriteError(w, http.StatusForbidden, "lan_only", "Forbidden")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(getPageHTML))
}
