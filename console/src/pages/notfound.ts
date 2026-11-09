import { h } from "../lib/dom";
import { button } from "../lib/ui";
import type { PageContext, PageResult } from "../main";

export function renderNotFound(): PageResult {
  const root = h(
    "div",
    { style: "padding:40px 0" },
    h("div", { style: "font-size:18px;font-weight:600;margin-bottom:8px", text: "没有这个页面" }),
    h("div", { class: "note", text: "地址可能拼错了，回概览看看。" }),
  );
  return { el: root };
}

export function notFoundWithNav(ctx: PageContext): PageResult {
  const result = renderNotFound();
  result.el.append(
    h(
      "div",
      { style: "margin-top:16px" },
      button("回概览", { variant: "primary", onClick: () => ctx.navigate("/console/") }),
    ),
  );
  return result;
}
