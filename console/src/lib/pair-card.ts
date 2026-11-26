// 手机安装 + 配对卡片：首页、设备页共用。
//
// 二维码是 http://<电脑局域网地址>:18787/get#p=<homing://pair…>&c=<6 位码>：
// 手机相机扫 → 落地页（下载 APK / 打开 Homing 配对）；App 里扫 → 直接配对。
// 配对走端到端加密的 /api/cloud/pair/*，电脑上核对四个校验词后接受。

import QRCode from "qrcode";
import { h, clear } from "./dom";
import { icons } from "./icons";
import { endpoints, type CloudPairInvite, type CloudPairRequest, type ConnectionInfo } from "./api";
import { button, openExternalUrl, reportError, toast } from "./ui";

export interface PairCardOptions {
  /** 有手机在等核对校验词时 true，核对完 false */
  onWaiting?: (waiting: boolean) => void;
  onPaired?: (name: string) => void;
  /** 二维码边长（像素） */
  qrSize?: number;
}

export interface PairCard {
  el: HTMLElement;
  /** 生成新的二维码并开始等手机 */
  start: () => Promise<void>;
  dispose: () => void;
}

export function landingURL(invite: CloudPairInvite, connection: ConnectionInfo | null): string {
  const lan = connection?.lan_urls?.find((url) => !url.includes("127.0.0.1") && !url.includes("localhost"));
  if (!lan) return invite.uri;
  return `${lan.replace(/\/+$/, "")}/get#p=${encodeURIComponent(invite.uri)}&c=${invite.code6}`;
}

export function createPairCard(options: PairCardOptions = {}): PairCard {
  const el = h("div", { class: "pair-card" });
  const qrSize = options.qrSize ?? 220;
  let disposed = false;
  let invite: CloudPairInvite | null = null;
  let inviteAt = 0;
  let error = "";
  let requests: CloudPairRequest[] = [];
  let connection: ConnectionInfo | null = null;
  let timer: number | null = null;
  let ttlEl: HTMLElement | null = null;

  async function start() {
    if (!connection) connection = await endpoints.connection().catch(() => null);
    try {
      invite = await endpoints.cloudPairStart();
      inviteAt = Date.now();
      error = "";
      requests = [];
    } catch (err) {
      invite = null;
      error = (err as Error).message;
    }
    if (disposed) return;
    paint();
    if (!timer) timer = window.setInterval(() => void tick(), 2000);
  }

  async function tick() {
    if (disposed || !invite) return;
    const left = invite.expires_in - (Date.now() - inviteAt) / 1000;
    if (left <= 0 && requests.length === 0) {
      await start();
      return;
    }
    try {
      const payload = await endpoints.cloudPairRequests();
      const before = requests.map((item) => item.client_id).join(",");
      const after = payload.requests.map((item) => item.client_id).join(",");
      if (before !== after) {
        requests = payload.requests;
        options.onWaiting?.(requests.length > 0);
        paint();
      } else {
        paintTTL();
      }
    } catch {
      /* 下一轮再试 */
    }
  }

  async function decide(request: CloudPairRequest, accept: boolean) {
    try {
      await endpoints.cloudPairDecision(request.client_id, accept);
      requests = requests.filter((item) => item.client_id !== request.client_id);
      options.onWaiting?.(requests.length > 0);
      if (accept) {
        toast(`${request.name || "手机"} 已配对`, "ok");
        options.onPaired?.(request.name || "手机");
      } else {
        toast("已拒绝这次配对", "plain");
      }
      // 用过的码作废，换一个新的给下一台
      await start();
    } catch (err) {
      reportError(err, "处理配对失败");
    }
  }

  function paintTTL() {
    if (!ttlEl || !invite) return;
    const left = Math.max(0, Math.round(invite.expires_in - (Date.now() - inviteAt) / 1000));
    ttlEl.textContent = `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")} 后换新码`;
  }

  function paint() {
    clear(el);
    if (requests.length > 0) {
      for (const request of requests) {
        el.append(
          h(
            "div",
            { class: "hub-sas" },
            h("div", { class: "note", text: `${request.name || "一台手机"}（${request.platform || "Android"}）请求配对` }),
            h("div", { class: "hub-sas-words" }, ...request.sas.map((word) => h("span", { text: word }))),
            h("div", { class: "note", text: "和手机上显示的四个词一样才点接受。" }),
            h(
              "div",
              { class: "hub-sas-actions" },
              button("不一样，拒绝", { variant: "quiet", onClick: () => void decide(request, false) }),
              button("一样，接受", { variant: "primary", iconSvg: icons.check, onClick: () => void decide(request, true) }),
            ),
          ),
        );
      }
      return;
    }
    if (!invite) {
      el.append(h("div", { class: error ? "note bad-text" : "note", text: error ? `生成配对码失败：${error}` : "正在生成二维码…" }));
      if (error) el.append(button("重试", { onClick: () => void start() }));
      return;
    }
    const qr = h("div", { class: "hub-qr", style: `width:${qrSize + 8}px;height:${qrSize + 8}px` }, h("div", { class: "note", text: "生成中…" }));
    void QRCode.toDataURL(landingURL(invite, connection), {
      errorCorrectionLevel: "M",
      margin: 1,
      width: qrSize * 2,
      color: { dark: "#0f172a", light: "#f8fafc" },
    })
      .then((url) => qr.replaceChildren(h("img", { src: url, alt: "安装与配对二维码", width: String(qrSize), height: String(qrSize), style: `width:${qrSize}px;height:${qrSize}px` })))
      .catch(() => qr.replaceChildren(h("div", { class: "note", text: "二维码生成失败，用下面的配对码" })));
    ttlEl = h("span", { class: "note" });
    paintTTL();
    el.append(
      qr,
      h(
        "ol",
        { class: "hub-phone-steps" },
        h("li", null, h("b", { text: "用手机相机扫码" }), h("span", { text: "没装 App 就先下载安装（同一个 Wi‑Fi）" })),
        h("li", null, h("b", { text: "点「打开 Homing 配对」" }), h("span", { text: "已装 App 也可以在「我的 › 添加电脑」里扫这个码" })),
        h("li", null, h("b", { text: "在这里核对四个词" }), h("span", { text: "一致就点接受，连接是端到端加密的" })),
      ),
      h(
        "div",
        { class: "hub-code" },
        h("span", { class: "note", text: "或输入配对码" }),
        h("b", { class: "mono", text: invite.code6.replace(/(\d{3})(\d{3})/, "$1 $2") }),
        ttlEl,
      ),
      h(
        "div",
        { class: "hub-phone-foot" },
        button("下载安卓安装包", { small: true, variant: "quiet", iconSvg: icons.download, onClick: () => void openExternalUrl("/apk") }),
        button("换一个", { small: true, variant: "quiet", iconSvg: icons.refresh, onClick: () => void start() }),
      ),
      h("div", {
        class: "note",
        text:
          invite.mode === "cloud"
            ? "当前走官方中转：手机不在同一个 Wi‑Fi 也能配对，但下载安装包需要同一个 Wi‑Fi。"
            : "局域网直连：手机需要和这台电脑在同一个 Wi‑Fi。",
      }),
    );
  }

  el.append(h("div", { class: "note", text: "正在生成二维码…" }));
  return {
    el,
    start,
    dispose: () => {
      disposed = true;
      if (timer) window.clearInterval(timer);
    },
  };
}
