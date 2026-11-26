// 轮询 DOM 保护器：防止定时 poll() 重绘销毁正在打字的输入框、打断中文拼音 IME 组词、
// 清除用户正在拖选复制的文本，或在重绘时合上已展开的 <details> 折叠区。

let imeComposing = false;

if (typeof document !== "undefined") {
  document.addEventListener("compositionstart", () => {
    imeComposing = true;
  }, true);
  document.addEventListener("compositionend", () => {
    imeComposing = false;
  }, true);
}

export function isImeComposing(): boolean {
  return imeComposing;
}

/**
 * 判断某个容器当前是否处于用户交互保护期（输入焦点、拼音组词、未提交草稿、
 * 打开了下拉菜单或正在拖选文本）。为 true 时后台轮询应跳过毁灭性 DOM 重绘。
 */
export function isContainerBusy(container: HTMLElement | null): boolean {
  if (!container || !container.isConnected) return false;
  if (imeComposing) return true;
  const active = document.activeElement;
  if (active && container.contains(active)) {
    const tag = active.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") {
      return true;
    }
  }
  const selection = window.getSelection?.();
  if (selection && !selection.isCollapsed && selection.anchorNode && container.contains(selection.anchorNode)) {
    return true;
  }
  return false;
}

/** 记录容器内所有带 data-details-key（或按索引兜底）的展开态 <details> */
export function captureOpenDetails(container: HTMLElement): Set<string> {
  const open = new Set<string>();
  const nodes = container.querySelectorAll<HTMLDetailsElement>("details");
  nodes.forEach((node, index) => {
    if (node.open) {
      open.add(node.dataset.detailsKey || `#idx:${index}`);
    }
  });
  return open;
}

/** 在重绘后恢复先前展开的 <details> */
export function restoreOpenDetails(container: HTMLElement, open: Set<string>): void {
  if (open.size === 0) return;
  const nodes = container.querySelectorAll<HTMLDetailsElement>("details");
  nodes.forEach((node, index) => {
    const key = node.dataset.detailsKey || `#idx:${index}`;
    if (open.has(key)) {
      node.open = true;
    }
  });
}
