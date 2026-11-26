// Legacy /console/setup route: redirects cleanly to /console/hub where E2E QR pairing and agent setup live.

import { h } from "../lib/dom";
import { loc } from "../i18n";
import type { PageContext, PageResult } from "../main";

export function renderSetup(ctx: PageContext): PageResult {
  const root = h(
    "div",
    { class: "wizard", style: "padding:32px;text-align:center" },
    h("p", { class: "note", text: loc("正在前往配置与配对页…", "Redirecting to Setup & Pair…") }),
  );
  window.setTimeout(() => ctx.navigate("/console/hub", { replace: true }), 0);
  return { el: root };
}
