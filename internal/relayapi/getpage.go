package relayapi

import (
	"net/http"

	"github.com/tomato-hug/homing/internal/api"
)

// GET /get is what the phone's camera opens after scanning the console QR.
// One code covers both cases: no app yet → download the APK from this
// computer; app installed → hand the pairing link (kept in the URL fragment,
// so it never reaches a server log) to Homing.
const getPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<meta name="color-scheme" content="dark">
<title>Homing · 安装与配对</title>
<style>
:root{--bg:#0b1220;--card:#111a2e;--line:#1f2a44;--text:#e2e8f0;--muted:#94a3b8;--brand:#2dd4bf;--ink:#042f2e}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;background:var(--bg);color:var(--text);font:16px/1.6 -apple-system,BlinkMacSystemFont,"PingFang SC","Noto Sans SC",sans-serif;padding:28px 18px calc(28px + env(safe-area-inset-bottom))}
main{max-width:440px;margin:0 auto}
.brand{display:flex;align-items:center;gap:12px;margin-bottom:22px}
.brand b{font-size:24px;letter-spacing:.2px}
.tile{width:48px;height:48px;border-radius:13px;background:#0f172a;display:grid;place-items:center;border:1px solid var(--line)}
h1{font-size:21px;margin:0 0 6px}
p{margin:0;color:var(--muted);font-size:14px}
.step{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:18px;margin-top:14px}
.num{display:inline-grid;place-items:center;width:24px;height:24px;border-radius:50%;background:#134e4a;color:var(--brand);font-size:13px;font-weight:700;margin-right:8px}
.title{font-weight:600;font-size:16px;display:flex;align-items:center;margin-bottom:8px}
a.btn{display:flex;justify-content:center;align-items:center;min-height:52px;border-radius:14px;margin-top:12px;font-weight:600;font-size:16px;text-decoration:none}
a.primary{background:var(--brand);color:var(--ink)}
a.ghost{border:1px solid var(--line);color:var(--text)}
a.off{opacity:.45;pointer-events:none}
.code{font:600 26px/1.2 ui-monospace,Menlo,monospace;letter-spacing:6px;color:var(--text);margin-top:10px}
.foot{margin-top:18px;font-size:12px;color:#64748b}
</style>
</head>
<body>
<main>
  <div class="brand">
    <span class="tile"><svg width="34" height="34" viewBox="0 0 120 120" aria-hidden="true"><g transform="translate(4 6)"><path fill="#E2E8F0" d="M62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C82 44 70 42 62 48 Z"/><path fill="#E2E8F0" d="M66 38 C65 28 77 24 84 29 C86 31 87 34 87 37 L96 41 L88 43 C86 47 80 50 73 50 C68 49 66 44 66 38 Z"/><circle cx="78" cy="34" r="2.4" fill="#0F172A"/><path fill="#2DD4BF" d="M63 44 C70 50 80 51 86 49 L87 55 C79 57 68 55 61 50 Z"/><path fill="#94A3B8" d="M36 71 C50 61 66 61 76 68 C64 80 48 82 36 71 Z"/></g></svg></span>
    <b>Homing</b>
  </div>
  <h1>在手机上管理电脑里的 Agent</h1>
  <p>两步完成：装好 App，再回到这里点「打开 Homing 配对」。</p>

  <section class="step">
    <div class="title"><span class="num">1</span>安装 Homing 安卓版</div>
    <p>从这台电脑直接下载，手机和电脑需要在同一个 Wi‑Fi。装过可以跳过。</p>
    <a class="btn ghost" href="/apk" download="homing.apk">下载安装包</a>
  </section>

  <section class="step">
    <div class="title"><span class="num">2</span>打开 Homing 完成配对</div>
    <p id="hint">点下面的按钮会跳到 Homing；电脑上确认四个校验词后就连好了。</p>
    <a id="open" class="btn primary off" href="#">打开 Homing 配对</a>
    <div id="code" class="code" hidden></div>
  </section>

  <div class="foot">配对链接只在 5 分钟内有效。过期了就在电脑上点「换一个」。</div>
</main>
<script>
(function(){
  var params = new URLSearchParams(location.hash.replace(/^#/, ""));
  var link = params.get("p") || "";
  var code = params.get("c") || "";
  var open = document.getElementById("open");
  if (link.indexOf("homing://pair?") === 0) {
    open.href = link;
    open.classList.remove("off");
  } else {
    document.getElementById("hint").textContent = "这个链接里没有配对信息。请在电脑的 Homing 控制台重新扫码。";
  }
  if (/^[0-9]{6}$/.test(code)) {
    var el = document.getElementById("code");
    el.hidden = false;
    el.textContent = code;
    document.getElementById("hint").textContent = "点按钮跳到 Homing；如果没有跳转，在 App 里选「配对码」输入下面 6 位数字。";
  }
})();
</script>
</body>
</html>`

func serveGetPage(w http.ResponseWriter, r *http.Request) {
	if !isLocalOrLAN(r) {
		api.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(getPageHTML))
}
