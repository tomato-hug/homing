// 手机 › 通知：今天推了多少、暂停开关、分级规则、最近的推送。

import { h, clear } from "../lib/dom";
import { icons } from "../lib/icons";
import { kindLabel, levelOf, timeAgo, truncate } from "../lib/format";
import { endpoints, poll, type HomingEvent, type StatsPayload } from "../lib/api";
import { button, emptyState, groupHead, listCard, reportError, tag, toast } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

const LEVEL_RULES: Array<{ level: "P0" | "P1" | "P2"; trigger: string; policy: string; why: string }> = [
  { level: "P0", trigger: "请求授权 / 出错", policy: "立即单条推送", why: "最要紧的先到" },
  { level: "P1", trigger: "等你回复", policy: "同一会话 120 秒内合并成一条", why: "同一件事不刷屏" },
  { level: "P2", trigger: "完成 / 停止", policy: "60 秒窗口合进「N 个会话已完成」摘要", why: "不打断你" },
];

function projectName(cwd?: string): string {
  if (!cwd) return "—";
  const parts = cwd.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || "—";
}

export function renderNotifications(_ctx: PageContext): PageResult {
  const root = h("div");
  let disposed = false;

  const stateHost = h("div");
  const recentHost = h("div");

  const rulesCard = listCard(
    ...LEVEL_RULES.map((rule) =>
      h(
        "div",
        { class: "lrow" },
        h("div", { class: "lrow-actions", style: "padding-top:1px;width:34px" }, tag(rule.level, rule.level === "P0" ? "bad" : rule.level === "P1" ? "warn" : "plain")),
        h(
          "div",
          { class: "lrow-main" },
          h("div", { class: "lrow-title" }, h("span", { class: "txt", text: rule.trigger })),
          h("div", { class: "lrow-sub", style: "white-space:normal" }, h("span", { text: rule.policy }), h("span", { class: "mono", text: ` · ${rule.why}` })),
        ),
      ),
    ),
  );

  root.append(
    h("div", { class: "group" }, groupHead("推送状态"), stateHost),
    h("div", { class: "group" }, groupHead("推送分级"), rulesCard),
    h("div", { class: "group" }, groupHead("最近的推送"), recentHost),
  );

  async function pause(minutes: number) {
    try {
      const state = await endpoints.pausePush(minutes);
      toast(`推送已暂停到 ${new Date(state.until || Date.now()).toLocaleTimeString()}`, "ok");
      await refresh();
    } catch (err) {
      reportError(err, "暂停失败");
    }
  }

  async function resume() {
    try {
      await endpoints.resumePush();
      toast("推送已恢复", "ok");
      await refresh();
    } catch (err) {
      reportError(err, "恢复失败");
    }
  }

  function paintState(payload: StatsPayload) {
    const { notify, pause: paused } = payload;
    clear(stateHost);
    stateHost.append(
      listCard(
        h(
          "div",
          { class: "lrow", style: "align-items:center" },
          h(
            "div",
            { class: "lrow-main" },
            h(
              "div",
              { class: "lrow-title" },
              h("span", { class: "txt", text: "今天推送到手机" }),
              h("span", { class: "lrow-time", text: notify.day }),
            ),
            h(
              "div",
              { class: "lrow-sub", style: "white-space:normal" },
              h("span", { text: `${notify.pushed} 条` }),
              h("span", { class: "mono", text: ` · 合并掉 ${notify.merged} 条` }),
              h("span", { class: "mono", text: ` · P0 ${notify.by_level.P0 || 0} / P1 ${notify.by_level.P1 || 0} / P2 ${notify.by_level.P2 || 0}` }),
            ),
          ),
          h("div", { class: "lrow-actions" }, tag(`${payload.devices} 台设备`, payload.devices > 0 ? "ok" : "mute")),
        ),
        h(
          "div",
          { class: "lrow", style: "align-items:center" },
          h(
            "div",
            { class: "lrow-main" },
            h(
              "div",
              { class: "lrow-title" },
              h("span", { class: "txt", text: "暂停推送" }),
              paused.paused ? tag(`已暂停 ${paused.minutes} 分钟`, "warn") : tag("推送正常", "ok"),
            ),
            h(
              "div",
              { class: "lrow-sub", style: "white-space:normal" },
              paused.paused
                ? `到 ${paused.until ? new Date(paused.until).toLocaleTimeString() : "—"} 自动恢复。事件照常进收件箱，只是不弹通知。`
                : "暂停期间不再往手机发通知，电脑上的收件箱照常更新。",
            ),
          ),
          h(
            "div",
            { class: "lrow-actions" },
            paused.paused
              ? button("立即恢复", { small: true, variant: "primary", iconSvg: icons.power, onClick: () => void resume() })
              : h(
                  "div",
                  { style: "display:flex;gap:6px" },
                  button("30 分钟", { small: true, variant: "quiet", iconSvg: icons.pause, onClick: () => void pause(30) }),
                  button("1 小时", { small: true, variant: "quiet", onClick: () => void pause(60) }),
                ),
          ),
        ),
      ),
    );
  }

  function paintRecent(events: HomingEvent[]) {
    const notifyish = events
      .filter((event) => levelOf(event.kind) !== null)
      .sort((a, b) => (b.id || 0) - (a.id || 0))
      .slice(0, 20);

    if (notifyish.length === 0) {
      recentHost.replaceChildren(
        h("div", { class: "list-card" }, emptyState("今天还没有推送", "agent 请求授权、等你回复或出错时才会推")),
      );
      return;
    }

    recentHost.replaceChildren(
      listCard(
        ...notifyish.map((event) => {
          const level = levelOf(event.kind) || "P2";
          const kind = kindLabel(event.kind);
          return h(
            "div",
            { class: "lrow" },
            h(
              "div",
              { class: "lrow-main" },
              h(
                "div",
                { class: "lrow-title" },
                h("span", { class: `tag ${level === "P0" ? "tag-bad" : level === "P1" ? "tag-warn" : ""}`.trim(), text: level }),
                h("span", { class: "txt", text: kind.text }),
                h("span", { class: "note", text: projectName(event.cwd) }),
              ),
              h("div", { class: "lrow-sub", style: "white-space:normal", text: truncate(event.text || "已经推送到手机", 110) }),
            ),
            h("span", { class: "lrow-time", text: timeAgo(event.ts) }),
          );
        }),
      ),
    );
  }

  async function refresh() {
    const [statsPayload, events] = await Promise.all([endpoints.stats(), endpoints.events()]);
    if (disposed) return;
    paintState(statsPayload);
    paintRecent(events);
  }

  stateHost.append(h("div", { class: "list-card", style: "padding:16px" }, h("div", { class: "note", text: "正在读取推送统计…" })));
  const stop = poll(() => refresh().catch(() => {}), 8000);
  return {
    el: root,
    dispose: () => {
      disposed = true;
      stop();
    },
  };
}
