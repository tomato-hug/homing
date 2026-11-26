// 安全 Markdown 渲染器（会话正文与会议室共用）：
// 全部经由 textContent / DOM 节点构造，绝不把 Agent 输出作为 HTML 解析。
// 支持：标题(h1-h4)、代码块(含一键复制)、GFM 表格(横向滚动)、有序/无序/任务列表、
// 引用块、行内代码、粗体、斜体、删除线、[文字](url) 命名链接与裸链接。

import { h } from "./dom";
import { t } from "../i18n";

function safeHref(url: string): string | null {
  try {
    const parsed = new URL(url);
    if (parsed.protocol === "http:" || parsed.protocol === "https:") return parsed.href;
  } catch {
    /* ignore */
  }
  return null;
}

function inlineMarkdown(text: string): Node[] {
  const nodes: Node[] = [];
  const pattern =
    /(`[^`\n]+`|\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)|\*\*[^*\n]+\*\*|~~[^~\n]+~~|\*[^*\n]+\*|https?:\/\/[^\s<>"']+)/g;
  let cursor = 0;
  for (const match of text.matchAll(pattern)) {
    const index = match.index ?? 0;
    if (index > cursor) nodes.push(document.createTextNode(text.slice(cursor, index)));
    const token = match[0];
    if (token.startsWith("`")) {
      nodes.push(h("code", { text: token.slice(1, -1) }));
    } else if (token.startsWith("[") && match[2] && match[3]) {
      const href = safeHref(match[3]);
      if (href) {
        nodes.push(h("a", { href, target: "_blank", rel: "noopener noreferrer", text: match[2] }));
      } else {
        nodes.push(document.createTextNode(token));
      }
    } else if (token.startsWith("**")) {
      nodes.push(h("strong", { text: token.slice(2, -2) }));
    } else if (token.startsWith("~~")) {
      nodes.push(h("del", { text: token.slice(2, -2) }));
    } else if (token.startsWith("*")) {
      nodes.push(h("em", { text: token.slice(1, -1) }));
    } else {
      // Strip trailing punctuation like .,;:) that wasn't part of the URL.
      let raw = token;
      let trailing = "";
      while (/[.,;:!?)]$/.test(raw)) {
        if (raw.endsWith(")") && (raw.match(/\(/g)?.length || 0) >= (raw.match(/\)/g)?.length || 0)) break;
        trailing = raw.slice(-1) + trailing;
        raw = raw.slice(0, -1);
      }
      const href = safeHref(raw);
      if (href) {
        nodes.push(h("a", { href, target: "_blank", rel: "noopener noreferrer", text: raw }));
      } else {
        nodes.push(document.createTextNode(raw));
      }
      if (trailing) nodes.push(document.createTextNode(trailing));
    }
    cursor = index + token.length;
  }
  if (cursor < text.length) nodes.push(document.createTextNode(text.slice(cursor)));
  return nodes;
}

function splitTableRow(line: string): string[] | null {
  const trimmed = line.trim();
  if (!trimmed.includes("|")) return null;
  const cleaned = trimmed.replace(/^\|/, "").replace(/\|$/, "");
  const cells: string[] = [];
  let current = "";
  for (let i = 0; i < cleaned.length; i++) {
    if (cleaned[i] === "\\" && cleaned[i + 1] === "|") {
      current += "|";
      i++;
      continue;
    }
    if (cleaned[i] === "|") {
      cells.push(current.trim());
      current = "";
      continue;
    }
    current += cleaned[i];
  }
  cells.push(current.trim());
  return cells.length >= 2 ? cells : null;
}

function isTableSeparator(line: string, expectedCols: number): boolean {
  const cells = splitTableRow(line);
  if (!cells || cells.length !== expectedCols) return false;
  return cells.every((cell) => /^:?-{3,}:?$/.test(cell));
}

function codeBlock(language: string, codeText: string): HTMLElement {
  const copyBtn = h("button", {
    class: "md-code-copy",
    type: "button",
    text: t("sessions.copyCode"),
  });
  copyBtn.addEventListener("click", () => {
    void navigator.clipboard?.writeText(codeText).then(
      () => {
        copyBtn.textContent = t("sessions.codeCopied");
        copyBtn.classList.add("copied");
        window.setTimeout(() => {
          copyBtn.textContent = t("sessions.copyCode");
          copyBtn.classList.remove("copied");
        }, 1600);
      },
      () => {
        copyBtn.textContent = t("common.copyFailed");
      },
    );
  });
  return h(
    "div",
    { class: "md-code" },
    h(
      "div",
      { class: "md-code-head" },
      h("span", { class: "md-code-lang", text: language || "text" }),
      copyBtn,
    ),
    h("pre", null, h("code", { text: codeText })),
  );
}

export function renderMarkdown(text: string): HTMLElement {
  const root = h("div", { class: "md" });
  const lines = String(text || "").replace(/\r\n/g, "\n").split("\n");
  let index = 0;

  while (index < lines.length) {
    const line = lines[index];

    // Fenced code block
    if (/^\s*(```|~~~)/.test(line)) {
      const fence = line.trim().slice(0, 3);
      const language = line.trim().slice(3).trim();
      const code: string[] = [];
      index++;
      while (index < lines.length && !lines[index].trim().startsWith(fence)) {
        code.push(lines[index]);
        index++;
      }
      if (index < lines.length) index++;
      root.append(codeBlock(language, code.join("\n")));
      continue;
    }

    if (!line.trim()) {
      index++;
      continue;
    }

    // Horizontal rule
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) {
      root.append(h("hr", { class: "md-hr" }));
      index++;
      continue;
    }

    // Heading h1-h4
    const heading = /^(#{1,4})\s+(.+)$/.exec(line);
    if (heading) {
      const level = Math.min(4, heading[1].length);
      const tagName = level === 1 ? "h2" : level === 2 ? "h3" : level === 3 ? "h4" : "h5";
      const element = h(tagName);
      element.append(...inlineMarkdown(heading[2]));
      root.append(element);
      index++;
      continue;
    }

    // GFM Table
    const headerCells = splitTableRow(line);
    if (headerCells && index + 1 < lines.length && isTableSeparator(lines[index + 1], headerCells.length)) {
      index += 2;
      const thead = h(
        "thead",
        null,
        h(
          "tr",
          null,
          ...headerCells.map((cell) => {
            const th = h("th");
            th.append(...inlineMarkdown(cell));
            return th;
          }),
        ),
      );
      const tbody = h("tbody");
      while (index < lines.length) {
        const rowCells = splitTableRow(lines[index]);
        if (!rowCells) break;
        const tr = h("tr");
        for (let c = 0; c < headerCells.length; c++) {
          const td = h("td");
          td.append(...inlineMarkdown(rowCells[c] || ""));
          tr.append(td);
        }
        tbody.append(tr);
        index++;
      }
      root.append(h("div", { class: "md-table-wrap" }, h("table", { class: "md-table" }, thead, tbody)));
      continue;
    }

    // Unordered or task list
    if (/^\s*[-*+]\s+/.test(line)) {
      const list = h("ul");
      while (index < lines.length && /^\s*[-*+]\s+/.test(lines[index])) {
        const rawItem = lines[index].replace(/^\s*[-*+]\s+/, "");
        const taskMatch = /^\[( |x|X)\]\s+(.*)$/.exec(rawItem);
        const item = h("li", taskMatch ? { class: "md-task" } : null);
        if (taskMatch) {
          const checked = taskMatch[1].toLowerCase() === "x";
          item.append(h("span", { class: `md-task-box${checked ? " checked" : ""}`, text: checked ? "✓" : "" }));
          item.append(...inlineMarkdown(taskMatch[2]));
        } else {
          item.append(...inlineMarkdown(rawItem));
        }
        list.append(item);
        index++;
      }
      root.append(list);
      continue;
    }

    // Ordered list
    if (/^\s*\d+[.)]\s+/.test(line)) {
      const list = h("ol");
      while (index < lines.length && /^\s*\d+[.)]\s+/.test(lines[index])) {
        const item = h("li");
        item.append(...inlineMarkdown(lines[index].replace(/^\s*\d+[.)]\s+/, "")));
        list.append(item);
        index++;
      }
      root.append(list);
      continue;
    }

    // Blockquote
    if (line.startsWith(">")) {
      const quote = h("blockquote");
      while (index < lines.length && lines[index].startsWith(">")) {
        const part = h("p");
        part.append(...inlineMarkdown(lines[index].replace(/^>\s?/, "")));
        quote.append(part);
        index++;
      }
      root.append(quote);
      continue;
    }

    // Paragraph
    const paragraph: string[] = [line];
    index++;
    while (
      index < lines.length &&
      lines[index].trim() &&
      !/^\s*(```|~~~)/.test(lines[index]) &&
      !/^(#{1,4})\s+/.test(lines[index]) &&
      !/^\s*[-*+]\s+/.test(lines[index]) &&
      !/^\s*\d+[.)]\s+/.test(lines[index]) &&
      !lines[index].startsWith(">") &&
      !(splitTableRow(lines[index]) && index + 1 < lines.length && isTableSeparator(lines[index + 1], splitTableRow(lines[index])!.length))
    ) {
      paragraph.push(lines[index]);
      index++;
    }
    const paragraphEl = h("p");
    paragraph.forEach((part, partIndex) => {
      if (partIndex > 0) paragraphEl.append(document.createElement("br"));
      paragraphEl.append(...inlineMarkdown(part));
    });
    root.append(paragraphEl);
  }
  return root;
}
