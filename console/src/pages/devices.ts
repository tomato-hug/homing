// 手机 › 设备：已配对列表 + 配对新手机（二维码 / 6 位码）

import QRCode from "qrcode";
import { h, clear } from "../lib/dom";
import { icons } from "../lib/icons";
import { appVersionLabel, timeAgo } from "../lib/format";
import { endpoints, poll, type Device, type PairOffer } from "../lib/api";
import { button, emptyState, groupHead, listCard, reportError, tag, toast, spinner } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

const ALL_DEVICE_SCOPES = ["read", "reply", "drive"] as const;
const DEVICE_SCOPE_LABELS: Record<string, string> = {
  read: "读取",
  reply: "回复",
  drive: "操作",
};

export function renderDevices(ctx: PageContext, query: URLSearchParams): PageResult {
  const root = h("div");
  let disposed = false;
  let offer: PairOffer | null = null;
  let countdownTimer: number | null = null;
  let pairingOpen = query.get("pair") === "1";

  const devicesHost = h("div");
  const pairingHost = h("div");

  // ------------------------------------------------------------ 已配对

  async function refreshDevices() {
    try {
      const payload = await endpoints.devices();
      if (disposed) return;
      paintDevices(payload.devices);
    } catch (err) {
      reportError(err, "读取已配对手机失败");
    }
  }

  function paintDevices(devices: Device[]) {
    clear(devicesHost);
    devicesHost.append(
      devices.length === 0
        ? h(
            "div",
            { class: "list-card" },
            emptyState("还没有配对手机", "在下方点「配对新手机」，再用 App 扫码。"),
          )
        : listCard(
            ...devices.map((device) =>
              h(
                "div",
                { class: "lrow" },
                h("span", { class: device.online ? "dot dot-ok" : "dot", style: "margin-top:7px" }),
                h(
                  "div",
                  { class: "lrow-main" },
                  h(
                    "div",
                    { class: "lrow-title" },
                    h("span", { class: "txt", text: device.name }),
                    tag(device.online ? "在线" : "离线", device.online ? "ok" : "mute"),
                  ),
                  h(
                    "div",
                    { class: "lrow-sub" },
                    h("span", { class: "mono", text: `${device.platform === "android" ? "Android" : device.platform} · ${appVersionLabel(device.app_version)}` }),
                    ` · ${device.paired_via === "qr" ? "扫码配对" : "配对码配对"} · ${device.last_seen_at ? timeAgo(device.last_seen_at) : "还没连过"}`,
                  ),
                  h(
                    "div",
                    { class: "lrow-sub" },
                    `权限：${(device.scopes?.length ? device.scopes : ALL_DEVICE_SCOPES)
                      .map((scope) => DEVICE_SCOPE_LABELS[scope] || scope)
                      .join(" / ")}`,
                  ),
                ),
                h(
                  "div",
                  { class: "lrow-actions" },
                  ...ALL_DEVICE_SCOPES.map((scope) => {
                    const enabled = (device.scopes?.length ? device.scopes : ALL_DEVICE_SCOPES).includes(scope);
                    return button(`${DEVICE_SCOPE_LABELS[scope]}${enabled ? " ✓" : " ✕"}`, {
                      small: true,
                      variant: "quiet",
                      title: enabled ? `关闭「${DEVICE_SCOPE_LABELS[scope]}」权限` : `开启「${DEVICE_SCOPE_LABELS[scope]}」权限`,
                      onClick: async () => {
                        const current = device.scopes?.length ? device.scopes : [...ALL_DEVICE_SCOPES];
                        const next = enabled
                          ? current.filter((item) => item !== scope)
                          : [...current, scope];
                        if (enabled && scope === "read") {
                          const ok = window.confirm("关闭读取权限后，这台手机会看不到任何会话。继续？");
                          if (!ok) return;
                        }
                        try {
                          await endpoints.setDeviceScopes(device.device_id, next);
                          toast("权限已更新", "ok");
                          await refreshDevices();
                        } catch (err) {
                          reportError(err, "更新权限失败");
                        }
                      },
                    });
                  }),
                  button("移除", {
                    small: true,
                    variant: "quiet",
                    onClick: async () => {
                      const ok = window.confirm(`移除「${device.name}」？它的设备令牌会立刻作废，手机需要重新配对。`);
                      if (!ok) return;
                      try {
                        await endpoints.revokeDevice(device.device_id);
                        toast("已移除，手机需要重新配对", "ok");
                        await refreshDevices();
                        ctx.refreshShell();
                      } catch (err) {
                        reportError(err, "移除失败");
                      }
                    },
                  }),
                ),
              ),
            ),
          ),
    );
  }

  // ------------------------------------------------------------ 配对新手机

  function paintPairing() {
    clear(pairingHost);
    if (!pairingOpen) {
      pairingHost.append(
        h(
          "div",
          { class: "list-card" },
          h(
            "div",
            { class: "lrow", style: "align-items:center" },
            h("div", { class: "lrow-main" }, h("div", { class: "lrow-title" }, h("span", { class: "txt", text: "用手机扫一下就能连上" })), h("div", { class: "lrow-sub", text: "5 分钟有效的一次性配对码，随时可以换一个" })),
            button("配对新手机", { variant: "primary", small: true, iconSvg: icons.plus, onClick: () => void openPairing() }),
          ),
        ),
      );
      return;
    }

    const qrBox = h("div", { class: "qr-box" }, h("div", { class: "qr-empty", text: "正在生成二维码…" }));
    const codeText = h("div", { class: "code-display mono", text: "······" });
    const ttlText = h("span", { class: "note", text: "" });
    const waitText = h("div", { class: "waiting" });
    const addressText = h("div", { class: "note mono", style: "margin-top:6px", text: "" });
    const changeBtn = button("换一个配对码", { small: true, iconSvg: icons.refresh, onClick: () => void newOffer() });

    pairingHost.append(
      h(
        "div",
        { class: "list-card", style: "padding:16px" },
        h(
          "div",
          { class: "pair-grid" },
          h(
            "div",
            null,
            qrBox,
            h(
              "div",
              { style: "display:flex;gap:8px;margin-top:10px" },
              button("下载安卓版", { small: true, variant: "quiet", iconSvg: icons.download, onClick: () => window.open("/apk", "_blank", "noopener") }),
              button("收起", { small: true, variant: "quiet", onClick: () => closePairing() }),
            ),
          ),
          h(
            "div",
            null,
            h("div", { class: "note", text: "6 位配对码" }),
            codeText,
            h("div", { style: "display:flex;gap:10px;align-items:center;margin:2px 0 12px" }, ttlText, changeBtn),
            h(
              "ol",
              { class: "steps" },
              h("li", { text: "在手机上打开 Homing App" }),
              h("li", { text: "进入「我的 › 添加电脑」" }),
              h("li", { text: "扫左边的二维码，或输入上面的 6 位配对码" }),
            ),
            waitText,
            addressText,
          ),
        ),
      ),
    );

    const paintQr = async (payload: string) => {
      try {
        const dataUrl = await QRCode.toDataURL(payload, {
          errorCorrectionLevel: "M",
          margin: 1,
          width: 416,
          color: { dark: "#0f172a", light: "#f8fafc" },
        });
        qrBox.replaceChildren(h("img", { src: dataUrl, alt: "配对二维码", width: "208", height: "208" }));
      } catch {
        qrBox.replaceChildren(h("div", { class: "qr-empty", text: "二维码生成失败，可以直接输入 6 位配对码" }));
      }
    };

    const paintWaiting = () => {
      if (!offer) return;
      const claimed = offer.status === "claimed";
      const expired = offer.status === "expired";
      waitText.replaceChildren(
        claimed ? tag("手机已配对", "ok") : expired ? tag("配对码已过期，换一个", "warn") : spinner(),
        h("span", {
          text: claimed ? "配对完成，手机应该已经连上了" : expired ? "这个码不能用了" : "等待手机扫码…",
        }),
      );
      if (claimed) {
        codeText.textContent = "已配对";
        ttlText.textContent = "";
        void refreshDevices();
        ctx.refreshShell();
      }
    };

    const paintCountdown = () => {
      if (!offer) return;
      const left = Math.max(0, Math.round((new Date(offer.expires_at).getTime() - Date.now()) / 1000));
      ttlText.textContent =
        offer.status === "claimed" ? "" : left > 0 ? `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")} 后失效` : "已失效";
      if (left === 0 && offer.status === "waiting") {
        offer.status = "expired";
        paintWaiting();
      }
    };

    const newOffer = async () => {
      changeBtn.disabled = true;
      try {
        offer = await endpoints.newOffer();
        codeText.textContent = offer.code.split("").join(" ");
        addressText.textContent = offer.lan_url ? `地址 ${offer.lan_url}` : "";
        if (offer.qr_payload) await paintQr(offer.qr_payload);
        paintCountdown();
        paintWaiting();
      } catch (err) {
        reportError(err, "生成配对码失败");
      } finally {
        changeBtn.disabled = false;
      }
    };

    const pollOffer = async () => {
      if (!offer || offer.status !== "waiting") return;
      try {
        const fresh = await endpoints.offer(offer.offer_id);
        offer = { ...offer, ...fresh };
        paintCountdown();
        paintWaiting();
      } catch {
        /* 下一轮再试 */
      }
    };

    // 这组计时器只在展开期间跑
    const tick = window.setInterval(paintCountdown, 1000);
    const stop = poll(() => pollOffer().catch(() => {}), 2000);
    timerCleanup = () => {
      window.clearInterval(tick);
      stop();
    };
    void newOffer();
  }

  let timerCleanup: (() => void) | null = null;

  async function openPairing() {
    if (pairingOpen) return;
    pairingOpen = true;
    paintPairing();
  }

  function closePairing() {
    pairingOpen = false;
    offer = null;
    if (timerCleanup) {
      timerCleanup();
      timerCleanup = null;
    }
    paintPairing();
  }

  root.append(
    h("div", { class: "group" }, groupHead("已配对的手机"), devicesHost),
    h("div", { class: "group" }, groupHead("配对"), pairingHost),
    h(
      "details",
      { class: "quiet-details" },
      h("summary", { text: "配对与安全说明" }),
      h(
        "p",
        {
          text: "二维码里是一次性配对令牌，5 分钟有效。扫码后手机会换发长期设备令牌，可随时在上方移除。这一版使用局域网直连，端到端加密计划在 M4。",
        },
      ),
    ),
  );

  paintPairing();
  void refreshDevices();
  const stopDevices = poll(() => refreshDevices().catch(() => {}), 10_000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stopDevices();
      if (timerCleanup) timerCleanup();
      if (countdownTimer) window.clearInterval(countdownTimer);
    },
  };
}
