// 手机 › 设备：已配对列表 + 配对新手机（二维码 / 6 位码）

import { h, clear } from "../lib/dom";
import { icons } from "../lib/icons";
import { appVersionLabel, timeAgo } from "../lib/format";
import { endpoints, poll, type Device } from "../lib/api";
import { createPairCard } from "../lib/pair-card";
import { confirmDialog } from "../lib/dialog";
import { button, emptyState, groupHead, listCard, reportError, tag, toast } from "../lib/ui";
import { loc, t } from "../i18n";
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
                    tag(
                      device.transport === "e2e"
                        ? device.online
                          ? loc("在线", "Online")
                          : loc("端到端", "E2E")
                        : device.online
                          ? loc("在线", "Online")
                          : loc("离线", "Offline"),
                      device.online ? "ok" : "mute",
                    ),
                  ),
                  h(
                    "div",
                    { class: "lrow-sub" },
                    h("span", { class: "mono", text: `${device.platform === "android" ? "Android" : device.platform} · ${appVersionLabel(device.app_version)}` }),
                    ` · ${
                      device.transport === "e2e"
                        ? "端到端配对"
                        : device.paired_via === "qr"
                          ? "扫码配对"
                          : "配对码配对"
                    } · ${
                      device.last_seen_at
                        ? timeAgo(device.last_seen_at)
                        : device.transport === "e2e" && device.created_at
                          ? `配对于 ${timeAgo(device.created_at)}`
                          : "还没连过"
                    }`,
                  ),
                  h(
                    "div",
                    { class: "lrow-sub" },
                    device.transport === "e2e"
                      ? "权限：端到端加密连接（读取 / 回复 / 操作）"
                      : `权限：${(device.scopes?.length ? device.scopes : ALL_DEVICE_SCOPES)
                          .map((scope) => DEVICE_SCOPE_LABELS[scope] || scope)
                          .join(" / ")}`,
                  ),
                ),
                h(
                  "div",
                  { class: "lrow-actions" },
                  ...(device.transport === "e2e"
                    ? []
                    : ALL_DEVICE_SCOPES.map((scope) => {
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
                              const ok = await confirmDialog({
                                title: loc("关闭读取权限", "Disable Read Permission"),
                                message: loc("关闭读取权限后，这台手机会看不到任何会话。继续？", "This phone will no longer see any sessions. Continue?"),
                                danger: true,
                              });
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
                      })),
                  button(t("common.remove"), {
                    small: true,
                    variant: "quiet",
                    onClick: async () => {
                      const ok = await confirmDialog({
                        title: loc("移除已配对设备", "Remove Paired Device"),
                        message:
                          device.transport === "e2e"
                            ? loc(`移除「${device.name}」？这台手机会失去配对，必须重新扫码。`, `Remove "${device.name}"? This phone will lose pairing and must rescan.`)
                            : loc(`移除「${device.name}」？它的设备令牌会立刻作废，手机需要重新配对。`, `Remove "${device.name}"? Its token will be revoked immediately.`),
                        confirmLabel: t("common.remove"),
                        danger: true,
                      });
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

    // 与首页同一张卡：一个二维码，相机扫了下载 App，App 里扫了直接配对；电脑上核对四个词
    const card = createPairCard({
      onPaired: () => {
        void refreshDevices();
        ctx.refreshShell();
      },
    });
    pairingHost.append(
      h(
        "div",
        { class: "list-card", style: "padding:16px;max-width:420px" },
        card.el,
        h("div", { style: "margin-top:10px" }, button("收起", { small: true, variant: "quiet", onClick: () => closePairing() })),
      ),
    );
    void card.start();
    timerCleanup = () => card.dispose();
  }

  let timerCleanup: (() => void) | null = null;

  async function openPairing() {
    if (pairingOpen) return;
    pairingOpen = true;
    paintPairing();
  }

  function closePairing() {
    pairingOpen = false;
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
          text: "二维码里是一次性配对令牌，5 分钟有效。手机和电脑交换公钥后在电脑上核对四个校验词，之后的连接都是端到端加密的（局域网直连或官方中转）。",
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
