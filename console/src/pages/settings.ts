// 设置：侧栏底部那个入口。放常规开关、维护入口和版本信息。
// 这一页不做表单大杂烩——每个 agent 的细节在自己的卡片里，这里只管全局的几件事。

import { h, clear, icon, type Child } from "../lib/dom";
import { icons } from "../lib/icons";
import { dateTimeShort, uptime } from "../lib/format";
import {
  api,
  endpoints,
  type AutoAwaySettings,
  type ConnectionInfo,
  type HomeSettings,
  type SearchIndexStatus,
} from "../lib/api";
import { button, groupHead, listCard, reportError, segmented, tag, toast, toggle } from "../lib/ui";
import { confirmDialog } from "../lib/dialog";
import { getLocale, loc, setLocale, t, type Locale } from "../i18n";
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

function formatIndexBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  if (value < 1024) return `${Math.round(value)} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(value < 10 * 1024 ? 1 : 0)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

export function renderSettings(ctx: PageContext): PageResult {
  const root = h("div");
  const host = h("div");
  root.append(host);

  let disposed = false;
  let autoAway: AutoAwaySettings = { enabled: false, idle_minutes: 5, available: false };
  let homeSettings: HomeSettings = { new_activity: "away", collab_return: "card" };
  let searchIndex: SearchIndexStatus | null = null;
  let searchBusy = false;

  async function copy(text: string, what: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast(`已复制${what}`, "ok");
    } catch {
      toast("复制失败，请手动选中", "bad");
    }
  }

  async function saveAutoAway(enabled: boolean, idleMinutes: number) {
    const minutes = Math.max(1, Math.min(240, Math.round(idleMinutes || 5)));
    try {
      await endpoints.setAutoAwaySettings({ enabled, idle_minutes: minutes });
      autoAway = { ...autoAway, enabled, idle_minutes: minutes };
      toast(
        enabled
          ? loc(`已开启自动离开（闲置 ${minutes} 分钟）`, `Auto-away enabled (${minutes}m idle)`)
          : loc("已关闭自动离开", "Auto-away disabled"),
        "ok",
      );
      await load();
    } catch (err) {
      reportError(err, loc("设置自动离开失败", "Failed to update auto-away"));
      await load();
    }
  }

  async function saveCollabReturn(enabled: boolean) {
    try {
      const result = await endpoints.updateHomeSettings({
        collab_return: enabled ? "inject" : "card",
      });
      homeSettings = result.settings;
      toast(
        enabled
          ? loc("已开启：子 Agent 完成后会自动把结论送回父会话", "Enabled: child results auto-forward to parent session")
          : loc("已关闭自动转交，子任务结果仅以卡片显示", "Disabled auto-forward; child results stay as cards"),
        "ok",
      );
      await load();
    } catch (err) {
      reportError(err, loc("设置跨 Agent 结果转交失败", "Failed to update dispatch return setting"));
      await load();
    }
  }

  async function changeSearchIndex(action: "enable" | "disable" | "rebuild" | "delete") {
    if (searchBusy) return;
    searchBusy = true;
    if (action === "delete") {
      const ok = await confirmDialog({
        title: loc("删除并关闭正文索引？", "Delete & Disable Full-Text Index?"),
        message: loc(
          "只会移除可重建的 index.db，不会删除任何会话原文。正文搜索会同时关闭；以后可在同一张卡片重新开启并建立索引。",
          "Only removes the rebuildable index.db without touching any session transcripts. Full-text search will be disabled until re-enabled.",
        ),
        confirmLabel: t("common.delete"),
        danger: true,
      });
      if (!ok) {
        searchBusy = false;
        return;
      }
    }
    try {
      const result = await endpoints.searchIndexAction(action);
      if (action === "enable") {
        toast("已开启正文索引；首次正文搜索会按需建立索引", "ok");
      } else if (action === "disable") {
        toast("已关闭正文索引；现有索引仍在，可继续删除", "ok");
      } else if (action === "rebuild") {
        toast(`索引已重建，共处理 ${result.indexed ?? 0} 个会话`, "ok");
      } else {
        toast("正文索引已删除并关闭，会话原文未受影响", "ok");
      }
    } catch (err) {
      reportError(err, "更新正文索引失败");
    } finally {
      searchBusy = false;
    }
    await load();
  }

  function paint(payload: ConnectionInfo) {
    clear(host);
    const runtime = payload.runtime === "go" ? `Go ${payload.runtime_version || ""}`.trim() : `Node ${payload.node}`;
    const minutesInput = h("input", {
      class: "input",
      type: "number",
      min: 1,
      max: 240,
      step: 1,
      value: String(autoAway.idle_minutes),
      disabled: !autoAway.available,
      title: "閒置分鐘數",
      style: "width:76px",
    });
    minutesInput.addEventListener("change", () => {
      void saveAutoAway(autoAway.enabled, Number(minutesInput.value || 5));
    });

    // 常规
    const autostartOn = payload.autostart.enabled;
    const autostartCard = listCard(
      row({
        title: t("settings.language"),
        sub: t("settings.languageSub"),
        right: [
          segmented<Locale>(
            [
              { value: "zh-CN", label: "简体中文" },
              { value: "en", label: "English" },
            ],
            getLocale(),
            (next) => setLocale(next),
          ),
        ],
      }),
      row({
        title: loc("开机自启", "Launch at Login"),
        sub: h(
          "span",
          null,
          loc("登录后自动把本机服务拉起来，不用每次手动开。", "Start the local Homing service automatically when you log in."),
          payload.autostart.legacy
            ? h("span", { style: "color:var(--amber)", text: loc(" 检测到已有 launchd 服务，两者不会重复启动。", " Existing launchd service detected; duplicates are prevented.") })
            : null,
        ),
        right: [
          tag(autostartOn ? loc("已开启", "Enabled") : loc("未开启", "Disabled"), autostartOn ? "ok" : "mute"),
          toggle({
            on: autostartOn,
            title: loc("开机自启", "Launch at Login"),
            onChange: async (next) => {
              try {
                const result = await api.post<{ ok: boolean; note?: string; error?: string }>("/api/autostart", {
                  enabled: next,
                });
                if (!result.ok) {
                  toast(result.error || loc("设置失败", "Failed to save"), "bad");
                  await load();
                  return;
                }
                toast(result.note || (next ? loc("已开启开机自启", "Autostart enabled") : loc("已关闭开机自启", "Autostart disabled")), "ok");
                await load();
              } catch (err) {
                reportError(err, loc("设置开机自启失败", "Failed to update autostart"));
                await load();
              }
            },
          }),
        ],
      }),
      row({
        title: loc("接入与手机配对", "Agent Setup & Phone Pairing"),
        sub: loc("接入新 Agent 或重新扫描端到端加密二维码配对手机。", "Connect new agents or pair a phone via encrypted QR code."),
        right: [linkButton(loc("打开向导", "Open Setup"), icons.key, () => ctx.navigate("/console/hub"))],
      }),
      row({
        title: loc("自动离开", "Auto-Away"),
        sub: autoAway.available
          ? loc("锁屏或键盘鼠标闲置超过指定分钟后，自动开启离开时段；解锁后自动结束。", "Automatically enable Away Mode after screen lock or idle timeout.")
          : loc("当前平台尚未提供锁屏与闲置探测。", "Idle / lock-screen detection is not available on this platform."),
        right: [
          minutesInput,
          h("span", { class: "note", text: loc("分钟", "min") }),
          toggle({
            on: autoAway.enabled,
            title: loc("自动离开", "Auto-Away"),
            disabled: !autoAway.available,
            onChange: (next) => void saveAutoAway(next, autoAway.idle_minutes),
          }),
        ],
      }),
      row({
        title: loc("自动转交子 Agent 结论", "Auto-Forward Child Results"),
        sub: h(
          "span",
          null,
          loc("开启后，跨 Agent 子任务完成时会把结论直接送入父会话；默认关闭，避免未经确认的提示词注入。", "Automatically inject completed @agent child results into the parent session. Off by default for safety."),
          homeSettings.collab_return === "inject"
            ? h("span", { style: "color:var(--amber)", text: loc(" 当前已开启，请仅对可信的 Agent 使用。", " Currently enabled; use with trusted agents only.") })
            : null,
        ),
        right: [
          tag(
            homeSettings.collab_return === "inject" ? loc("自动转交", "Auto-Forward") : loc("仅显示卡片", "Card Only"),
            homeSettings.collab_return === "inject" ? "warn" : "mute",
          ),
          toggle({
            on: homeSettings.collab_return === "inject",
            title: loc("自动转交子 Agent 结论", "Auto-Forward Child Results"),
            onChange: (next) => void saveCollabReturn(next),
          }),
        ],
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

    const stats = searchIndex?.stats;
    const indexUnavailable = Boolean(searchIndex?.error || stats?.open_error);
    const indexEnabled = searchIndex?.enabled === true;
    const indexExists = stats?.exists === true;
    const indexSummary = stats
      ? indexExists
        ? `${stats.sessions} 个会话 · ${stats.items} 条正文 · ${formatIndexBytes(stats.size_bytes)}`
        : "索引文件尚未建立"
      : searchIndex
        ? "索引状态不可用"
        : "正在读取索引状态…";
    const indexDetail = stats?.updated_at
      ? `最后更新 ${dateTimeShort(stats.updated_at)}`
      : stats?.trigram
        ? "使用 SQLite FTS5 trigram 本机存储"
        : "尚未取得索引统计";

    const searchCard = listCard(
      h(
        "div",
        { class: "lrow settings-index-row" },
        h(
          "div",
          { class: "lrow-main" },
          h(
            "div",
            { class: "lrow-title" },
            h("span", { class: "txt", text: "正文搜索索引" }),
            tag(indexEnabled ? "已开启" : "已关闭", indexEnabled ? "ok" : "mute"),
            indexUnavailable ? tag("不可用", "bad") : null,
          ),
          h(
            "div",
            { class: "lrow-sub", style: "white-space:normal" },
            "只在本机索引用户与助手的文字，每个会话最多 200 KB；工具输出不会进入索引。关闭不会自动删除已有数据，删除则会一并关闭。",
          ),
        ),
        h(
          "div",
          { class: "lrow-actions" },
          toggle({
            on: indexEnabled,
            title: "正文搜索索引",
            disabled: searchBusy || !searchIndex,
            onChange: (next) => void changeSearchIndex(next ? "enable" : "disable"),
          }),
        ),
      ),
      h(
        "div",
        { class: "lrow settings-index-row" },
        h(
          "div",
          { class: "lrow-main" },
          h("div", { class: "lrow-title" }, h("span", { class: "txt", text: indexSummary })),
          h(
            "div",
            { class: "lrow-sub", style: "white-space:normal" },
            indexUnavailable ? searchIndex?.error || stats?.open_error : indexDetail,
          ),
        ),
        h(
          "div",
          { class: "lrow-actions" },
          button("重建", {
            variant: "quiet",
            small: true,
            disabled: searchBusy || !searchIndex || !indexEnabled,
            title: indexEnabled ? "清空并按当前会话重建正文索引" : "请先开启正文索引",
            onClick: () => void changeSearchIndex("rebuild"),
          }),
        ),
      ),
      h(
        "div",
        { class: "lrow settings-index-row" },
        h(
          "div",
          { class: "lrow-main" },
          h("div", { class: "lrow-title" }, h("span", { class: "txt", text: "索引文件" })),
          h(
            "div",
            { class: "lrow-sub mono", style: "white-space:normal;overflow-wrap:anywhere" },
            stats?.path || "—",
          ),
        ),
        h(
          "div",
          { class: "lrow-actions" },
          button("复制路径", {
            variant: "quiet",
            small: true,
            iconSvg: icons.copy,
            disabled: !stats?.path,
            onClick: () => void copy(stats?.path || "", "索引路径"),
          }),
          button("删除", {
            variant: "danger",
            small: true,
            disabled: searchBusy || !indexExists,
            title: indexExists ? "关闭正文搜索并删除可重建的本机索引" : "索引文件尚未建立",
            onClick: () => void changeSearchIndex("delete"),
          }),
        ),
      ),
    );

    // 关于
    const aboutCard = listCard(
      row({
        title: "版本",
        sub: h(
          "span",
          { class: "mono" },
          `v${payload.version} · ${payload.runtime === "go" ? "单文件 homing（Go）" : payload.packaged ? "单文件 homingd" : "node relay/server.mjs"} · 已运行 ${uptime(payload.uptime_s)}`,
        ),
        right: [linkButton("关于 Homing", icons.info, () => ctx.navigate("/console/about"))],
      }),
      row({
        title: loc("致谢与打赏支持 ☕", "Acknowledgments & Sponsor ☕"),
        sub: loc("查看开源生态致谢、Apache-2.0 许可声明，或扫码支持公网中继服务器带宽。", "Open-source credits, Apache-2.0 notices, and WeChat / Alipay tip QR codes."),
        right: [linkButton(loc("致谢与打赏", "Credits & Tip"), icons.info, () => ctx.navigate("/console/about"))],
      }),
      row({
        title: "运行环境",
        sub: h("span", { class: "mono", text: `${payload.platform} · ${payload.arch} · ${runtime}` }),
        right: [linkButton("复制版本信息", icons.copy, () => void copy(`${payload.platform} ${payload.arch} ${runtime} homing ${payload.version}`, "版本信息"))],
      }),
    );

    host.append(
      h("div", { class: "group" }, groupHead("常规"), autostartCard),
      h("div", { class: "group" }, groupHead("搜索与隐私"), searchCard),
      h("div", { class: "group" }, groupHead("维护"), maintenanceCard),
      h("div", { class: "group" }, groupHead("关于"), aboutCard),
    );
  }

  async function load() {
    try {
      const [payload, away, settings, index] = await Promise.all([
        endpoints.connection(),
        endpoints.autoAwaySettings().catch(() => autoAway),
        endpoints.homeSettings().catch(() => ({ ok: true, settings: homeSettings })),
        endpoints.searchIndexStatus().catch((err) => ({
          enabled: false,
          error: (err as Error).message || "无法读取正文索引状态",
        })),
      ]);
      if (disposed) return;
      autoAway = away;
      homeSettings = settings.settings;
      searchIndex = index;
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
