// 设置 › 关于：版本、运行信息、开源致谢与打赏支持。

import { h, clear } from "../lib/dom";
import { icons, pigeon } from "../lib/icons";
import { uptime } from "../lib/format";
import { endpoints, type ConnectionInfo } from "../lib/api";
import { button, groupHead, listCard, openExternalUrl, reportError, tag, toast } from "../lib/ui";
import { loc } from "../i18n";
import type { PageContext, PageResult } from "../main";

export function renderAbout(ctx: PageContext): PageResult {
  const root = h("div");
  const infoHost = h("div");
  const qrPanelHost = h("div");
  let disposed = false;
  let qrExpanded = false;

  const hero = h(
    "div",
    { class: "list-card", style: "padding:20px;display:flex;gap:16px;align-items:center;justify-content:space-between;flex-wrap:wrap" },
    h(
      "div",
      { style: "display:flex;gap:16px;align-items:center" },
      h("span", { style: "color:#CBD5E1;display:inline-flex" }, pigeon(48, "#CBD5E1")),
      h(
        "div",
        null,
        h(
          "div",
          { style: "display:flex;align-items:center;gap:8px" },
          h("span", { style: "font-size:18px;font-weight:600;color:var(--text)", text: "Homing" }),
          tag("Apache-2.0 开源", "ok"),
        ),
        h("div", { class: "note", text: "Every coding agent, one pocket inbox." }),
        h("div", { class: "note", text: "每个 agent 的回复，都能飞回你手里。" }),
      ),
    ),
    h(
      "div",
      { style: "display:flex;gap:8px;flex-wrap:wrap" },
      button(loc("GitHub 仓库 ↗", "GitHub Repo ↗"), {
        small: true,
        variant: "quiet",
        onClick: () => void openExternalUrl("https://github.com/tomato-hug/homing"),
      }),
      button(loc("请作者喝杯咖啡 ☕", "Support Homing ☕"), {
        small: true,
        variant: "primary",
        onClick: () => {
          qrExpanded = !qrExpanded;
          paintSponsorQR();
        },
      }),
    ),
  );

  function paintSponsorQR() {
    clear(qrPanelHost);
    if (!qrExpanded) return;
    const qrCard = (title: string, badgeText: string, accent: string, src: string) =>
      h(
        "div",
        {
          style: `flex:1;min-width:210px;max-width:260px;background:var(--bg-elev, rgba(15,23,42,0.55));border:1px solid ${accent}44;border-radius:12px;padding:14px;text-align:center`,
        },
        h("div", { style: `font-size:13.5px;font-weight:600;color:${accent};margin-bottom:8px`, text: `${title} · ${badgeText}` }),
        h("img", {
          src,
          alt: title,
          style: "width:190px;height:220px;object-fit:contain;border-radius:10px;display:block;margin:0 auto",
        }),
      );

    qrPanelHost.append(
      h(
        "div",
        {
          style: "padding:16px 18px;border-top:1px solid var(--line);display:flex;gap:16px;flex-wrap:wrap;justify-content:center;align-items:center",
        },
        qrCard(loc("微信赞赏码", "WeChat Reward"), loc("微信扫一扫", "Scan in WeChat"), "#07C160", "/console/sponsor/wechat-reward.png"),
        qrCard(loc("支付宝收款码", "Alipay Tip"), loc("支付宝扫一扫", "Scan in Alipay"), "#1677FF", "/console/sponsor/alipay-reward.png"),
      ),
    );
  }

  const sponsorCard = listCard(
    h(
      "div",
      { class: "lrow", style: "align-items:center" },
      h(
        "div",
        { class: "lrow-main" },
        h(
          "div",
          { class: "lrow-title" },
          h("span", { class: "txt", text: loc("支持开源与公网中继带宽", "Support Open Source & Public Relay") }),
          tag(loc("自愿打赏", "Voluntary Tip"), "plain"),
        ),
        h(
          "div",
          { class: "lrow-sub", style: "white-space:normal" },
          loc(
            "Homing 坚持 Apache-2.0 开源、无广告、端到端零知识加密。如果它帮你在离席时省下了切 App 与配网络的麻烦，欢迎通过微信或支付宝请作者喝杯咖啡——所有打赏将用于分担官方公网中继服务器的带宽成本，并可在 SPONSORS.md 留名致谢（支持匿名）。",
            "Homing is Apache-2.0 open source, ad-free, and E2E encrypted. Tips directly help cover official public relay bandwidth costs, and backers are credited in SPONSORS.md.",
          ),
        ),
      ),
      h(
        "div",
        { class: "lrow-actions" },
        button(loc("展示微信 / 支付宝赞赏码", "Show WeChat / Alipay QR"), {
          small: true,
          variant: "primary",
          onClick: () => {
            qrExpanded = !qrExpanded;
            paintSponsorQR();
          },
        }),
      ),
    ),
    qrPanelHost,
  );

  const versionCard = listCard(
    h(
      "div",
      { class: "lrow", style: "align-items:center" },
      h(
        "div",
        { class: "lrow-main" },
        h("div", { class: "lrow-title" }, h("span", { class: "txt", text: "版本" }), tag("Apache-2.0", "plain")),
        h("div", { class: "lrow-sub", style: "white-space:normal" }, "桌面端、服务端与手机端共用同一套端到端加密协议；中继接口保持向后兼容。"),
      ),
      h(
        "div",
        { class: "lrow-actions" },
        button("连接与官方中继", {
          small: true,
          variant: "quiet",
          iconSvg: icons.plug,
          onClick: () => ctx.navigate("/console/relay"),
        }),
      ),
    ),
  );

  const creditsCard = listCard(
    h(
      "div",
      { class: "lrow" },
      h(
        "div",
        { class: "lrow-main" },
        h("div", { class: "lrow-title" }, h("span", { class: "txt", text: "架构灵感与生态致谢" })),
        h(
          "div",
          { class: "lrow-sub", style: "white-space:normal" },
          "感谢 Magpie（yetone/magpie）在单文件 Go + Wails 跨平台桌面架构上的先行探索；感谢 Claude Code、OpenAI Codex、WorkBuddy、OpenCode、Gemini CLI、Kimi CLI 等开放 CLI / Hook / ACP 协议生态，以及 @lobehub/icons 提供的开源品牌图标资源。",
        ),
      ),
    ),
    h(
      "div",
      { class: "lrow" },
      h(
        "div",
        { class: "lrow-main" },
        h("div", { class: "lrow-title" }, h("span", { class: "txt", text: "核心开源组件与字体许可" })),
        h(
          "div",
          { class: "lrow-sub", style: "white-space:normal" },
          "Homing 基于 Apache License 2.0 开源发布。桌面与中继端构建于 Go 标准库、Wails、modernc.org/sqlite、coder/websocket 与 golang.org/x/crypto（NaCl / Ed25519）；Android 客户端构建于 Jetpack Compose、OkHttp、Lazysodium 与 ZXing；等宽字体采用 JetBrains Mono（SIL Open Font License 1.1）。",
        ),
      ),
    ),
    h(
      "div",
      { class: "lrow", style: "align-items:center" },
      h(
        "div",
        { class: "lrow-main" },
        h("div", { class: "lrow-title" }, h("span", { class: "txt", text: "贡献者与赞助者鸣谢榜" })),
        h(
          "div",
          { class: "lrow-sub", style: "white-space:normal" },
          "感谢每一位提交 Issue、PR 与分担公网中继带宽的社区伙伴，完整名单同步维护于仓库 SPONSORS.md。",
        ),
      ),
      h(
        "div",
        { class: "lrow-actions" },
        button("查看 SPONSORS.md ↗", {
          small: true,
          variant: "quiet",
          onClick: () => void openExternalUrl("https://github.com/tomato-hug/homing/blob/main/SPONSORS.md"),
        }),
      ),
    ),
  );

  const communityCard = listCard(
    h(
      "div",
      { class: "lrow", style: "align-items:center" },
      h(
        "div",
        { class: "lrow-main" },
        h(
          "div",
          { class: "lrow-title" },
          h("span", { class: "txt", text: loc("问题反馈与交流 QQ 群", "Feedback & Community QQ Group") }),
          tag("512365124", "ok"),
        ),
        h(
          "div",
          { class: "lrow-sub", style: "white-space:normal" },
          loc(
            "安装部署答疑、国内公网中继与代理分流配置、新 Agent 适配需求及实测 Bug 反馈，欢迎加入官方交流 QQ 群：512365124，或在 GitHub 提交 Issue。",
            "Join QQ Group 512365124 for setup help, relay configuration, adapter requests, and bug reports, or open an issue on GitHub.",
          ),
        ),
      ),
      h(
        "div",
        { class: "lrow-actions" },
        button(loc("复制群号 512365124", "Copy 512365124"), {
          small: true,
          variant: "primary",
          onClick: () => {
            void navigator.clipboard?.writeText("512365124");
            toast(loc("已复制反馈交流 QQ 群号：512365124", "Copied QQ Group: 512365124"), "ok");
          },
        }),
      ),
    ),
  );

  root.append(
    h("div", { class: "group" }, hero),
    h("div", { class: "group" }, groupHead(loc("社区交流与问题反馈", "Community & Feedback")), communityCard),
    h("div", { class: "group" }, groupHead(loc("打赏与支持", "Support & Sponsor")), sponsorCard),
    h("div", { class: "group" }, groupHead(loc("致谢与开源许可", "Acknowledgments & Licenses")), creditsCard),
    h("div", { class: "group" }, groupHead("版本"), versionCard),
    h("div", { class: "group" }, groupHead("运行信息"), infoHost),
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
