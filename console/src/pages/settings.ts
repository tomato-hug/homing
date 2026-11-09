// 设置：侧栏底部那个入口。放常规开关、维护入口和版本信息。
// 这一页不做表单大杂烩——每个 agent 的细节在自己的卡片里，这里只管全局的几件事。

import { h, clear, icon, type Child } from "../lib/dom";
import { icons } from "../lib/icons";
import { uptime } from "../lib/format";
import { api, endpoints, type ConnectionInfo } from "../lib/api";
import { groupHead, listCard, reportError, tag, toast, toggle } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

/** 一行：左边标题 + 说明，右边控件 */
function row(options: { title: string; sub?: Child | null; right?: Child[] }): HTMLElement {
  return h(
    "div",
    { class: "lrow", style: "align-items:center" },
    h(
      "div",
      { class: "lrow-main" },
      h("div", { class: "lrow-title" }, h("span", { class: "txt", text: options.title })),
      options.sub ? h("div", { class: "lrow-sub", style: "white-space:normal" }, options.sub) : null,
    ),
    h("div", { class: "lrow-actions" }, ...(options.right || [])),
  );
}

function linkButton(label: string, svg: string, onClick: () => void): HTMLButtonElement {
  const el = h("button", { class: "btn btn-quiet btn-sm", type: "button" });
  el.append(icon(svg, 13), h("span", { text: label }));
  el.addEventListener("click", onClick);
  return el;
}

export function renderSettings(ctx: PageContext): PageResult {
  const root = h("div");
  const host = h("div");
  root.append(host);

  let disposed = false;

  async function copy(text: string, what: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast(`已复制${what}`, "ok");
    } catch {
      toast("复制失败，请手动选中", "bad");
    }
  }

  function paint(payload: ConnectionInfo) {
    clear(host);

    // 常规
    const autostartOn = payload.autostart.enabled;
    const autostartCard = listCard(
      row({
        title: "开机自启",
        sub: h(
          "span",
          null,
          "登录后自动把本机服务拉起来，不用每次手动开。",
          payload.autostart.legacy
            ? h("span", { style: "color:var(--amber)", text: " 检测到已有 launchd 服务，两者不会重复启动。" })
            : null,
        ),
        right: [
          tag(autostartOn ? "已开启" : "未开启", autostartOn ? "ok" : "mute"),
          toggle({
            on: autostartOn,
            title: "开机自启",
            onChange: async (next) => {
              try {
                const result = await api.post<{ ok: boolean; note?: string; error?: string }>("/api/autostart", {
                  enabled: next,
                });
                if (!result.ok) {
                  toast(result.error || "设置失败", "bad");
                  await load();
                  return;
                }
                toast(result.note || (next ? "已开启开机自启" : "已关闭开机自启"), "ok");
                await load();
              } catch (err) {
                reportError(err, "设置开机自启失败");
                await load();
              }
            },
          }),
        ],
      }),
      row({
        title: "首次设置向导",
        sub: "重新选一遍要接入的 agent 和连接方式，不影响已经配好的设备。",
        right: [linkButton("重新走一遍", icons.key, () => ctx.navigate("/console/setup"))],
      }),
    );

    // 维护
    const maintenanceCard = listCard(
      row({
        title: "运行日志",
        sub: "homingd 最近几百行，只写在本机，token 与配对令牌会先抹掉。",
        right: [linkButton("查看日志", icons.logs, () => ctx.navigate("/console/logs"))],
      }),
      row({
        title: "数据目录",
        sub: h("span", { class: "mono", text: payload.data_dir }),
        right: [linkButton("复制路径", icons.copy, () => void copy(payload.data_dir, "数据目录"))],
      }),
      row({
        title: "连接与地址",
        sub: "换 Wi-Fi、改端口、看局域网地址都在那边。",
        right: [linkButton("去连接页", icons.plug, () => ctx.navigate("/console/relay"))],
      }),
    );

    // 关于
    const aboutCard = listCard(
      row({
        title: "版本",
        sub: h(
          "span",
          { class: "mono" },
          `v${payload.version} · ${payload.packaged ? "单文件 homingd" : "node relay/server.mjs"} · 已运行 ${uptime(payload.uptime_s)}`,
        ),
        right: [linkButton("关于 Homing", icons.info, () => ctx.navigate("/console/about"))],
      }),
      row({
        title: "运行环境",
        sub: h("span", { class: "mono", text: `${payload.platform} · ${payload.arch} · Node ${payload.node}` }),
        right: [linkButton("复制版本信息", icons.copy, () => void copy(`${payload.platform} ${payload.arch} node ${payload.node} homing ${payload.version}`, "版本信息"))],
      }),
    );

    host.append(
      h("div", { class: "group" }, groupHead("常规"), autostartCard),
      h("div", { class: "group" }, groupHead("维护"), maintenanceCard),
      h("div", { class: "group" }, groupHead("关于"), aboutCard),
    );
  }

  async function load() {
    try {
      const payload = await endpoints.connection();
      if (disposed) return;
      paint(payload);
    } catch (err) {
      if (disposed) return;
      clear(host);
      host.append(
        h(
          "div",
          { class: "list-card", style: "padding:18px" },
          h("div", { class: "empty", text: `读不到本机信息：${(err as Error).message}` }),
        ),
      );
    }
  }

  clear(host);
  host.append(h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: "正在读取设置…" })));
  void load();

  return {
    el: root,
    dispose: () => {
      disposed = true;
    },
  };
}
