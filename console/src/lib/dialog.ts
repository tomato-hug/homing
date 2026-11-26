// 统一非阻塞 <dialog> 模态框：替换全部原生 window.confirm / window.prompt，
// 不阻塞 JS 主线程和 SSE 连接，支持键盘 Esc、⌘Enter 确认与焦點安全归还。

import { h } from "./dom";
import { t } from "../i18n";

export interface ConfirmOptions {
  title: string;
  message: string;
  detail?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}

export interface PromptOptions {
  title: string;
  message?: string;
  value?: string;
  defaultValue?: string;
  placeholder?: string;
  multiline?: boolean;
  confirmLabel?: string;
  cancelLabel?: string;
}

export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const dialog = h("dialog", { class: "hm-dialog", "aria-modal": "true" }) as HTMLDialogElement;
    let settled = false;
    const finish = (result: boolean) => {
      if (settled) return;
      settled = true;
      try {
        if (dialog.open) dialog.close();
      } catch {
        /* ignore */
      }
      dialog.remove();
      if (previousFocus && previousFocus.isConnected) {
        previousFocus.focus();
      }
      resolve(result);
    };

    const cancelBtn = h("button", {
      class: "btn btn-quiet btn-sm",
      type: "button",
      text: options.cancelLabel || t("common.cancel"),
    });
    const okBtn = h("button", {
      class: `btn btn-sm ${options.danger ? "btn-danger" : "btn-primary"}`,
      type: "button",
      text: options.confirmLabel || t("common.confirm"),
    });
    cancelBtn.addEventListener("click", () => finish(false));
    okBtn.addEventListener("click", () => finish(true));

    dialog.append(
      h("div", { class: "hm-dialog-head" }, h("h3", { text: options.title })),
      h("p", { class: "hm-dialog-msg", text: options.message }),
    );
    if (options.detail) {
      dialog.append(h("pre", { class: "hm-dialog-detail" }, h("code", { text: options.detail })));
    }
    dialog.append(h("div", { class: "hm-dialog-foot" }, cancelBtn, okBtn));
    dialog.addEventListener("cancel", (event) => {
      event.preventDefault();
      finish(false);
    });
    dialog.addEventListener("click", (event) => {
      if (event.target === dialog) finish(false);
    });

    document.body.append(dialog);
    dialog.showModal();
    window.requestAnimationFrame(() => (options.danger ? cancelBtn : okBtn).focus());
  });
}

export function promptDialog(options: PromptOptions): Promise<string | null> {
  return new Promise((resolve) => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const dialog = h("dialog", { class: "hm-dialog hm-dialog-prompt", "aria-modal": "true" }) as HTMLDialogElement;
    let settled = false;
    const finish = (result: string | null) => {
      if (settled) return;
      settled = true;
      try {
        if (dialog.open) dialog.close();
      } catch {
        /* ignore */
      }
      dialog.remove();
      if (previousFocus && previousFocus.isConnected) {
        previousFocus.focus();
      }
      resolve(result);
    };

    const field = options.multiline
      ? (h("textarea", {
          class: "input hm-dialog-textarea",
          rows: "6",
          placeholder: options.placeholder || "",
        }) as HTMLTextAreaElement)
      : (h("input", {
          class: "input hm-dialog-input",
          type: "text",
          placeholder: options.placeholder || "",
        }) as HTMLInputElement);
    field.value = options.value ?? options.defaultValue ?? "";

    const cancelBtn = h("button", {
      class: "btn btn-quiet btn-sm",
      type: "button",
      text: options.cancelLabel || t("common.cancel"),
    });
    const okBtn = h("button", {
      class: "btn btn-primary btn-sm",
      type: "submit",
      text: options.confirmLabel || t("common.confirm"),
    });
    cancelBtn.addEventListener("click", () => finish(null));

    field.addEventListener("keydown", (event) => {
      const ke = event as KeyboardEvent;
      if (ke.isComposing) return;
      if (options.multiline) {
        if (ke.key === "Enter" && (ke.metaKey || ke.ctrlKey)) {
          ke.preventDefault();
          finish(field.value);
        }
      } else if (ke.key === "Enter") {
        ke.preventDefault();
        finish(field.value);
      }
    });

    const form = h(
      "form",
      {
        class: "hm-dialog-form",
        onSubmit: (event: Event) => {
          event.preventDefault();
          finish(field.value);
        },
      },
      h("div", { class: "hm-dialog-head" }, h("h3", { text: options.title })),
      options.message ? h("p", { class: "hm-dialog-msg", text: options.message }) : null,
      field,
      h("div", { class: "hm-dialog-foot" }, cancelBtn, okBtn),
    );

    dialog.append(form);
    dialog.addEventListener("cancel", (event) => {
      event.preventDefault();
      finish(null);
    });
    dialog.addEventListener("click", (event) => {
      if (event.target === dialog) finish(null);
    });

    document.body.append(dialog);
    dialog.showModal();
    window.requestAnimationFrame(() => {
      field.focus();
      if (!options.multiline) field.select();
    });
  });
}
