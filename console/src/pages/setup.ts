import QRCode from "qrcode";
import { h, clear } from "../lib/dom";
import { icons, pigeon } from "../lib/icons";
import { AGENT_LABELS, signalHint, statusLabel } from "../lib/format";
import { endpoints, type Agent, type AgentPaths, type ConnectionInfo, type PairOffer } from "../lib/api";
import { agentSquare, button, reportError, tag, toast } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

type Step = 1 | 2 | 3;

export function renderSetup(ctx: PageContext): PageResult {
  const root = h("div", { class: "wizard" });
  let step: Step = 1;
  let disposed = false;
  let agents: Agent[] = [];
  let paths: AgentPaths | null = null;
  let connection: ConnectionInfo | null = null;
  const selected = new Set<string>();
  let method: "lan" | "cloud" | "self_hosted" = "lan";
  let offer: PairOffer | null = null;
  let offerTimer: number | null = null;
  let claimed = false;

  const head = h(
    "div",
    { class: "wizard-head" },
    h("span", { style: "display:inline-flex" }, pigeon(30, "#CBD5E1")),
    h("span", { style: "font-size:16px;font-weight:600", text: "Homing 设置" }),
    h("div", { style: "flex:1" }),
    h(
      "a",
      {
        href: "/console/",
        class: "note",
        "data-link": "1",
        style: "cursor:pointer",
        onClick: () => localStorage.setItem("homing.setup.skipped", "1"),
      },
      "跳过，先去控制台",
    ),
  );

  const progress = h("div", { class: "wizard-steps" });
  const body = h("div");
  const footLeft = h("div");
  const footRight = h("div", { style: "display:flex;gap:10px" });

  const foot = h("div", { class: "wizard-foot" }, footLeft, h("div", { class: "spacer" }), footRight);

  root.append(head, progress, body, foot);

  function paintProgress() {
    clear(progress);
    for (const index of [1, 2, 3]) {
      progress.append(h("div", { class: `wizard-step ${step > index ? "done" : step === index ? "current" : ""}`.trim() }));
    }
  }

  function paintFoot() {
    clear(footLeft);
    clear(footRight);
    if (step > 1) {
      footRight.append(button("上一步", { variant: "quiet", iconSvg: icons.arrowLeft, onClick: () => go((step - 1) as Step) }));
    }
    if (step < 3) {
      footRight.append(
        button("下一步", {
          variant: "primary",
          onClick: () => go((step + 1) as Step),
        }),
      );
    } else {
      footRight.append(
        button(claimed ? "完成" : "稍后配对，先用着", {
          variant: claimed ? "primary" : "quiet",
          onClick: () => void finish(),
        }),
      );
    }
    if (step === 1) {
      footLeft.append(button("重新检测", { variant: "quiet", iconSvg: icons.refresh, onClick: () => void loadAgents() }));
    }
  }

  // ------------------------------------------------------------ 第一步

  function stepAgents() {
    const list = h("div", { class: "list-plain" });
    const content = h(
      "div",
      null,
      h("h2", { text: "找到你的 agent" }),
      h("p", { class: "wizard-sub", text: "Homing 只读它们自己的会话文件，不改内容。勾掉不想推送的。" }),
      h("div", { class: "card", style: "padding:6px" }, list),
      h(
        "div",
        { class: "note", style: "margin-top:12px;line-height:1.9" },
        h("div", { text: "· 安装钩子会修改 ~/.claude/settings.json，修改前自动备份（*.bak-homing-<时间>）。" }),
        connection?.platform_raw === "win32"
          ? h("div", { text: "· 在 Windows 上，WSL 里的 agent 也会被检测到。" })
          : null,
        h("div", { text: "· 轮询读取的 agent 不需要钩子，代价是通知慢几十秒。" }),
      ),
    );

    if (agents.length === 0) {
      list.append(h("div", { class: "empty", text: "还没检测到 agent，装好之后点「重新检测」" }));
    }

    for (const agent of agents) {
      const status = statusLabel(agent.status);
      const path = paths?.paths?.[agent.source]?.path || "";
      const checkbox = h("span", {
        class: selected.has(agent.source) ? "check on" : "check",
        role: "checkbox",
        "aria-checked": selected.has(agent.source) ? "true" : "false",
        tabindex: "0",
      });
      if (selected.has(agent.source)) {
        const svg = document.createElement("span");
        svg.innerHTML = icons.check;
        checkbox.append(svg.firstElementChild as SVGElement);
      }

      const toggle = () => {
        if (selected.has(agent.source)) selected.delete(agent.source);
        else selected.add(agent.source);
        paintStep();
      };
      checkbox.addEventListener("click", toggle);
      checkbox.addEventListener("keydown", (event) => {
        if ((event as KeyboardEvent).key === "Enter" || (event as KeyboardEvent).key === " ") toggle();
      });

      const hookTag = agent.capabilities?.approve
        ? tag("钩子已安装", "ok")
        : agent.push_signal === "poll"
          ? tag("轮询读取", "mute", signalHint(agent.push_signal))
          : tag("需要安装钩子", "warn");

      const installBtn =
        !agent.capabilities?.approve && agent.push_signal !== "poll"
          ? button("安装钩子", {
              small: true,
              variant: "primary",
              iconSvg: icons.download,
              onClick: async (event) => {
                const target = event.currentTarget as HTMLButtonElement;
                target.disabled = true;
                try {
                  const result = await endpoints.installHooks(agent.source);
                  if (result.ok && result.changed) toast("钩子已安装，重启该 agent 后生效", "ok");
                  else if (result.ok) toast(result.message || "已经装好了", "ok");
                  else toast(result.error || result.message || "需要手动添加", "plain", 8000);
                  await loadAgents();
                } catch (err) {
                  reportError(err, "安装钩子失败");
                } finally {
                  target.disabled = false;
                }
              },
            })
          : null;

      list.append(
        h(
          "div",
          { class: "list-row", style: "align-items:center" },
          checkbox,
          agentSquare(agent.source),
          h(
            "div",
            { style: "flex:1;min-width:0" },
            h(
              "div",
              { style: "display:flex;align-items:center;gap:8px;flex-wrap:wrap" },
              h("span", { text: AGENT_LABELS[agent.source] || agent.display_name }),
              tag(status.text, status.tone === "mute" ? "mute" : status.tone),
              hookTag,
            ),
            h("div", { class: "path", text: path || "未找到数据目录", title: path }),
          ),
          installBtn,
        ),
      );
    }

    clear(body);
    body.append(content);
  }

  // ------------------------------------------------------------ 第二步

  function stepMethod() {
    const selfInput = h("input", {
      class: "input mono",
      placeholder: "https://homing.example.com",
      disabled: true,
      style: "margin-top:10px",
    });

    const make = (key: "lan" | "cloud" | "self_hosted", title: string, desc: string, badge: HTMLElement | null, disabled: boolean, extra?: HTMLElement) => {
      const el = h(
        "div",
        { class: `choice ${method === key ? "selected" : ""} ${disabled ? "disabled" : ""}`.trim() },
        h("span", { class: "choice-radio" }),
        h(
          "div",
          { class: "choice-body" },
          h("div", { class: "choice-title" }, h("span", { text: title }), badge),
          h("div", { class: "choice-desc", text: desc }),
          extra || null,
        ),
      );
      if (!disabled) {
        el.addEventListener("click", () => {
          method = key;
          paintStep();
        });
      }
      return el;
    };

    clear(body);
    body.append(
      h("h2", { text: "手机怎么连过来" }),
      h("p", { class: "wizard-sub", text: "先选一种；以后可以在「连接」页改。" }),
      make("lan", "局域网直连", "手机和这台电脑在同一个 Wi-Fi 下，直接连，不经过任何服务器。", tag("推荐 · 这一版默认", "ok"), false),
      make("cloud", "Homing 云中转", "不在同一个网络时，通过 Homing 的服务器转发。", tag("即将推出", "warn"), true),
      make("self_hosted", "自建服务器", "把 homingd 跑在自己的服务器上，手机连那台机器。", tag("这一版不可选", "mute"), true, selfInput),
      h("div", { class: "note", style: "margin-top:10px" }, "三种方式都会使用端到端加密；端到端加密在 M4 接入，这一版是局域网内的设备令牌鉴权。"),
    );
  }

  // ------------------------------------------------------------ 第三步

  function stepPair() {
    const qrBox = h("div", { class: "qr-box" }, h("div", { class: "qr-empty", text: "正在生成…" }));
    const codeText = h("div", { class: "code-display mono", text: "······" });
    const ttl = h("span", { class: "note", text: "" });
    const wait = h("div", { class: "waiting" });
    const addr = h("div", { class: "note mono", style: "margin-top:10px", text: "" });

    const regen = button("换一个", {
      small: true,
      variant: "quiet",
      iconSvg: icons.refresh,
      onClick: () => void newOffer(),
    });

    clear(body);
    body.append(
      h("h2", { text: "配对手机" }),
      h("p", { class: "wizard-sub", text: "用手机扫一下，或者在 App 里输入 6 位配对码。" }),
      h(
        "div",
        { class: "pair-grid" },
        h(
          "div",
          null,
          qrBox,
          h(
            "div",
            { style: "margin-top:12px" },
            button("下载安卓版", {
              small: true,
              variant: "quiet",
              iconSvg: icons.download,
              onClick: () => window.open("/apk", "_blank", "noopener"),
            }),
          ),
        ),
        h(
          "div",
          null,
          h("div", { class: "note", text: "6 位配对码 · 5 分钟内有效" }),
          codeText,
          h("div", { style: "display:flex;gap:10px;align-items:center;margin-bottom:14px" }, ttl, regen),
          h(
            "ol",
            { class: "steps" },
            h("li", { text: "在手机上打开 Homing App" }),
            h("li", { text: "进入「我的 › 添加电脑」" }),
            h("li", { text: "扫左边的二维码，或输入上面的 6 位配对码" }),
          ),
          wait,
          addr,
        ),
      ),
    );
    paintOfferInto({ qrBox, codeText, ttl, wait, addr });
    if (!offer) void newOffer({ qrBox, codeText, ttl, wait, addr });
  }

  interface PairRefs {
    qrBox: HTMLElement;
    codeText: HTMLElement;
    ttl: HTMLElement;
    wait: HTMLElement;
    addr: HTMLElement;
  }
  let pairRefs: PairRefs | null = null;

  function paintOfferInto(refs: PairRefs) {
    pairRefs = refs;
    paintOffer();
  }

  function paintOffer() {
    if (!pairRefs || !offer) return;
    const { qrBox, codeText, ttl, wait, addr } = pairRefs;
    codeText.textContent = offer.code.split("").join(" ");
    addr.textContent = offer.lan_url ? `地址 ${offer.lan_url}` : "";
    const left = Math.max(0, Math.round((new Date(offer.expires_at).getTime() - Date.now()) / 1000));
    ttl.textContent = claimed ? "" : left > 0 ? `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")} 后失效` : "已失效，点「换一个」";

    if (claimed) {
      wait.replaceChildren(tag("手机已配对", "ok"), h("span", { text: "搞定了，可以直接完成设置" }));
    } else if (left <= 0) {
      wait.replaceChildren(tag("配对码过期", "warn"));
    } else {
      wait.replaceChildren(h("span", { class: "spinner" }), h("span", { text: "等待手机扫码…" }));
    }

    if (offer.qr_payload && !qrBox.querySelector("img")) {
      void QRCode.toDataURL(offer.qr_payload, {
        errorCorrectionLevel: "M",
        margin: 1,
        width: 416,
        color: { dark: "#0f172a", light: "#f8fafc" },
      })
        .then((url) => qrBox.replaceChildren(h("img", { src: url, alt: "配对二维码", width: "208", height: "208" })))
        .catch(() => qrBox.replaceChildren(h("div", { class: "qr-empty", text: "二维码生成失败，直接输入配对码" })));
    }
  }

  async function newOffer(refs?: PairRefs) {
    try {
      offer = await endpoints.newOffer();
      claimed = false;
      if (refs) pairRefs = refs;
      paintOffer();
      if (offerTimer) window.clearInterval(offerTimer);
      offerTimer = window.setInterval(async () => {
        if (!offer || offer.status !== "waiting" || claimed) {
          paintOffer();
          return;
        }
        try {
          const fresh = await endpoints.offer(offer.offer_id);
          if (fresh.status === "claimed") {
            claimed = true;
            offer = { ...offer, ...fresh };
            paintOffer();
            paintFoot();
            toast("手机配对成功", "ok");
            if (offerTimer) window.clearInterval(offerTimer);
          } else {
            paintOffer();
          }
        } catch {
          /* 忽略单次失败 */
        }
      }, 2000);
    } catch (err) {
      reportError(err, "生成配对码失败");
    }
  }

  // ------------------------------------------------------------ 流程控制

  function paintStep() {
    paintProgress();
    paintFoot();
    if (step === 1) stepAgents();
    if (step === 2) stepMethod();
    if (step === 3) stepPair();
  }

  function go(next: Step) {
    step = next;
    paintStep();
  }

  async function finish() {
    try {
      await endpoints.saveSetup({ agents: [...selected], method });
      toast("设置完成", "ok");
      ctx.navigate("/console/");
    } catch (err) {
      reportError(err, "保存设置失败");
    }
  }

  async function loadAgents() {
    try {
      const [agentPayload, pathPayload, info] = await Promise.all([
        endpoints.agents(),
        endpoints.agentPaths().catch(() => null),
        endpoints.connection().catch(() => null),
      ]);
      if (disposed) return;
      agents = agentPayload.agents;
      paths = pathPayload;
      connection = info;
      for (const agent of agents) {
        if (agent.installed !== false) selected.add(agent.source);
      }
      paintStep();
    } catch (err) {
      reportError(err, "读取 agent 失败");
    }
  }

  paintProgress();
  paintFoot();
  void loadAgents();

  return {
    el: root,
    dispose: () => {
      disposed = true;
      if (offerTimer) window.clearInterval(offerTimer);
    },
  };
}
