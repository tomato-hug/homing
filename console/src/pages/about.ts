// 设置 › 关于：版本、运行信息、许可。

import { h, clear } from "../lib/dom";
import { icons, pigeon } from "../lib/icons";
import { uptime } from "../lib/format";
import { endpoints, type ConnectionInfo } from "../lib/api";
import { button, groupHead, listCard, reportError, tag, toast } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

export function renderAbout(ctx: PageContext): PageResult {
  const root = h("div");
  const infoHost = h("div");
  let disposed = false;

  const hero = h(
    "div",
    { class: "list-card", style: "padding:20px;display:flex;gap:16px;align-items:center" },
    h("span", { style: "color:#CBD5E1;display:inline-flex" }, pigeon(48, "#CBD5E1")),
    h(
      "div",
      null,
      h("div", { style: "font-size:18px;font-weight:600;color:var(--text)", text: "Homing" }),
      h("div", { class: "note", text: "Every coding agent, one pocket inbox." }),
      h("div", { class: "note", text: "每个 agent 的回复，都能飞回你手里。" }),
    ),
  );

  const versionCard = listCard(
    h(
      "div",
      { class: "lrow", style: "align-items:center" },
      h(
        "div",
        { class: "lrow-main" },
        h("div", { class: "lrow-title" }, h("span", { class: "txt", text: "版本" }), tag("开发版 · 未签名", "warn")),
        h("div", { class: "lrow-sub", style: "white-space:normal" }, "桌面端与手机端共用一个版本号；中继接口保持向后兼容。"),
      ),
      h(
        "div",
        { class: "lrow-actions" },
        button("检查更新", {
          small: true,
          variant: "quiet",
          iconSvg: icons.refresh,
          onClick: () => toast("这一版还没有接自动更新，签名与更新通道放在开源发布阶段"),
        }),
        button("连接与地址", {
          small: true,
          variant: "quiet",
          iconSvg: icons.plug,
          onClick: () => ctx.navigate("/console/relay"),
        }),
      ),
    ),
  );

  const credit = listCard(
    h(
      "div",
      { class: "lrow" },
      h(
        "div",
        { class: "lrow-main" },
        h("div", { class: "lrow-title" }, h("span", { class: "txt", text: "许可与致谢" })),
        h(
          "div",
          { class: "lrow-sub", style: "white-space:normal" },
          "Homing 会以开源方式发布（MIT）。界面字体使用系统无衬线；等宽部分使用 JetBrains Mono（OFL 许可，随包分发）。鸽子图标由 pigeon.json 的四层路径绘制，静态轮廓用于托盘与图标。",
        ),
      ),
    ),
  );

  root.append(
    h("div", { class: "group" }, hero),
    h("div", { class: "group" }, groupHead("版本"), versionCard),
    h("div", { class: "group" }, groupHead("运行信息"), infoHost),
    h("div", { class: "group" }, groupHead("许可"), credit),
  );

  function paintInfo(info: ConnectionInfo) {
    const entries: Array<[string, string]> = [
      ["应用版本", `v${info.version}`],
      ["运行方式", info.runtime === "go" ? "单文件 homing（Go）" : info.packaged ? "单文件 homingd" : "node relay/server.mjs"],
      ["平台", `${info.platform} · ${info.arch}`],
      [info.runtime === "go" ? "Go" : "Node", info.runtime === "go" ? info.runtime_version || "-" : info.node],
      ["端口", String(info.port)],
      ["已运行", uptime(info.uptime_s)],
      ["数据目录", info.data_dir],
      ["控制台目录", info.console_dir],
    ];
    clear(infoHost);
    infoHost.append(
      listCard(
        ...entries.map(([key, value]) =>
          h(
            "div",
            { class: "lrow", style: "align-items:center" },
            h("div", { class: "lrow-main" }, h("div", { class: "lrow-title" }, h("span", { class: "txt", text: key }))),
            h("span", { class: "mono note", style: "max-width:60%;overflow:hidden;text-overflow:ellipsis", text: value, title: value }),
          ),
        ),
      ),
    );
  }

  infoHost.append(h("div", { class: "list-card", style: "padding:16px" }, h("div", { class: "note", text: "正在读取运行信息…" })));
  endpoints
    .connection()
    .then((info) => {
      if (!disposed) paintInfo(info);
    })
    .catch((err) => {
      if (disposed) return;
      clear(infoHost);
      infoHost.append(h("div", { class: "list-card", style: "padding:16px" }, h("div", { class: "empty", text: (err as Error).message })));
      reportError(err, "读取版本信息失败");
    });

  return {
    el: root,
    dispose: () => {
      disposed = true;
    },
  };
}
