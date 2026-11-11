// ACP can offer more than allow/deny (for example "always allow"). Render the
// options the server actually supplied instead of collapsing every decision to
// the legacy two-button pair.

import { h } from "./dom";
import { icons } from "./icons";
import type { HomeApproval } from "./api";
import { button } from "./ui";

export interface ApprovalChoice {
  id: string;
  label: string;
  decision: "allow" | "deny";
}

export function approvalChoices(approval?: HomeApproval): ApprovalChoice[] {
  const supplied = (approval?.options || [])
    .map((option, index) => {
      const id = String(option.id || option.option_id || "").trim();
      const kind = String(option.kind || "").toLowerCase();
      const decision = String(option.decision || "").toLowerCase() === "deny" ||
        /reject|deny/.test(kind)
        ? "deny"
        : "allow";
      return {
        id,
        label: String(option.label || option.name || option.kind || id || `选项 ${index + 1}`),
        decision: decision as ApprovalChoice["decision"],
      };
    })
    .filter((option) => option.label);
  if (supplied.length) return supplied;
  return [
    { id: "allow_once", label: "允許", decision: "allow" },
    { id: "reject_once", label: "拒絕", decision: "deny" },
  ];
}

export function approvalButtons(
  approval: HomeApproval | undefined,
  onChoose: (decision: "allow" | "deny", optionId?: string) => void,
): HTMLElement {
  const host = h("div", { class: "approval-actions approval-options" });
  let firstAllow = true;
  for (const choice of approvalChoices(approval)) {
    const primary = choice.decision === "allow" && firstAllow;
    if (primary) firstAllow = false;
    host.append(
      button(choice.label, {
        small: true,
        variant: primary ? "primary" : choice.decision === "deny" ? "quiet" : "ghost",
        iconSvg: primary ? icons.check : undefined,
        title: choice.id || choice.label,
        onClick: () => onChoose(choice.decision, choice.id || undefined),
      }),
    );
  }
  return host;
}
