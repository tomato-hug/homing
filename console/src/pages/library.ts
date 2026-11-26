// 扩展 › 资源库：本机技能、指令、hooks 与接入套件的统一抽屉。

import { append, h, clear, icon } from "../lib/dom";
import {
  endpoints,
  type LibraryAuditEntry,
  type LibraryDetail,
  type LibraryImportPlan,
  type LibraryInstallation,
  type LibraryKind,
  type LibraryPackInstallation,
  type LibraryPackPlan,
  type LibraryPublishCheck,
  type LibraryRemotePlan,
  type LibraryRemoteProvider,
  type LibraryResource,
  type LibrarySnapshot,
  type LibrarySource,
} from "../lib/api";
import { AGENT_LABELS } from "../lib/format";
import { icons } from "../lib/icons";
import {
  button,
  emptyState,
  listCard,
  reportError,
  segmented,
  tag,
  toast,
} from "../lib/ui";
import { confirmDialog, promptDialog } from "../lib/dialog";
import { loc } from "../i18n";
import type { PageContext, PageResult } from "../main";

const KIND_LABELS: Record<LibraryKind, string> = {
  skill: "技能",
  instructions: "指令",
  mcp: "MCP",
  hook: "Hook",
  workflow: "工作流",
  plugin: "插件",
  context: "上下文",
  pack: "套件",
};

const SOURCE_LABEL: Record<string, string> = {
  homing: "Homing",
  agents: "Agent Skills",
};

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(value < 10 * 1024 ? 1 : 0)} KB`;
  return `${(value / 1024 / 1024).toFixed(1)} MB`;
}

function prettyTime(value: string): string {
  const at = Date.parse(value);
  if (!Number.isFinite(at)) return "—";
  const seconds = Math.max(1, Math.round((Date.now() - at) / 1000));
  if (seconds < 60) return "刚刚";
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)} 小时前`;
  return `${Math.round(seconds / 86400)} 天前`;
}

function targetLabel(agent: string): string {
  return SOURCE_LABEL[agent] || AGENT_LABELS[agent] || agent;
}

function closeButton(onClose: () => void): HTMLButtonElement {
  const close = h("button", { class: "icon-btn", type: "button", title: "关闭", "aria-label": "关闭" });
  close.append(icon(icons.close, 15));
  close.addEventListener("click", onClose);
  return close;
}

export function renderLibrary(_ctx: PageContext): PageResult {
  const root = h("div", { class: "library-page" });
  let disposed = false;
  let snapshot: LibrarySnapshot | null = null;
  let tab: "discover" | "installed" | "packs" | "publish" | "sources" | "rollback" | "audit" = "discover";
  let kind = "";
  let query = "";
  let drawerOpen = false;
  let selected: LibraryDetail | null = null;
  let selectedError = "";
  let importOpen = false;
  let importMode: "local" | "remote" = "local";
  let importPath = "";
  let importURL = "";
  let importRef = "";
  let importKind = "";
  let importName = "";
  let importPlan: LibraryImportPlan | null = null;
  let remotePlan: LibraryRemotePlan | null = null;
  let importError = "";
  let importing = false;
  let providers: LibraryRemoteProvider[] = [];
  let installations: LibraryInstallation[] = [];
  let installationsLoaded = false;
  let packPath = "";
  let packPlan: LibraryPackPlan | null = null;
  let packError = "";
  let packPlanning = false;
  let packs: LibraryPackInstallation[] = [];
  let packsLoaded = false;
  let auditEntries: LibraryAuditEntry[] = [];
  let auditLoaded = false;
  let publishPath = "";
  let publishKind = "";
  let publishChecking = false;
  let publishError = "";
  let publishResult: LibraryPublishCheck | null = null;

  const metrics = h("div", { class: "agent-library-metrics" });
  const toolbar = h("div", { class: "library-toolbar" });
  const search = h("input", {
    class: "input",
    type: "search",
    placeholder: "搜索资源、来源或说明…",
    "aria-label": "搜索资源",
  });
  search.addEventListener("input", () => {
    query = (search as HTMLInputElement).value.trim();
    paint();
  });
  const content = h("div", { class: "library-content" });
  const drawer = h("aside", { class: "library-drawer", hidden: true });
  const overlay = h("div", { class: "library-overlay", hidden: true });
  const importPanel = h("section", { class: "library-import", hidden: true });

  const head = h(
    "div",
    { class: "agent-library-head compact library-head" },
    h(
      "div",
      { class: "agent-library-copy" },
      h("h1", { text: "资源库" }),
      h("p", { text: "本机能力索引。导入前预览，写入可回滚。" }),
    ),
    metrics,
  );

  function paintMetrics() {
    clear(metrics);
    const total = snapshot?.total || 0;
    metrics.hidden = total === 0;
    if (!total) return;
    const managed = snapshot?.managed || 0;
    const skills = snapshot?.counts.skill || 0;
    const sources = snapshot?.sources.filter((source) => source.exists).length || 0;
    for (const [value, label] of [
      [total, "资源"],
      [skills, "技能"],
      [managed, "Homing 管理"],
      [sources, "来源"],
    ] as Array<[number, string]>) {
      metrics.append(
        h(
          "div",
          { class: "agent-library-metric" },
          h("strong", { text: String(value) }),
          h("span", { text: label }),
        ),
      );
    }
  }

  function paintToolbar() {
    clear(toolbar);
    const tabs = segmented(
      [
        { value: "discover", label: "探索" },
        { value: "installed", label: "已安装" },
        { value: "packs", label: "套件" },
        { value: "publish", label: "发布" },
        { value: "sources", label: "来源" },
        { value: "rollback", label: "安装记录" },
        { value: "audit", label: "审计" },
      ],
      tab,
      (next) => {
        tab = next;
        if (tab === "audit" && !auditLoaded) void loadAudit();
        if (tab === "rollback" && !installationsLoaded) void loadInstallations();
        if (tab === "packs" && !packsLoaded) void loadPacks();
        paintToolbar();
        paint();
      },
    );
    tabs.classList.add("library-tabs");
    toolbar.append(tabs);
    if (tab === "discover" || tab === "installed") {
      toolbar.append(search);
      const filter = h("div", { class: "segmented library-kinds" });
      const kinds: Array<[string, string]> = [["", "全部"], ...Object.entries(KIND_LABELS)];
      for (const [value, label] of kinds) {
        const buttonEl = h("button", {
          type: "button",
          class: kind === value ? "on" : "",
          text: label,
        });
        buttonEl.addEventListener("click", () => {
          kind = value;
          paintToolbar();
          paint();
        });
        filter.append(buttonEl);
      }
      toolbar.append(filter);
    }
    const hasResources = Boolean(snapshot?.resources.length);
    if ((tab === "discover" || tab === "installed") && hasResources) {
      const spacer = h("div", { class: "spacer", style: "flex:1" });
      const importButton = button("导入本机资源", {
        variant: "primary",
        iconSvg: icons.plus,
        onClick: () => {
          importOpen = true;
          importMode = "local";
          importPlan = null;
          importError = "";
          paintImport();
        },
      });
      const remoteButton = button("从 URL 安装", {
        variant: "ghost",
        iconSvg: icons.plug,
        onClick: () => {
          importOpen = true;
          importMode = "remote";
          remotePlan = null;
          importError = "";
          paintImport();
        },
      });
      toolbar.append(spacer, remoteButton, importButton);
    }
  }

  function filtered(): LibraryResource[] {
    const resources = snapshot?.resources || [];
    return resources.filter((resource) => {
      if (tab === "installed" && !resource.removable && resource.targets.every((target) => !target.managed)) {
        return false;
      }
      if (kind && resource.kind !== kind) return false;
      if (!query) return true;
      const haystack = [
        resource.name,
        resource.description,
        resource.source,
        resource.kind,
        ...resource.targets.map((target) => `${target.agent} ${target.path}`),
      ]
        .join(" ")
        .toLowerCase();
      return haystack.includes(query.toLowerCase());
    });
  }

  function resourceCard(resource: LibraryResource): HTMLElement {
    const targetRow = h("div", { class: "library-targets" });
    for (const target of resource.targets.slice(0, 5)) {
      targetRow.append(
        h(
          "span",
          { class: "library-target", title: target.path },
          h("span", { class: "dot dot-ok" }),
          h("span", { text: targetLabel(target.agent) }),
          target.managed ? h("span", { class: "library-managed", text: "管理" }) : null,
        ),
      );
    }
    if (resource.targets.length > 5) {
      targetRow.append(h("span", { class: "library-target muted", text: `+${resource.targets.length - 5}` }));
    }
    return h(
      "article",
      { class: "library-card" },
      h(
        "div",
        { class: "library-card-head" },
        h("span", { class: `library-kind kind-${resource.kind}`, text: KIND_LABELS[resource.kind] || resource.kind }),
        h("div", { class: "spacer", style: "flex:1" }),
        resource.removable ? tag("可回滚", "ok") : tag("Agent 原生", "mute"),
      ),
      h("h3", { text: resource.name }),
      h("p", { text: resource.description || "没有说明文件；可以打开详情查看实际文件。" }),
      targetRow,
      h(
        "div",
        { class: "library-card-foot" },
        h("span", { class: "mono", text: `${resource.file_count} 个文件 · ${formatBytes(resource.size_bytes)}` }),
        h("span", { class: "muted", text: prettyTime(resource.modified_at) }),
        h("div", { class: "spacer", style: "flex:1" }),
        button("详情", {
          variant: "quiet",
          small: true,
          onClick: () => void openDetail(resource),
        }),
        resource.removable
          ? button("移除", {
              variant: "danger",
              small: true,
              onClick: () => void removeResource(resource),
            })
          : null,
      ),
    );
  }

  function sourceCard(source: LibrarySource): HTMLElement {
    return h(
      "article",
      { class: source.exists ? "library-source-card" : "library-source-card missing" },
      h(
        "div",
        { class: "library-source-head" },
        h("span", { class: source.exists ? "dot dot-ok" : "dot" }),
        h("strong", { text: source.label }),
        h("div", { class: "spacer", style: "flex:1" }),
        source.managed ? tag("Homing 管理", "ok") : tag("只读扫描", "mute"),
      ),
      h("div", { class: "mono library-source-path", text: source.path }),
      h("div", { class: "library-source-meta" },
        h("span", { text: KIND_LABELS[source.kind] || source.kind }),
        h("span", { text: source.exists ? `${source.count} 个资源` : "目录不存在" }),
      ),
      source.note ? h("p", { text: source.note }) : null,
    );
  }

  function paint() {
    if (!snapshot) return;
    paintMetrics();
    clear(content);
    if (tab === "publish") {
      content.append(publishView());
      return;
    }
    if (tab === "sources") {
      const remote = h(
        "section",
        { class: "library-source-section" },
        h("div", { class: "section-title" }, h("span", { text: "远端来源" })),
        h(
          "div",
          { class: "library-source-grid" },
          ...providers.map((provider) =>
            h(
              "article",
              { class: "library-source-card" },
              h(
                "div",
                { class: "library-source-head" },
                h("span", { class: provider.ready ? "dot dot-ok" : "dot dot-warn" }),
                h("strong", { text: provider.label }),
                h("div", { class: "spacer", style: "flex:1" }),
                tag(provider.ready ? "可用" : "未启用", provider.ready ? "ok" : "mute"),
              ),
              h("div", { class: "mono library-source-path", text: provider.example }),
              h("p", { text: provider.note }),
              button("用这个来源安装", {
                small: true,
                variant: "quiet",
                onClick: () => {
                  importOpen = true;
                  importMode = "remote";
                  importURL = provider.example;
                  remotePlan = null;
                  importError = "";
                  paintImport();
                },
              }),
            ),
          ),
        ),
      );
      const local = h(
        "section",
        { class: "library-source-section" },
        h("div", { class: "section-title" }, h("span", { text: "本机来源" })),
      );
      const grid = h("div", { class: "library-source-grid" });
      for (const source of snapshot.sources) grid.append(sourceCard(source));
      local.append(grid);
      content.append(remote, local);
      return;
    }
    if (tab === "packs") {
      content.append(packsView());
      return;
    }
    if (tab === "rollback") {
      content.append(installationsView());
      return;
    }
    if (tab === "audit") {
      content.append(auditView());
      return;
    }
    const resources = filtered();
    if (!resources.length) {
      const localAction = () => {
        importOpen = true;
        importMode = "local";
        importPlan = null;
        remotePlan = null;
        importError = "";
        paintImport();
      };
      const remoteAction = () => {
        importOpen = true;
        importMode = "remote";
        remotePlan = null;
        importPlan = null;
        importError = "";
        paintImport();
      };
      content.append(
        h(
          "div",
          { class: "list-card library-empty" },
          h(
            "div",
            { class: "empty" },
            h("div", { text: query || kind ? "没有匹配的资源" : "还没有可展示的资源" }),
            h("div", {
              class: "note",
              text: query || kind ? "换一个关键词或类型试试。" : "从一个带 SKILL.md 的资料夹开始，或先预览远端来源。",
            }),
          ),
          h(
            "div",
            { class: "library-empty-actions" },
            button("导入本机资源", {
              variant: "primary",
              iconSvg: icons.plus,
              onClick: localAction,
            }),
            button("从 URL 安装", {
              variant: "quiet",
              iconSvg: icons.download,
              onClick: remoteAction,
            }),
          ),
        ),
      );
      return;
    }
    const grid = h("div", { class: "library-grid" });
    for (const resource of resources) grid.append(resourceCard(resource));
    content.append(grid);
  }

  function packsView(): HTMLElement {
    const wrap = h("div", { class: "library-packs" });
    const pathInput = h("input", {
      class: "input mono",
      placeholder: "包含 homing.pack.yaml 的本机套件目录",
      value: packPath,
    });
    pathInput.addEventListener("input", () => {
      packPath = (pathInput as HTMLInputElement).value;
      packPlan = null;
      packError = "";
    });
    const hero = h(
      "section",
      { class: "library-pack-hero" },
      h(
        "div",
        { class: "library-pack-copy" },
        h("div", { class: "eyebrow", text: "MULTI-RESOURCE TRANSACTION" }),
        h("h2", { text: "Agent Pack 套件" }),
        h("p", {
          text: "一个 manifest 可包含多个 skill、指令与 hook。先解析依赖、Agent layout、checksum 与冲突，再一次性 staging；任何一步失败都不会留下半套资源。",
        }),
      ),
      h(
        "div",
        { class: "library-pack-form" },
        h("label", null, h("span", { text: "套件目录" }), pathInput),
        h(
          "div",
          { class: "library-pack-form-actions" },
          button(packPlanning ? "正在校验…" : "生成安装计划", {
            variant: "primary",
            iconSvg: icons.check,
            disabled: packPlanning,
            onClick: () => void doPackPlan(),
          }),
        ),
      ),
    );
    wrap.append(hero);
    if (packError) wrap.append(h("div", { class: "library-warning", text: packError }));
    if (packPlan) wrap.append(packPlanView(packPlan));

    wrap.append(
      h(
        "div",
        { class: "section-title library-pack-installed-title" },
        h("span", { text: "已安装套件" }),
        h("span", { class: "muted", text: packsLoaded ? `${packs.length} 个事务记录` : "读取中…" }),
      ),
    );
    if (!packsLoaded) {
      wrap.append(
        h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: "正在读取套件安装记录…" })),
      );
    } else if (!packs.length) {
      wrap.append(
        h(
          "div",
          { class: "list-card" },
          emptyState(
            "还没有 Agent Pack 安装记录",
            "在 homing.pack.yaml 中声明多个资源与 targets，预览后一次安装。",
          ),
        ),
      );
    } else {
      wrap.append(
        listCard(
          ...packs.map((pack) => {
            const memberCount = pack.members.reduce((sum, member) => sum + member.targets.length, 0);
            return h(
              "div",
              { class: "library-pack-record" },
              h(
                "div",
                { class: "library-pack-record-main" },
                h(
                  "div",
                  { class: "library-plan-head" },
                  h("strong", { text: pack.name }),
                  tag(pack.state === "installed" ? "已安装" : "已回滚", pack.state === "installed" ? "ok" : "mute"),
                  pack.version ? tag(`v${pack.version}`, "mute") : null,
                ),
                h("div", { class: "lrow-sub mono", text: pack.pack_id }),
                h(
                  "div",
                  { class: "lrow-sub" },
                  h("span", {
                    text: `${pack.members.length} 个资源 · ${memberCount} 个投放目录 · ${prettyTime(
                      pack.state === "installed" ? pack.installed_at : pack.rolled_back_at || pack.installed_at,
                    )}`,
                  }),
                ),
                h(
                  "div",
                  { class: "library-pack-targets" },
                  ...pack.members.flatMap((member) =>
                    member.targets.map((target) =>
                      h(
                        "span",
                        { class: "library-target", title: target.destination },
                        h("span", { class: "dot dot-ok" }),
                        h("span", { text: targetLabel(target.agent) }),
                        h("span", { class: "mono", text: member.name }),
                      ),
                    ),
                  ),
                ),
                pack.missing?.length
                  ? h("div", { class: "library-warning", text: `回滚时 ${pack.missing.length} 个目录已不存在` })
                  : null,
              ),
              pack.state === "installed"
                ? button("回滚整套", {
                    variant: "danger",
                    small: true,
                    onClick: () => void rollbackPack(pack),
                  })
                : h("span", { class: "muted mono", text: pack.trash_path || "已移入回收区" }),
            );
          }),
        ),
      );
    }
    return wrap;
  }

  function packPlanView(plan: LibraryPackPlan): HTMLElement {
    const card = h(
      "section",
      { class: "library-pack-plan" },
      h(
        "div",
        { class: "library-pack-plan-head" },
        h(
          "div",
          null,
          h("div", { class: "eyebrow", text: "DRY RUN · NOTHING WRITTEN" }),
          h("h3", { text: plan.pack.metadata.name }),
          h("p", { text: plan.pack.metadata.description || "套件没有附加说明。" }),
        ),
        h("div", { class: "spacer", style: "flex:1" }),
        tag(`v${plan.pack.metadata.version}`, "ok"),
        tag(`${plan.members.length} 资源`, "mute"),
        tag(`${plan.total_files} 文件`, "mute"),
        tag(formatBytes(plan.total_bytes), "mute"),
      ),
      h("div", { class: "library-pack-root mono", text: plan.root }),
      h(
        "div",
        { class: "library-pack-order" },
        h("span", { text: "依赖顺序" }),
        h("strong", { class: "mono", text: plan.order.join(" → ") }),
      ),
      h(
        "div",
        { class: "library-pack-members" },
        ...plan.members.map((member, index) =>
          h(
            "article",
            { class: "library-pack-member" },
            h(
              "div",
              { class: "library-pack-member-head" },
              h("span", { class: "library-pack-step", text: String(index + 1).padStart(2, "0") }),
              h("strong", { text: member.name }),
              tag(KIND_LABELS[member.kind] || member.kind, "mute"),
              member.requires?.length
                ? tag(`依赖 ${member.requires.join(", ")}`, "warn")
                : null,
            ),
            h("div", { class: "library-checksum mono", text: member.checksum }),
            h(
              "div",
              { class: "library-pack-targets" },
              ...member.targets.map((target) =>
                h(
                  "div",
                  { class: "library-pack-target" },
                  h("span", { class: "dot dot-ok" }),
                  h("strong", { text: targetLabel(target.agent) }),
                  h("span", { class: "mono", text: target.destination }),
                ),
              ),
            ),
            member.warnings?.length
              ? h("div", { class: "library-warning" }, ...member.warnings.map((warning) => h("div", { text: warning })))
              : null,
          ),
        ),
      ),
      plan.warnings?.length
        ? h("div", { class: "library-warning" }, ...plan.warnings.map((warning) => h("div", { text: warning })))
        : null,
      h(
        "div",
        { class: "library-pack-actions" },
        h("div", { class: "note", text: `计划在 ${prettyTime(plan.created_at)} 生成，30 分钟内有效；安装前会重新核对 checksum。` }),
        h("div", { class: "spacer", style: "flex:1" }),
        button("确认安装整包", {
          variant: "primary",
          disabled: packPlanning,
          onClick: () => void doPackInstall(),
        }),
      ),
    );
    return card;
  }

  function publishView(): HTMLElement {
    const wrap = h("div", { class: "library-publish" });
    const pathInput = h("input", {
      class: "input mono",
      placeholder: "包含 homing.resource.yaml 的本机资源目录",
      value: publishPath,
      "aria-label": "待发布资源目录",
    });
    pathInput.addEventListener("input", () => {
      publishPath = (pathInput as HTMLInputElement).value;
      publishResult = null;
      publishError = "";
    });
    const kindSelect = h("select", { class: "input", "aria-label": "资源类型" }) as HTMLSelectElement;
    kindSelect.append(h("option", { value: "", text: "自动识别" }));
    for (const [value, label] of Object.entries(KIND_LABELS)) {
      kindSelect.append(h("option", { value, text: label }));
    }
    kindSelect.value = publishKind;
    kindSelect.addEventListener("change", () => {
      publishKind = kindSelect.value;
      publishResult = null;
      publishError = "";
    });

    const hero = h(
      "section",
      { class: "library-publish-hero" },
      h(
        "div",
        { class: "library-publish-copy" },
        h("div", { class: "eyebrow", text: "LOCAL PUBLISH PREFLIGHT" }),
        h("h2", { text: "发布预检" }),
        h("p", {
          text: "在把能力交给别人之前，先核对 manifest、内容 checksum、文件清单与安装边界。检查只在本地读取，不上传、不签名，也不执行资源声明的命令。",
        }),
      ),
      h(
        "div",
        { class: "library-publish-form" },
        h("label", null, h("span", { text: "资源目录" }), pathInput),
        h("label", null, h("span", { text: "类型" }), kindSelect),
        button(publishChecking ? "正在检查…" : "开始发布预检", {
          variant: "primary",
          iconSvg: icons.shield,
          disabled: publishChecking,
          onClick: () => void doPublishCheck(),
        }),
      ),
    );
    wrap.append(hero);
    if (publishError) wrap.append(h("div", { class: "library-warning", text: publishError }));
    if (publishResult) {
      wrap.append(publishResultView(publishResult));
    } else {
      wrap.append(
        h(
          "section",
          { class: "library-publish-guide" },
          h("div", { class: "section-title" }, h("span", { text: "预检会检查什么" })),
          h(
            "div",
            { class: "library-publish-grid" },
            publishGuideCard(
              icons.fileText,
              "统一 manifest",
              "读取 homing.resource.yaml，验证类型、来源、版本、targets 与受支持的安装策略。",
            ),
            publishGuideCard(
              icons.shield,
              "内容指纹",
              "按相对路径与文件字节计算 SHA-256；清单缺 checksum 时只允许作为本机草稿。",
            ),
            publishGuideCard(
              icons.info,
              "文件卫生",
              "拒绝符号链接与特殊文件，检查文件数、大小和不可读项目，避免把本机路径带进分享包。",
            ),
            publishGuideCard(
              icons.terminal,
              "权限边界",
              "列出网络、文件系统与命令声明供人工复核；Homing 不会执行这些命令。",
            ),
          ),
        ),
      );
    }
    return wrap;
  }

  function publishGuideCard(iconSvg: string, title: string, body: string): HTMLElement {
    return h(
      "article",
      { class: "library-publish-check" },
      h("span", { class: "library-publish-icon" }, icon(iconSvg, 17)),
      h("div", null, h("strong", { text: title }), h("p", { text: body })),
    );
  }

  function publishResultView(result: LibraryPublishCheck): HTMLElement {
    const problems = result.problems || [];
    const warnings = result.warnings || [];
    const state = problems.length ? "blocked" : result.ok ? "ready" : "review";
    const manifest = result.manifest;
    const checksum = result.inspection.checksum || "未生成";
    const targets = manifest?.spec?.targets || [];
    const permissions = manifest?.spec?.permissions;
    const commands = permissions?.commands || [];
    const readPaths = permissions?.filesystem?.read || [];
    const writePaths = permissions?.filesystem?.write || [];

    const status = h(
      "div",
      { class: `library-publish-status ${state}` },
      h(
        "span",
        { class: "library-publish-status-icon" },
        icon(problems.length ? icons.warn : result.ok ? icons.check : icons.info, 20),
      ),
      h(
        "div",
        null,
        h("strong", {
          text: problems.length
            ? "存在阻塞项，暂时不能作为可分享版本"
            : result.ok
              ? "本机预检通过"
              : "需要人工确认后再分享",
        }),
        h("p", { text: result.note }),
      ),
      h("div", { class: "spacer", style: "flex:1" }),
      tag(problems.length ? "已阻塞" : result.ok ? "可分享" : "待复核", problems.length ? "bad" : result.ok ? "ok" : "warn"),
    );

    const facts = h(
      "div",
      { class: "library-publish-facts" },
      publishFact("类型", KIND_LABELS[result.inspection.kind] || result.inspection.kind),
      publishFact("文件", `${result.inspection.file_count} 个 · ${formatBytes(result.inspection.size_bytes)}`),
      publishFact("建议 ID", result.inspection.suggested_id),
      publishFact("版本", result.inspection.version || manifest?.metadata.version || "未声明"),
    );

    const manifestBlock = manifest
      ? h(
          "section",
          { class: "library-publish-manifest" },
          h(
            "div",
            { class: "library-publish-manifest-head" },
            h("div", { class: "eyebrow", text: "MANIFEST" }),
            h("strong", { text: manifest.metadata.name }),
            h("div", { class: "spacer", style: "flex:1" }),
            tag(`v${manifest.metadata.version}`, "ok"),
          ),
          h("div", { class: "library-publish-manifest-id mono", text: manifest.metadata.id }),
          h("div", { class: "library-publish-manifest-source mono", text: manifest.metadata.source }),
          h(
            "div",
            { class: "library-publish-targets" },
            ...targets.map((target) =>
              h(
                "span",
                { class: "library-target" },
                h("span", { class: "dot dot-ok" }),
                h("span", { text: targetLabel(target.agent) }),
                target.mode ? h("span", { class: "mono", text: target.mode }) : null,
              ),
            ),
            targets.length ? null : h("span", { class: "muted", text: "manifest 没有声明投放目标" }),
          ),
          h(
            "div",
            { class: "library-publish-permissions" },
            publishPermission("网络", permissions?.network ? "需要" : "不需要", Boolean(permissions?.network)),
            publishPermission("命令", commands.length ? `${commands.length} 条，未执行` : "无", commands.length > 0),
            publishPermission(
              "文件系统",
              readPaths.length || writePaths.length
                ? `读取 ${readPaths.length} · 写入 ${writePaths.length}`
                : "未声明",
              readPaths.length + writePaths.length > 0,
            ),
            publishPermission("安装策略", manifest.spec?.install?.strategy || "copy", false),
          ),
          manifest.spec?.install?.backup
            ? h("div", { class: "note", text: "manifest 要求安装前备份；Homing 的本机导入与套件安装会保留回滚位置。" })
            : null,
        )
      : h(
          "div",
          { class: "library-publish-missing" },
          icon(icons.fileText, 18),
          h("div", null, h("strong", { text: "没有可用的 homing.resource.yaml" }), h("p", { text: "补上 manifest 与 metadata.checksum 后再运行预检。" })),
        );

    const issues = h("div", { class: "library-publish-issues" });
    if (problems.length) {
      issues.append(
        h(
          "section",
          { class: "library-publish-issue blocked" },
          h("div", { class: "library-publish-issue-title" }, icon(icons.warn, 15), h("strong", { text: "阻塞项" })),
          ...problems.map((problem) => h("div", { class: "library-publish-issue-row", text: problem })),
        ),
      );
    }
    if (warnings.length) {
      issues.append(
        h(
          "section",
          { class: "library-publish-issue review" },
          h("div", { class: "library-publish-issue-title" }, icon(icons.info, 15), h("strong", { text: "人工复核" })),
          ...warnings.map((warning) => h("div", { class: "library-publish-issue-row", text: warning })),
        ),
      );
    }

    return h(
      "section",
      { class: "library-publish-result" },
      status,
      facts,
      h(
        "div",
        { class: "library-publish-columns" },
        manifestBlock,
        h(
          "section",
          { class: "library-publish-checksum-card" },
          h("div", { class: "eyebrow", text: "CONTENT CHECKSUM" }),
          h("div", { class: "library-publish-checksum mono", text: checksum }),
          h(
            "div",
            { class: "library-publish-checksum-actions" },
            button("复制 checksum", {
              small: true,
              variant: "quiet",
              iconSvg: icons.copy,
              onClick: () => {
                void navigator.clipboard
                  .writeText(checksum)
                  .then(() => toast(loc("checksum 已复制", "Checksum copied"), "ok"))
                  .catch(() => promptDialog({ title: loc("复制 checksum", "Copy checksum"), defaultValue: checksum, confirmLabel: loc("确定", "OK") }));
              },
            }),
          ),
          h("p", {
            text: "这是本机内容指纹，不是签名。分享后接收方仍需重新校验，L4 才会加入签名与来源撤销。",
          }),
          h("div", { class: "library-publish-path mono", text: result.inspection.path }),
          manifest?.metadata.license
            ? h("div", { class: "library-publish-license", text: `License · ${manifest.metadata.license}` })
            : null,
        ),
      ),
      issues,
    );
  }

  function publishFact(label: string, value: string): HTMLElement {
    return h("div", { class: "library-publish-fact" }, h("span", { text: label }), h("strong", { text: value }));
  }

  function publishPermission(label: string, value: string, review: boolean): HTMLElement {
    return h(
      "div",
      { class: review ? "library-publish-permission review" : "library-publish-permission" },
      h("span", { text: label }),
      h("strong", { text: value }),
    );
  }

  async function doPublishCheck() {
    if (!publishPath.trim()) {
      publishError = "先填写包含 homing.resource.yaml 的本机资源目录";
      paint();
      return;
    }
    publishChecking = true;
    publishError = "";
    publishResult = null;
    paint();
    try {
      publishResult = await endpoints.checkLibraryPublish(publishPath.trim(), publishKind || undefined);
    } catch (err) {
      publishError = (err as Error).message;
    } finally {
      publishChecking = false;
      paint();
    }
  }

  function openPublishCheck(resource: LibraryResource) {
    const managed = resource.targets.find((target) => target.managed);
    const source = managed || resource.targets[0];
    publishPath = source?.path || "";
    publishKind = resource.kind;
    publishResult = null;
    publishError = "";
    tab = "publish";
    closeDrawer();
    paintToolbar();
    paint();
  }

  function auditView(): HTMLElement {
    if (!auditLoaded) {
      return h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: "正在读取审计记录…" }));
    }
    if (!auditEntries.length) {
      return h(
        "div",
        { class: "list-card" },
        emptyState("还没有资源库操作", "导入或移除资源后会在这里留下设备、动作与资源 id，不记录文件内容。"),
      );
    }
    return listCard(
      ...auditEntries.map((entry) =>
        h(
          "div",
          { class: "library-audit-row" },
          h("span", { class: "library-kind", text: entry.action.replace("library_", "") }),
          h("div", { class: "lrow-main" },
            h("div", { class: "lrow-title", text: entry.resource_id || "—" }),
            h("div", { class: "lrow-sub mono", text: `${entry.device || "local"} · ${entry.kind || "resource"}` }),
          ),
          h("span", { class: "lrow-time", text: prettyTime(entry.at) }),
        ),
      ),
    );
  }

  function installationsView(): HTMLElement {
    if (!installationsLoaded) {
      return h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: "正在读取安装记录…" }));
    }
    if (!installations.length) {
      return h(
        "div",
        { class: "list-card" },
        emptyState("还没有远端安装记录", "从 URL 导入资源后会记录来源、SHA-256、目标目录和回滚状态。"),
      );
    }
    return listCard(
      ...installations.map((installation) => {
        const restoredPrevious =
          installation.state === "installed" && Boolean(installation.rolled_back_at);
        return h(
          "div",
          { class: "library-audit-row" },
          h("span", {
            class: `library-kind ${installation.state === "installed" ? "kind-skill" : ""}`,
            text:
              installation.state === "installed"
                ? restoredPrevious
                  ? "已恢复旧版"
                  : "已安装"
                : "已回滚",
          }),
          h(
            "div",
            { class: "lrow-main" },
            h("div", { class: "lrow-title", text: installation.name }),
            h(
              "div",
              { class: "lrow-sub mono" },
              h("span", {
                text: `${KIND_LABELS[installation.kind] || installation.kind}${
                  installation.version ? ` · v${installation.version}` : ""
                } · ${prettyTime(installation.installed_at)}${
                  restoredPrevious ? " · 已恢复上一版本" : ""
                }`,
              }),
            ),
            h("div", { class: "lrow-sub mono", text: installation.destination }),
            installation.download_sha256
              ? h("div", { class: "library-checksum mono", text: installation.download_sha256 })
              : null,
          ),
          installation.state === "installed"
            ? h(
                "div",
                { style: "display:flex;gap:6px;align-items:center;flex-wrap:wrap;justify-content:flex-end" },
                installation.source_url
                  ? button("检查更新", {
                      small: true,
                      variant: "quiet",
                      onClick: () => void checkInstallationUpdate(installation),
                    })
                  : null,
                button("回滚", {
                  small: true,
                  variant: "danger",
                  onClick: () => void rollbackInstallation(installation),
                }),
              )
            : h("span", { class: "muted", text: installation.trash_path || "已移入回收区" }),
        );
      }),
    );
  }

  async function load() {
    try {
      const [next, sourcePayload] = await Promise.all([
        endpoints.library(),
        endpoints.libraryProviders().catch(() => null),
      ]);
      if (disposed) return;
      snapshot = next;
      if (sourcePayload) providers = sourcePayload.providers || [];
      paintToolbar();
      paint();
    } catch (err) {
      if (disposed) return;
      clear(content);
      content.append(h("div", { class: "list-card" }, emptyState("读取资源库失败", (err as Error).message)));
    }
  }

  async function loadInstallations() {
    try {
      const payload = await endpoints.libraryInstallations();
      if (disposed) return;
      installations = payload.installations;
      installationsLoaded = true;
      if (tab === "rollback") paint();
    } catch (err) {
      if (disposed) return;
      installationsLoaded = true;
      installations = [];
      if (tab === "rollback") paint();
      reportError(err, "读取安装记录失败");
    }
  }

  async function loadPacks() {
    try {
      const payload = await endpoints.libraryPacks();
      if (disposed) return;
      packs = payload.packs;
      packsLoaded = true;
      if (tab === "packs") paint();
    } catch (err) {
      if (disposed) return;
      packsLoaded = true;
      packs = [];
      if (tab === "packs") paint();
      reportError(err, "读取套件安装记录失败");
    }
  }

  async function doPackPlan() {
    if (!packPath.trim()) {
      packError = "先填写包含 homing.pack.yaml 的本机套件目录";
      paint();
      return;
    }
    packPlanning = true;
    packError = "";
    packPlan = null;
    paint();
    try {
      packPlan = await endpoints.planLibraryPack(packPath.trim());
    } catch (err) {
      packError = (err as Error).message;
    } finally {
      packPlanning = false;
      paint();
    }
  }

  async function doPackInstall() {
    if (!packPlan) return;
    const destinations = packPlan.members.flatMap((member) =>
      member.targets.map((target) => `${targetLabel(target.agent)} → ${target.destination}`),
    );
    const ok = await confirmDialog({
      title: loc(`安装套件「${packPlan.pack.metadata.name}」`, `Install pack "${packPlan.pack.metadata.name}"`),
      message:
        `安装「${packPlan.pack.metadata.name}」的 ${packPlan.members.length} 个资源？\n\n` +
        `${destinations.join("\n")}\n\n` +
        "先全部 staging，再逐项目标换入；失败会移除本次已写入的目录。",
      confirmLabel: loc("确认安装", "Install"),
    });
    if (!ok) {
      return;
    }
    packPlanning = true;
    packError = "";
    paint();
    try {
      const result = await endpoints.installLibraryPack(packPlan.plan_id);
      toast(`已安装套件「${result.installation.name}」`, "ok", 6000);
      packPlan = null;
      packPath = "";
      packsLoaded = false;
      auditLoaded = false;
      await Promise.all([loadPacks(), load()]);
    } catch (err) {
      packError = (err as Error).message;
    } finally {
      packPlanning = false;
      paint();
    }
  }

  async function rollbackPack(pack: LibraryPackInstallation) {
    const count = pack.members.reduce((sum, member) => sum + member.targets.length, 0);
    const ok = await confirmDialog({
      title: loc(`回滚套件「${pack.name}」`, `Rollback pack "${pack.name}"`),
      message:
        `把「${pack.name}」的 ${count} 个投放目录移入 ~/.homing/trash？\n\n` +
        "只会移动这个套件创建的目录，不会清理 Agent 根目录或其他资源。",
      confirmLabel: loc("确认回滚", "Rollback"),
      danger: true,
    });
    if (!ok) {
      return;
    }
    try {
      const result = await endpoints.rollbackLibraryPack(pack.id);
      toast(
        result.missing.length
          ? `套件已回滚；${result.missing.length} 个目录此前已不存在`
          : "套件已整体回滚",
        "ok",
      );
      packsLoaded = false;
      auditLoaded = false;
      await Promise.all([loadPacks(), load()]);
    } catch (err) {
      reportError(err, "套件回滚失败");
    }
  }

  async function rollbackInstallation(installation: LibraryInstallation) {
    const restoringPrevious = Boolean(installation.backup_path);
    const ok = await confirmDialog({
      title: loc(`回滚「${installation.name}」`, `Rollback "${installation.name}"`),
      message: restoringPrevious
        ? `把当前版本移入 ~/.homing/trash，并恢复「${installation.name}」的上一版本？`
        : `把「${installation.name}」移入 ~/.homing/trash，并标记为已回滚？`,
      confirmLabel: loc("确认回滚", "Rollback"),
      danger: true,
    });
    if (!ok) {
      return;
    }
    try {
      await endpoints.rollbackLibraryInstallation(installation.id);
      toast(restoringPrevious ? "已恢复上一版本" : "已回滚到安装前状态", "ok");
      installationsLoaded = false;
      auditLoaded = false;
      await Promise.all([loadInstallations(), load()]);
    } catch (err) {
      reportError(err, "回滚失败");
    }
  }

  async function checkInstallationUpdate(installation: LibraryInstallation) {
    try {
      const plan = await endpoints.planLibraryInstallationUpdate(installation.id);
      if (!plan.update_available) {
        toast(
          `${installation.name} 已是最新${plan.available_version ? `版本 v${plan.available_version}` : ""}`,
          "ok",
        );
        return;
      }
      const from = plan.current_version ? `v${plan.current_version}` : "当前版本";
      const to = plan.available_version ? `v${plan.available_version}` : "远端新版本";
      const before = plan.current_checksum?.slice(0, 19) || "未记录";
      const after = plan.available_checksum?.slice(0, 19) || "未记录";
      const ok = await confirmDialog({
        title: loc(`更新「${installation.name}」`, `Update "${installation.name}"`),
        message:
          `把「${installation.name}」从 ${from} 更新到 ${to}？\n\n` +
          `内容校验：${before} → ${after}\n\n` +
          "旧版会保留在 ~/.homing/library/backups；更新后可回滚恢复上一版。",
        confirmLabel: loc("确认更新", "Update"),
      });
      if (!ok) {
        return;
      }
      await endpoints.applyLibraryInstallationUpdate(installation.id, plan.plan.fetch_id);
      toast(`「${installation.name}」已更新到 ${to}`, "ok", 6000);
      installationsLoaded = false;
      auditLoaded = false;
      await Promise.all([loadInstallations(), load()]);
    } catch (err) {
      reportError(err, "检查或安装更新失败");
    }
  }

  async function loadAudit() {
    try {
      const payload = await endpoints.libraryAudit();
      if (disposed) return;
      auditEntries = payload.entries;
      auditLoaded = true;
      if (tab === "audit") paint();
    } catch (err) {
      if (disposed) return;
      auditLoaded = true;
      auditEntries = [];
      if (tab === "audit") paint();
      reportError(err, "读取资源库审计失败");
    }
  }

  async function openDetail(resource: LibraryResource) {
    drawerOpen = true;
    selected = null;
    selectedError = "";
    paintDrawer();
    try {
      const detail = await endpoints.libraryDetail(resource.id);
      if (disposed || !drawerOpen) return;
      selected = detail;
      paintDrawer();
    } catch (err) {
      if (disposed) return;
      selectedError = (err as Error).message;
      paintDrawer();
    }
  }

  function paintDrawer() {
    drawer.hidden = !drawerOpen;
    overlay.hidden = !drawerOpen;
    clear(drawer);
    if (!drawerOpen) return;
    drawer.append(
      h(
        "div",
        { class: "library-drawer-head" },
        h("div", null, h("div", { class: "eyebrow", text: "RESOURCE" }), h("h2", { text: selected?.name || "正在读取…" })),
        h("div", { class: "spacer", style: "flex:1" }),
        closeButton(closeDrawer),
      ),
    );
    if (selectedError) {
      drawer.append(h("div", { class: "library-drawer-body" }, h("div", { class: "empty", text: selectedError })));
      return;
    }
    if (!selected) {
      drawer.append(h("div", { class: "library-drawer-body" }, h("div", { class: "note", text: "正在读取文件…" })));
      return;
    }
    const body = h("div", { class: "library-drawer-body" });
    body.append(
      h("p", { class: "body", text: selected.description || "没有说明文件。" }),
      h(
        "div",
        { class: "library-detail-facts" },
        h("span", { text: KIND_LABELS[selected.kind] || selected.kind }),
        h("span", { text: `${selected.file_count} 文件` }),
        h("span", { text: formatBytes(selected.size_bytes) }),
        h("span", { text: prettyTime(selected.modified_at) }),
        selected.version ? h("span", { text: `v${selected.version}` }) : null,
      ),
    );
    if (selected.warnings?.length) {
      body.append(h("div", { class: "library-warning" }, ...selected.warnings.map((warning) => h("div", { text: warning }))));
    }
    if (selected.targets.length) {
      body.append(
        h(
          "div",
          { class: "library-detail-actions" },
          button("检查可发布性", {
            small: true,
            variant: "quiet",
            iconSvg: icons.shield,
            onClick: () => openPublishCheck(selected!),
          }),
          h("span", { class: "note", text: "进入发布页后仍可修改路径与类型，不会上传或签名。" }),
        ),
      );
    }
    for (const target of selected.targets) {
      body.append(
        h(
          "section",
          { class: "library-target-block" },
          h("div", { class: "library-target-title" },
            h("strong", { text: targetLabel(target.agent) }),
            h("span", { class: "mono", text: target.path }),
          ),
          target.entry ? h("div", { class: "note", text: `入口：${target.entry}` }) : null,
          target.managed
            ? button("从此目录移除", {
                variant: "danger",
                small: true,
                onClick: () => void removeResource(selected!, target.path),
              })
            : h("div", { class: "note", text: "Agent 原生目录，只读；Homing 不会删除。" }),
        ),
      );
    }
    body.append(h("div", { class: "section-title" }, h("span", { text: "文件" })));
    body.append(
      listCard(
        ...selected.files.map((file) =>
          h(
            "div",
            { class: "library-file-row" },
            h("span", { class: "mono", text: file.path }),
            h("span", { class: "muted", text: formatBytes(file.size) }),
          ),
        ),
      ),
    );
    drawer.append(body);
  }

  function closeDrawer() {
    drawerOpen = false;
    drawer.hidden = true;
    overlay.hidden = true;
  }

  function paintImport() {
    importPanel.hidden = !importOpen;
    if (!importOpen) return;
    if (importMode === "remote") {
      paintRemoteImport();
      return;
    }
    clear(importPanel);
    const pathInput = h("input", {
      class: "input mono",
      placeholder: "~/Projects/my-skill 或 /Users/you/skill.zip 的解压目录",
      value: importPath,
    });
    pathInput.addEventListener("input", () => {
      importPath = (pathInput as HTMLInputElement).value;
      importPlan = null;
      importError = "";
    });
    const nameInput = h("input", {
      class: "input",
      placeholder: "显示名称（留空读取 SKILL.md 或目录名）",
      value: importName,
    });
    nameInput.addEventListener("input", () => {
      importName = (nameInput as HTMLInputElement).value;
    });
    const kindSelect = h("select", { class: "input" }) as HTMLSelectElement;
    kindSelect.append(h("option", { value: "", text: "自动识别" }));
    for (const [value, label] of Object.entries(KIND_LABELS)) {
      kindSelect.append(h("option", { value, text: label }));
    }
    kindSelect.value = importKind;
    kindSelect.addEventListener("change", () => {
      importKind = kindSelect.value;
      importPlan = null;
      importError = "";
    });
    const form = h(
      "div",
      { class: "library-import-form" },
      h("label", null, h("span", { text: "本机路径" }), pathInput),
      h("label", null, h("span", { text: "类型" }), kindSelect),
      h("label", null, h("span", { text: "名称" }), nameInput),
    );
    const actions = h("div", { class: "library-import-actions" });
    const preview = button("预览变更", {
      variant: "ghost",
      disabled: importing,
      onClick: () => void doPlan(),
    });
    const install = button(importPlan ? "确认导入" : "预览后导入", {
      variant: "primary",
      disabled: !importPlan || importing,
      onClick: () => void doInstall(),
    });
    actions.append(preview, install);
    append(importPanel, [
      h(
        "div",
        { class: "library-import-head" },
        h("div", null, h("h2", { text: "导入本机资源" })),
        importModeSwitch(),
        h("div", { class: "spacer", style: "flex:1" }),
        closeButton(() => {
          importOpen = false;
          importPanel.hidden = true;
        }),
      ),
      h("p", {
        class: "body",
        text: "只复制到 ~/.homing，不执行第三方安装命令，不改任何 Agent 的原生配置。符号链接与特殊文件会被拒绝。",
      }),
      form,
      importError ? h("div", { class: "library-warning", text: importError }) : null,
      importPlan ? importPlanView(importPlan) : null,
      actions,
    ]);
  }

  function importModeSwitch(): HTMLElement {
    return segmented(
      [
        { value: "local", label: "本机目录" },
        { value: "remote", label: "URL／Registry" },
      ],
      importMode,
      (next) => {
        importMode = next;
        importError = "";
        paintImport();
      },
    );
  }

  function paintRemoteImport() {
    clear(importPanel);
    const urlInput = h("input", {
      class: "input mono",
      placeholder: "https://github.com/owner/repo#v1.2.0 或 mcp://server-name",
      value: importURL,
    });
    urlInput.addEventListener("input", () => {
      importURL = (urlInput as HTMLInputElement).value;
      remotePlan = null;
      importError = "";
    });
    const refInput = h("input", {
      class: "input mono",
      placeholder: "可选：分支、tag 或 commit",
      value: importRef,
    });
    refInput.addEventListener("input", () => {
      importRef = (refInput as HTMLInputElement).value;
      remotePlan = null;
    });
    const nameInput = h("input", {
      class: "input",
      placeholder: "可选：覆盖 manifest 中的名称",
      value: importName,
    });
    nameInput.addEventListener("input", () => {
      importName = (nameInput as HTMLInputElement).value;
    });
    const kindSelect = h("select", { class: "input" }) as HTMLSelectElement;
    kindSelect.append(h("option", { value: "", text: "自动识别" }));
    for (const [value, label] of Object.entries(KIND_LABELS)) {
      kindSelect.append(h("option", { value, text: label }));
    }
    kindSelect.value = importKind;
    kindSelect.addEventListener("change", () => {
      importKind = kindSelect.value;
      remotePlan = null;
    });

    const close = () => {
      importOpen = false;
      importPanel.hidden = true;
    };
    const actions = h("div", { class: "library-import-actions" });
    actions.append(
      button("预览下载与变更", {
        variant: "ghost",
        disabled: importing,
        onClick: () => void doRemotePlan(),
      }),
      button(remotePlan ? "确认安装" : "预览后安装", {
        variant: "primary",
        disabled: !remotePlan || importing,
        onClick: () => void doRemoteInstall(),
      }),
    );
    append(importPanel, [
      h(
        "div",
        { class: "library-import-head" },
        h("div", null, h("h2", { text: "从远端来源安装" })),
        importModeSwitch(),
        h("div", { class: "spacer", style: "flex:1" }),
        closeButton(close),
      ),
      h("p", {
        class: "body",
        text: "只下载、校验、解包、预览与复制；不执行第三方安装脚本，不修改 Agent 原生配置。GitHub、skills.sh、直接 URL 与 MCP Registry 都会先 pin 下载内容的 SHA-256。",
      }),
      h(
        "div",
        { class: "library-import-form" },
        h("label", null, h("span", { text: "来源 URL" }), urlInput),
        h("label", null, h("span", { text: "Ref" }), refInput),
        h("label", null, h("span", { text: "类型" }), kindSelect),
        h("label", null, h("span", { text: "名称" }), nameInput),
      ),
      providers.length
        ? h(
            "div",
            { class: "library-provider-hints" },
            ...providers.map((provider) =>
              h("button", {
                class: "library-provider-hint",
                type: "button",
                title: provider.note,
                text: provider.label,
                onClick: () => {
                  importURL = provider.example;
                  remotePlan = null;
                  paintRemoteImport();
                },
              }),
            ),
          )
        : null,
      importError ? h("div", { class: "library-warning", text: importError }) : null,
      remotePlan ? remotePlanView(remotePlan) : null,
      actions,
    ]);
  }

  function remotePlanView(plan: LibraryRemotePlan): HTMLElement {
    return h(
      "div",
      { class: "library-plan" },
      h(
        "div",
        { class: "library-plan-head" },
        tag(KIND_LABELS[plan.inspection.kind] || plan.inspection.kind, "ok"),
        tag(plan.trust, "warn"),
        h("strong", { text: plan.inspection.name }),
        h("span", { class: "muted", text: `${plan.inspection.file_count} 文件 · ${formatBytes(plan.inspection.size_bytes)}` }),
      ),
      h("div", { class: "library-path-arrow mono" },
        h("span", { text: plan.resolved_url }),
        h("span", { text: "→" }),
        h("span", { text: plan.plan.destination }),
      ),
      h("div", { class: "library-checksum mono", text: plan.download_sha256 }),
      plan.manifest ? h("div", { class: "note", text: `已验 manifest · ${plan.manifest.metadata.id} v${plan.manifest.metadata.version}` }) : null,
      ...(plan.warnings || plan.inspection.warnings || []).map((warning) =>
        h("div", { class: "library-warning", text: warning }),
      ),
    );
  }

  function importPlanView(plan: LibraryImportPlan): HTMLElement {
    return h(
      "div",
      { class: "library-plan" },
      h(
        "div",
        { class: "library-plan-head" },
        tag(KIND_LABELS[plan.kind] || plan.kind, "ok"),
        h("strong", { text: plan.name }),
        h("span", { class: "muted", text: `${plan.inspection.file_count} 文件 · ${formatBytes(plan.inspection.size_bytes)}` }),
      ),
      h("div", { class: "library-path-arrow mono" },
        h("span", { text: plan.source }),
        h("span", { text: "→" }),
        h("span", { text: plan.destination }),
      ),
      plan.inspection.checksum
        ? h("div", { class: "library-checksum mono", text: plan.inspection.checksum })
        : null,
      ...(plan.warnings || plan.inspection.warnings || []).map((warning) =>
        h("div", { class: "library-warning", text: warning }),
      ),
    );
  }

  async function doPlan() {
    if (!importPath.trim()) {
      importError = "先填写本机路径";
      paintImport();
      return;
    }
    importing = true;
    importError = "";
    paintImport();
    try {
      importPlan = await endpoints.planLibrary({
        path: importPath.trim(),
        kind: importKind || undefined,
        name: importName.trim() || undefined,
      });
    } catch (err) {
      importPlan = null;
      importError = (err as Error).message;
    } finally {
      importing = false;
      paintImport();
    }
  }

  async function doRemotePlan() {
    if (!importURL.trim()) {
      importError = "先填写远端 URL";
      paintRemoteImport();
      return;
    }
    importing = true;
    importError = "";
    paintRemoteImport();
    try {
      remotePlan = await endpoints.planRemoteLibrary({
        url: importURL.trim(),
        ref: importRef.trim() || undefined,
        kind: importKind || undefined,
        name: importName.trim() || undefined,
      });
    } catch (err) {
      remotePlan = null;
      importError = (err as Error).message;
    } finally {
      importing = false;
      paintRemoteImport();
    }
  }

  async function doRemoteInstall() {
    if (!remotePlan) return;
    importing = true;
    importError = "";
    paintRemoteImport();
    try {
      await endpoints.installRemoteLibrary(remotePlan.fetch_id);
      toast(`已安装「${remotePlan.inspection.name}」`, "ok");
      importOpen = false;
      remotePlan = null;
      importURL = "";
      importRef = "";
      importName = "";
      importKind = "";
      installationsLoaded = false;
      auditLoaded = false;
      await load();
    } catch (err) {
      importError = (err as Error).message;
    } finally {
      importing = false;
      paintRemoteImport();
      paint();
    }
  }

  async function doInstall() {
    if (!importPlan) return;
    importing = true;
    importError = "";
    paintImport();
    try {
      await endpoints.installLibrary({
        path: importPlan.source,
        kind: importPlan.kind,
        name: importPlan.name,
      });
      toast(`已导入「${importPlan.name}」`, "ok");
      importOpen = false;
      importPlan = null;
      importPath = "";
      importName = "";
      importKind = "";
      auditLoaded = false;
      await load();
    } catch (err) {
      importError = (err as Error).message;
    } finally {
      importing = false;
      paintImport();
      paint();
    }
  }

  async function removeResource(resource: LibraryResource, target?: string) {
    const label = target ? loc("这个管理目录", "this managed folder") : loc("该资源", "this resource");
    const ok = await confirmDialog({
      title: loc("移除资源", "Remove resource"),
      message: loc(
        `把${label}移入 ~/.homing/trash？不会删除 Agent 原生目录，也不会执行任何脚本。`,
        `Move ${label} to ~/.homing/trash? Native agent directories will not be deleted.`,
      ),
      confirmLabel: loc("移入回收区", "Move to trash"),
      danger: true,
    });
    if (!ok) return;
    try {
      await endpoints.removeLibrary(resource.id, target);
      toast(loc("已移入回收区，可手动恢复", "Moved to trash"), "ok");
      closeDrawer();
      auditLoaded = false;
      await load();
    } catch (err) {
      reportError(err, loc("移除资源失败", "Failed to remove resource"));
    }
  }

  root.append(head, toolbar, importPanel, content);
  overlay.addEventListener("click", closeDrawer);
  const handleEsc = (event: KeyboardEvent) => {
    if (event.key === "Escape" && drawer.classList.contains("open")) {
      event.preventDefault();
      closeDrawer();
    }
  };
  document.addEventListener("keydown", handleEsc);
  root.append(drawer, overlay);

  paintToolbar();
  clear(content);
  content.append(h("div", { class: "list-card", style: "padding:18px" }, h("div", { class: "note", text: loc("正在扫描本机资源…", "Scanning local resources…") })));
  void load();

  return {
    el: root,
    dispose: () => {
      disposed = true;
      document.removeEventListener("keydown", handleEsc);
    },
  };
}
