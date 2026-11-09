// 扩展 › 接入：内置 / 发现 / 已安装 / 自定义
// 自定义页的清单解析、/api/ingest、脚本适配器加载由 relay 侧（DSH）接上，这里先把界面和三张方式卡片做完。

import { h, clear, icon } from "../lib/dom";
import { icons } from "../lib/icons";
import { AGENT_INITIALS } from "../lib/format";
import { endpoints, type AdaptersPayload, type AdapterTestResult, type CustomAdapter } from "../lib/api";
import { button, emptyState, groupHead, listCard, reportError, tag, toast } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

export type AdapterTab = "builtin" | "discover" | "installed" | "custom";

const TAB_LABELS: Array<{ key: AdapterTab; label: string; path: string }> = [
  { key: "builtin", label: "内置", path: "/console/adapters" },
  { key: "discover", label: "发现", path: "/console/adapters/discover" },
  { key: "installed", label: "已安装", path: "/console/adapters/installed" },
  { key: "custom", label: "自定义", path: "/console/adapters/custom" },
];

const FORMAT_LABELS: Record<string, string> = {
  jsonl: "JSON Lines（一行一条）",
  json: "JSON 数组",
  sqlite: "SQLite 库",
  markdown: "Markdown 聊天记录",
  log: "纯文本日志",
};

const PUSH_LABELS: Record<string, string> = {
  hook: "通知钩子",
  watch: "监听文件",
  poll: "轮询",
};

const PUSH_VALUE: Record<string, string> = { 通知钩子: "hook", 监听文件: "watch", 轮询: "poll" };

/** 清单 id 只留 ASCII：URL 里要能带着它删改，中文名落成稳定哈希 */
function slug(input: string): string {
  const raw = input.trim().toLowerCase();
  const ascii = raw.replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  if (ascii) return ascii;
  let hash = 0;
  for (const ch of raw) hash = (hash * 31 + ch.codePointAt(0)!) % 0xffffffff;
  return `adapter-${hash.toString(36)}`;
}

export function renderAdapters(ctx: PageContext, tab: AdapterTab): PageResult {
  const root = h("div");
  let disposed = false;
  let payload: AdaptersPayload | null = null;
  const host = h("div");

  root.append(buildTabs(tab), host);

  function buildTabs(active: AdapterTab): HTMLElement {
    const wrap = h("div", { class: "segmented", style: "margin-bottom:16px" });
    for (const item of TAB_LABELS) {
      const btn = h("button", { type: "button", class: item.key === active ? "on" : "", text: item.label });
      btn.addEventListener("click", () => ctx.navigate(item.path));
      wrap.append(btn);
    }
    return wrap;
  }

  // ------------------------------------------------------------ 内置

  function paintBuiltin(data: AdaptersPayload) {
    const rows = data.builtin.map((item) =>
      h(
        "div",
        { class: "lrow" },
        h("span", { class: "agent-square", text: AGENT_INITIALS[item.id] || item.id.slice(0, 1).toUpperCase(), style: "margin-top:2px" }),
        h(
          "div",
          { class: "lrow-main" },
          h(
            "div",
            { class: "lrow-title" },
            h("span", { class: "txt", text: item.name }),
            item.connected ? tag("已接入", "ok") : tag("未接入", "mute"),
          ),
          h(
            "div",
            { class: "lrow-sub" },
            h("span", { class: "mono", text: item.push.map((signal) => PUSH_LABELS[signal] || signal).join(" / ") }),
            ` · 回复：${item.reply}`,
            item.config_file ? ` · ${item.config_file}` : " · 不改配置文件",
          ),
        ),
        h(
          "div",
          { class: "lrow-actions" },
          item.connected
            ? button("管理", { small: true, variant: "quiet", onClick: () => ctx.navigate("/console/agents") })
            : button("去接入", { small: true, variant: "primary", onClick: () => ctx.navigate("/console/agents") }),
        ),
      ),
    );

    clear(host);
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("Homing 自带的接入", data.builtin.length),
        listCard(...rows),
        h(
          "div",
          { class: "note", style: "margin-top:8px" },
          "内置接入选的是「读它自己的会话文件 + 能回就回」，不需要装插件。要接别的 agent，用「自定义」。",
        ),
      ),
    );
  }

  // ------------------------------------------------------------ 发现

  function paintDiscover(data: AdaptersPayload) {
    clear(host);
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("社区适配器", data.community.length),
        listCard(
          ...data.community.map((item) =>
            h(
              "div",
              { class: "lrow" },
              h("span", { class: "agent-square", text: item.name.slice(0, 1).toUpperCase(), style: "margin-top:2px" }),
              h(
                "div",
                { class: "lrow-main" },
                h("div", { class: "lrow-title" }, h("span", { class: "txt", text: item.name }), tag("社区", "plain")),
                h("div", { class: "lrow-sub", text: item.note }),
              ),
              h(
                "div",
                { class: "lrow-actions" },
                button("还不能装", { small: true, variant: "quiet", disabled: true, title: "适配器市场随 ingest 一起上" }),
              ),
            ),
          ),
        ),
        h(
          "div",
          { class: "note", style: "margin-top:8px" },
          "社区适配器要等 relay 的 ingest 与安装通道接上才能装；现在可以先用「自定义」写一份清单。",
        ),
      ),
    );
  }

  // ------------------------------------------------------------ 已安装

  function paintInstalled(data: AdaptersPayload) {
    clear(host);
    const builtin = data.builtin.filter((item) => item.connected);
    const custom = data.custom;

    if (builtin.length === 0 && custom.length === 0) {
      host.append(h("div", { class: "list-card" }, emptyState("还没有安装任何接入", "去「内置」或「自定义」接一个")));
      return;
    }

    const rows: HTMLElement[] = [];
    for (const item of builtin) {
      rows.push(
        h(
          "div",
          { class: "lrow" },
          h("span", { class: "agent-square", text: AGENT_INITIALS[item.id] || item.id.slice(0, 1).toUpperCase(), style: "margin-top:2px" }),
          h(
            "div",
            { class: "lrow-main" },
            h("div", { class: "lrow-title" }, h("span", { class: "txt", text: item.name }), tag("内置", "ok")),
            h("div", { class: "lrow-sub" }, h("span", { class: "mono", text: item.home || "—" })),
          ),
          h("div", { class: "lrow-actions" }, button("管理", { small: true, variant: "quiet", onClick: () => ctx.navigate("/console/agents") })),
        ),
      );
    }
    for (const item of custom) {
      rows.push(customRow(item));
    }
    host.append(
      h(
        "div",
        { class: "group" },
        groupHead("已安装", builtin.length + custom.length),
        listCard(...rows),
        h(
          "div",
          { class: "note", style: "margin-top:8px" },
          `内置接入跟着 agent 走；自定义接入的清单文件放在 ${data.root}，删掉文件就是卸载。`,
        ),
      ),
    );
  }

  function customRow(item: CustomAdapter): HTMLElement {
    return h(
      "div",
      { class: "lrow" },
      h("span", { class: "agent-square", text: (item.name || item.id).slice(0, 1).toUpperCase(), style: "margin-top:2px" }),
      h(
        "div",
        { class: "lrow-main" },
        h(
          "div",
          { class: "lrow-title" },
          h("span", { class: "txt", text: item.name || item.id }),
          tag("自定义", "warn"),
          item.broken ? tag("清单有问题", "bad") : null,
        ),
        h(
          "div",
          { class: "lrow-sub" },
          h("span", { class: "mono", text: item.source?.path || "—" }),
          ` · ${FORMAT_LABELS[item.source?.format || ""] || item.source?.format || "—"} · ${PUSH_LABELS[item.push?.method || ""] || "轮询"}`,
        ),
      ),
      h(
        "div",
        { class: "lrow-actions" },
        button("删除", {
          small: true,
          variant: "quiet",
          onClick: async () => {
            const ok = window.confirm(`删除 ${item.name || item.id} 的接入清单？文件会从 ~/.homing/agents 里删掉。`);
            if (!ok) return;
            try {
              await endpoints.removeAdapter(item.id);
              toast("已删除", "ok");
              await load();
            } catch (err) {
              reportError(err, "删除失败");
            }
          },
        }),
      ),
    );
  }

  // ------------------------------------------------------------ 自定义

  function paintCustom(data: AdaptersPayload) {
    clear(host);

    const state = {
      method: "manifest" as "events" | "manifest" | "script",
      name: "",
      kind: "file",
      path: "",
      format: "jsonl",
      push: "poll",
      command: "",
      test: null as AdapterTestResult | null,
    };

    const yamlBox = h("pre", { class: "yaml" });
    const testHost = h("div");
    const formHost = h("div", { class: "adapter-form" });

    function currentManifest() {
      const command = state.command.trim();
      return {
        id: slug(state.name || "adapter"),
        name: state.name || "未命名接入",
        source: { kind: state.kind, path: state.path || "~/", format: state.format },
        push: { method: state.push },
        // 没写回复命令就是只读，别硬报「命令不能为空」
        reply: { mode: command ? "command" : "none", command },
      };
    }

    function paintYaml() {
      const manifest = currentManifest();
      const lines: Array<[string, string]> = [
        ["#", "保存到 ~/.homing/agents/"],
        ["id", manifest.id],
        ["name", manifest.name],
        ["source:", ""],
        ["  kind", manifest.source.kind],
        ["  path", manifest.source.path],
        ["  format", manifest.source.format],
        ["push:", ""],
        ["  method", manifest.push.method],
        ["reply:", ""],
        ["  mode", manifest.reply.mode],
        ...(manifest.reply.mode === "command" ? [["  command", manifest.reply.command] as [string, string]] : []),
      ];
      clear(yamlBox);
      for (const [key, value] of lines) {
        if (key === "#") {
          yamlBox.append(h("span", { class: "c", text: `# ${value}${manifest.id}.yaml` }), document.createTextNode("\n"));
          continue;
        }
        if (value === "") {
          yamlBox.append(h("span", { class: "k", text: key }), document.createTextNode("\n"));
          continue;
        }
        yamlBox.append(h("span", { class: "k", text: key }), document.createTextNode(": "), h("span", { class: "v", text: value }), document.createTextNode("\n"));
      }
    }

    function field(label: string, control: HTMLElement, hint?: string): HTMLElement {
      return h("div", { class: "field" }, h("div", { class: "field-label", text: label }), control, hint ? h("div", { class: "hint", text: hint }) : null);
    }

    const nameInput = h("input", { class: "input", placeholder: "比如 Cursor / 我的脚本", value: state.name });
    nameInput.addEventListener("input", () => {
      state.name = (nameInput as HTMLInputElement).value;
      paintYaml();
    });

    const kindSelect = h("select", { class: "select" });
    for (const [value, label] of [
      ["file", "会话文件 / 目录"],
      ["process", "进程输出"],
    ]) {
      const option = h("option", { value, text: label });
      if (value === state.kind) option.selected = true;
      kindSelect.append(option);
    }
    kindSelect.addEventListener("change", () => {
      state.kind = (kindSelect as HTMLSelectElement).value;
      paintYaml();
    });

    const pathInput = h("input", { class: "input mono", placeholder: "~/myagent/sessions", value: state.path });
    pathInput.addEventListener("input", () => {
      state.path = (pathInput as HTMLInputElement).value;
      paintYaml();
    });

    const formatSelect = h("select", { class: "select" });
    for (const [value, label] of Object.entries(FORMAT_LABELS)) {
      const option = h("option", { value, text: label });
      if (value === state.format) option.selected = true;
      formatSelect.append(option);
    }
    formatSelect.addEventListener("change", () => {
      state.format = (formatSelect as HTMLSelectElement).value;
      paintYaml();
    });

    const pushSelect = h("select", { class: "select" });
    for (const [label, value] of Object.entries(PUSH_VALUE)) {
      const option = h("option", { value, text: label });
      if (value === state.push) option.selected = true;
      pushSelect.append(option);
    }
    pushSelect.addEventListener("change", () => {
      state.push = (pushSelect as HTMLSelectElement).value;
      paintYaml();
    });

    const commandInput = h("input", { class: "input mono", placeholder: "myagent send --session {session_id} --text {text}", value: state.command });
    commandInput.addEventListener("input", () => {
      state.command = (commandInput as HTMLInputElement).value;
      paintYaml();
    });

    const testBtn = button("测试读取", {
      small: true,
      iconSvg: icons.bolt,
      onClick: async () => {
        if (!state.path.trim()) {
          toast("先填文件位置", "bad");
          return;
        }
        testBtn.disabled = true;
        try {
          const result = await endpoints.testAdapter({ path: state.path, format: state.format });
          state.test = result;
          paintTest();
        } catch (err) {
          reportError(err, "测试读取失败");
        } finally {
          testBtn.disabled = false;
        }
      },
    });

    function paintTest() {
      clear(testHost);
      const result = state.test;
      if (!result) return;
      if (!result.ok) {
        testHost.append(
          h(
            "div",
            { class: "test-result bad" },
            h("div", { text: result.error || "读不到" }),
            result.checked ? h("div", { class: "mono", style: "margin-top:4px", text: result.checked }) : null,
          ),
        );
        return;
      }
      testHost.append(
        h(
          "div",
          { class: "test-result" },
          h(
            "div",
            null,
            `读到 ${result.file_count} 个文件，共 ${((result.total_size || 0) / 1024).toFixed(1)} KB · ${result.is_dir ? "目录" : "单个文件"} · ${FORMAT_LABELS[result.format || ""] || result.format}`,
          ),
          ...(result.samples || []).map((sample) =>
            h(
              "div",
              { style: "margin-top:8px" },
              h("div", { class: "mono", text: sample.file }),
              h("div", { class: "mono", style: "color:#94a3b8;white-space:pre-wrap;word-break:break-all", text: sample.preview || "（空文件）" }),
            ),
          ),
          result.note ? h("div", { class: "hint", text: result.note }) : null,
        ),
      );
    }

    const saveBtn = button("保存并接入", {
      variant: "primary",
      iconSvg: icons.check,
      onClick: async () => {
        try {
          const result = await endpoints.saveAdapter(currentManifest());
          if (!result.ok) {
            toast((result.errors || ["保存失败"]).join("；"), "bad", 6000);
            return;
          }
          toast("已保存并从 ~/.homing/agents 加载", "ok");
          ctx.navigate("/console/adapters/installed");
        } catch (err) {
          reportError(err, "保存失败");
        }
      },
    });

    const methods = h("div", { class: "method-grid" });
    const methodDefs = [
      { key: "events" as const, icon: icons.bolt, title: "事件上报", desc: "让你的 agent 在关键节点 POST 到本机接口，最实时。" },
      { key: "manifest" as const, icon: icons.fileText, title: "适配清单", desc: "填几个字段，Homing 自己去读会话文件。", badge: "推荐" },
      { key: "script" as const, icon: icons.terminal, title: "脚本适配器", desc: "写个脚本输出标准事件，最灵活。" },
    ];
    for (const def of methodDefs) {
      const el = h(
        "div",
        { class: state.method === def.key ? "method on" : "method" },
        h(
          "div",
          { class: "method-top" },
          icon(def.icon, 15),
          h("span", { text: def.title }),
          def.badge ? tag(def.badge, "ok") : null,
        ),
        h("div", { class: "method-desc", text: def.desc }),
      );
      el.addEventListener("click", () => {
        state.method = def.key;
        for (const child of Array.from(methods.children)) child.classList.remove("on");
        el.classList.add("on");
        paintForm();
      });
      methods.append(el);
    }

    function paintForm() {
      clear(formHost);
      if (state.method !== "manifest") {
        formHost.append(
          h(
            "div",
            { style: "grid-column:1 / -1" },
            h(
              "div",
              { class: "list-card", style: "padding:16px" },
              h(
                "div",
                { class: "note", style: "line-height:1.8" },
                state.method === "events"
                  ? `让 agent 在需要你决定的时候往 http://${location.host}/hook/generic 发一条 JSON，Homing 就会把它变成一条收件箱事件。具体字段和鉴权等 relay 的 /api/ingest 接上后一起放开。`
                  : "脚本适配器：写一个可执行脚本，按约定输出事件 JSON；Homing 定时跑它，把输出当成事件。加载器由 relay 侧实现，这一版先把入口放这里。",
              ),
            ),
          ),
        );
        return;
      }

      formHost.append(
        h(
          "div",
          null,
          field("名称", nameInput),
          field("会话来源", kindSelect),
          field("文件位置", pathInput, "目录或单个文件都行，支持 ~"),
          field("记录格式", formatSelect),
          field("推送方式", pushSelect, "钩子最及时；没有钩子的 agent 用监听文件或轮询"),
          field("回复命令", commandInput, "留空就是只读；{session_id} 和 {text} 会被替换"),
          h(
            "div",
            { style: "display:flex;gap:8px;align-items:center;margin-top:4px" },
            testBtn,
            saveBtn,
          ),
          h("div", { style: "margin-top:12px" }, testHost),
        ),
        h("div", null, h("div", { class: "yaml-head" }, h("span", { text: "接入清单预览" }), h("span", { class: "mono", text: data.root })), yamlBox, h("div", { class: "hint", text: "保存后这份 YAML 会写到上面的目录，可以直接手改。" })),
      );
    }

    host.append(
      methods,
      formHost,
      h(
        "div",
        { class: "note", style: "margin-top:14px" },
        "自定义接入的后端（清单解析、/api/ingest、脚本适配器加载）由 relay 那边接上；现在这三个接口是占位，清单文件会真的写进 ~/.homing/agents。",
      ),
    );

    paintYaml();
    paintForm();

    if (data.custom.length > 0) {
      host.append(
        h(
          "div",
          { class: "group", style: "margin-top:18px" },
          groupHead("已有的自定义接入", data.custom.length),
          listCard(...data.custom.map((item) => customRow(item))),
        ),
      );
    }
  }

  // ------------------------------------------------------------ 加载

  async function load() {
    try {
      payload = await endpoints.adapters();
      if (disposed || !payload) return;
      const data = payload;
      if (tab === "builtin") paintBuiltin(data);
      else if (tab === "discover") paintDiscover(data);
      else if (tab === "installed") paintInstalled(data);
      else paintCustom(data);
    } catch (err) {
      if (disposed) return;
      clear(host);
      host.append(h("div", { class: "list-card" }, h("div", { class: "empty" }, h("div", { text: "读不到接入列表" }), h("div", { class: "note", text: (err as Error).message }))));
    }
  }

  clear(host);
  host.append(h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: "正在读取接入…" })));
  void load();

  return {
    el: root,
    dispose: () => {
      disposed = true;
    },
  };
}
