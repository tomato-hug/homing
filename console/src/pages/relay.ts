// 连接 › 中转与局域网：三种连接方式、本机地址、安全说明

import { h, clear } from "../lib/dom";
import { icons } from "../lib/icons";
import { uptime } from "../lib/format";
import { api, endpoints, poll, type ConnectionInfo } from "../lib/api";
import { button, groupHead, kv, listCard, reportError, tag, toast } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

export function renderRelay(_ctx: PageContext): PageResult {
  const root = h("div");
  let disposed = false;
  const host = h("div");

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast("已复制", "ok");
    } catch {
      toast("复制失败，请手动选中", "bad");
    }
  }

  async function saveCloud(server: string) {
    try {
      const result = await api.put<{ ok: boolean; cloud: ConnectionInfo["cloud"] }>("/api/cloud/config", { server });
      toast(
        server.trim()
          ? result.cloud?.connected
            ? "中继已连接"
            : "中继地址已保存，正在后台连接"
          : "已关闭云中转连接",
        "ok",
        5000,
      );
      await load();
    } catch (err) {
      reportError(err, "保存云中转地址失败");
    }
  }

  function methodRow(options: {
    title: string;
    desc: string;
    state: "on" | "ready" | "soon" | "off";
    badge?: { text: string; tone: "ok" | "warn" | "mute" | "plain" };
    note?: string;
  }): HTMLElement {
    const badge =
      options.badge
        ? tag(options.badge.text, options.badge.tone)
        : options.state === "on"
          ? tag("当前使用", "ok")
          : options.state === "ready"
            ? tag("可用", "plain")
            : options.state === "soon"
              ? tag("即将推出", "warn")
              : tag("这一版不可选", "mute");
    const dotClass =
      options.state === "on" ? "dot dot-ok" : options.state === "ready" ? "dot dot-warn" : "dot";
    return h(
      "div",
      { class: "lrow", style: "align-items:flex-start" },
      h("span", { class: dotClass, style: "margin-top:8px" }),
      h(
        "div",
        { class: "lrow-main" },
        h("div", { class: "lrow-title" }, h("span", { class: "txt", text: options.title }), badge),
        h("div", { class: "lrow-sub", style: "white-space:normal", text: options.desc }),
        options.note ? h("div", { class: "note", style: "margin-top:4px", text: options.note }) : null,
      ),
    );
  }

  function paint(info: ConnectionInfo) {
    clear(host);
    const cloud = info.cloud || { configured: false, connected: false, server: "", peers: 0 };
    const cloudConfigured = Boolean(cloud.configured);
    const cloudConnected = cloudConfigured && Boolean(cloud.connected);
    const cloudBadge = !cloudConfigured
      ? { text: "可配置", tone: "plain" as const }
      : cloudConnected
        ? { text: "已连接", tone: "ok" as const }
        : { text: "已配置 · 重连中", tone: "warn" as const };
    const cloudNote = !cloudConfigured
      ? "尚未配置。填写 Homing 云中转或自建 homingd 的 WebSocket 地址后，手机可在离开局域网时继续使用。"
      : `${cloudConnected ? "已连接" : "正在后台重连"}：${cloud.server}${cloud.peers > 0 ? ` · ${cloud.peers} 台已配对设备` : ""}`;

    // 连接方式
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("连接方式"),
        listCard(
          methodRow({
            title: "局域网直连",
            desc: "手机和这台电脑在同一个 Wi-Fi 下，直接连，不经过任何服务器。",
            state: "on",
            note: `配对时用局域网地址：${info.lan_urls[0] || `http://127.0.0.1:${info.port}`}`,
          }),
          methodRow({
            title: "Homing 云中转",
            desc: "不在同一个网络时，通过 Homing 的服务器转发。",
            state: "ready",
            badge: cloudBadge,
            note: cloudNote,
          }),
          methodRow({
            title: "自建服务器",
            desc: "把 homingd 跑在自己的服务器上，手机连那台机器。",
            state: "ready",
            badge: { text: "可配置", tone: "plain" },
            note: "与云中转使用同一套端到端加密 RPC；地址可指向自建 homingd。",
          }),
        ),
      ),
    );

    // 云中转地址：与自建服务器共用一条 connector。
    const serverInput = h("input", {
      class: "input mono",
      type: "text",
      value: cloud.server || "",
      placeholder: "wss://relay.example.com/v1/ws",
      spellcheck: "false",
      autocomplete: "off",
      style: "min-width:240px;flex:1",
    }) as HTMLInputElement;
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("云中转设置"),
        listCard(
          h(
            "div",
            { class: "lrow", style: "align-items:flex-start" },
            h("span", { class: cloudConnected ? "dot dot-ok" : "dot", style: "margin-top:8px" }),
            h(
              "div",
              { class: "lrow-main" },
              h(
                "div",
                { class: "lrow-title" },
                h("span", { class: "txt", text: cloudConfigured ? "已配置中继" : "连接云中转或自建服务器" }),
                tag(cloudConnected ? "已连接" : cloudConfigured ? "重连中" : "未配置", cloudConnected ? "ok" : cloudConfigured ? "warn" : "mute"),
              ),
              h("div", { class: "lrow-sub", style: "white-space:normal", text: "填写 WebSocket 地址；保存后立即在后台连接，无需重启本机服务。" }),
              h(
                "div",
                { style: "display:flex;gap:8px;flex-wrap:wrap;margin-top:10px;align-items:center" },
                serverInput,
                button("保存并重连", {
                  small: true,
                  variant: "primary",
                  iconSvg: icons.refresh,
                  onClick: () => void saveCloud(serverInput.value),
                }),
                cloudConfigured
                  ? button("断开", {
                      small: true,
                      variant: "quiet",
                      onClick: () => void saveCloud(""),
                    })
                  : null,
              ),
            ),
          ),
        ),
        h("div", { class: "note", style: "margin-top:8px" }, "中继地址保存在本机 peers.json；手机仍需单独配对。"),
      ),
    );

    // 本机地址
    const addresses = [
      { who: "本机", url: `http://127.0.0.1:${info.port}`, hint: "控制台" },
      ...info.lan_urls.map((url, index) => ({ who: index === 0 ? "局域网" : "局域网", url, hint: "手机配对" })),
    ];
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("这台电脑的地址"),
        h(
          "div",
          { class: "list-card", style: "padding:10px 15px" },
          h(
            "div",
            null,
            ...addresses.map((item) =>
              h(
                "div",
                { class: "addr-row" },
                h("span", { class: "who", text: item.who }),
                h("span", { style: "flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis", text: item.url }),
                h("span", { class: "note", text: item.hint }),
                button("复制", {
                  small: true,
                  variant: "quiet",
                  iconSvg: icons.copy,
                  onClick: () => void copy(item.hint === "控制台" ? `${item.url}/console` : item.url),
                }),
              ),
            ),
          ),
          h("div", { class: "note", style: "margin-top:8px" }, "手机配对用局域网地址；控制台只在本机 127.0.0.1 打开。"),
        ),
      ),
    );

    // 安全
    const e2eTag =
      info.security.e2e === "connected"
        ? tag("端到端加密 · 已连接", "ok")
        : info.security.e2e === "configured"
          ? tag("端到端加密 · 待重连", "warn")
          : info.security.e2e === "planned_m4"
            ? tag("端到端加密 M4 接入", "warn")
            : tag("端到端加密 · 可配置", "plain");
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("安全"),
        h(
          "div",
          { class: "list-card", style: "padding:14px 15px" },
          h(
            "div",
            { style: "display:flex;gap:8px;flex-wrap:wrap;margin-bottom:10px" },
            tag(info.security.tls ? "HTTPS" : "局域网明文传输", info.security.tls ? "ok" : "warn"),
            tag("设备令牌鉴权", "ok"),
            e2eTag,
          ),
          h(
            "div",
            { class: "note" },
            "本机控制台用 HttpOnly cookie 鉴权，页面里不出现 token；手机用一次性配对令牌换长期设备令牌，可以随时在「手机 › 设备」里移除。局域网直连是明文 HTTP；云中转与自建服务器走端到端加密 RPC。",
          ),
        ),
      ),
    );

    // 本机服务
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("本机服务"),
        h(
          "div",
          { class: "list-card", style: "padding:14px 15px" },
          kv([
            ["版本", info.version],
            ["平台", `${info.platform} · ${info.arch}`],
            ["运行方式", info.runtime === "go" ? "单文件 homing（Go）" : info.packaged ? "单文件 homingd" : "node relay/server.mjs"],
            [info.runtime === "go" ? "Go" : "Node", info.runtime === "go" ? info.runtime_version || "-" : info.node],
            ["端口", String(info.port)],
            ["已运行", uptime(info.uptime_s)],
            ["数据目录", info.data_dir],
            ["控制台目录", info.console_dir],
          ]),
        ),
      ),
    );

    // 开机自启（纯命令行版）
    const enabled = info.autostart.enabled;
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead(
          "开机自启（命令行版）",
          undefined,
          button(enabled ? "关闭" : "开启", {
            small: true,
            variant: enabled ? "quiet" : "ghost",
            iconSvg: icons.power,
            onClick: async () => {
              try {
                const result = await api.post<{ ok: boolean; note?: string; error?: string }>("/api/autostart", { enabled: !enabled });
                if (!result.ok) {
                  toast(result.error || "设置失败", "bad");
                  return;
                }
                toast(result.note || "已更新", "ok");
                await load();
              } catch (err) {
                reportError(err, "设置开机自启失败");
              }
            },
          }),
        ),
        h(
          "div",
          { class: "list-card", style: "padding:14px 15px" },
          h(
            "div",
            { style: "display:flex;gap:8px;flex-wrap:wrap;margin-bottom:8px" },
            tag(enabled ? "已开启" : "未开启", enabled ? "ok" : "mute"),
            tag(info.autostart.backend === "run-key" ? "当前用户登录启动项" : "用户级 LaunchAgent", "plain"),
          ),
          info.autostart.legacy
            ? h("div", { class: "note", style: "color:var(--amber)", text: `检测到已有 launchd 服务：${info.autostart.legacy.split("/").pop()}，桌面端不会重复拉起 homingd。` })
            : null,
          h("div", { class: "note", text: "桌面 App 用系统自带的登录项；这里管的是纯命令行版 homingd。" }),
        ),
      ),
    );
  }

  async function load() {
    try {
      const info = await endpoints.connection();
      if (disposed) return;
      const active = document.activeElement;
      if (active instanceof HTMLInputElement && host.contains(active)) return;
      paint(info);
    } catch (err) {
      if (disposed) return;
      clear(host);
      host.append(h("div", { class: "list-card" }, h("div", { class: "empty", text: (err as Error).message })));
    }
  }

  clear(host);
  host.append(h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: "正在读取连接信息…" })));
  root.append(host);
  void load();
  const stop = poll(load, 10_000);

  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
    },
  };
}
