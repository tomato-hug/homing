import type { PageContext, PageResult } from "../main";
import { h, clear, icon } from "../lib/dom";
import { icons } from "../lib/icons";
import { agentLogo } from "../lib/agent-icons";
import { endpoints, poll, type Room, type RoomAction, type RoomAgentChoice, type RoomParticipant } from "../lib/api";
import { confirmDialog } from "../lib/dialog";
import { isContainerBusy } from "../lib/guard";
import { renderMarkdown } from "../lib/markdown";
import { button, card, emptyState, pill, reportError, tag, toast } from "../lib/ui";
import { loc, t } from "../i18n";

const statusText = (r: Room) =>
  ({
    running: r.round ? t("rooms.status.running", { round: r.round, rounds: r.rounds }) : t("rooms.status.preparing"),
    paused: t("rooms.status.paused"),
    summarizing: t("rooms.status.summarizing"),
    ended: t("rooms.status.ended"),
  })[r.status] || r.status;

const participantText = (p: RoomParticipant) =>
  ({
    speaking: t("rooms.speaking"),
    needs_you: t("rooms.needsYou"),
    failed: t("rooms.failed"),
    waiting: t("rooms.waiting"),
  })[p.status] || p.status;

function actionTone(status: string): "ok" | "run" | "warn" | "bad" | "mute" {
  if (status === "completed") return "ok";
  if (status === "running" || status === "started" || status === "starting" || status === "preparing" || status === "submitting" || status === "launching") return "run";
  if (status === "needs_you" || status === "waiting_session" || status === "unknown") return "warn";
  if (status === "failed" || status === "prepare_failed") return "bad";
  return "mute";
}

function actionLabel(status: string): string {
  const map: Record<string, [string, string]> = {
    suggested: ["待确认派发", "Suggested"],
    preparing: ["准备 Worktree", "Preparing"],
    prepare_failed: ["准备失败", "Prepare Failed"],
    submitting: ["正在提交", "Submitting"],
    launching: ["正在启动", "Launching"],
    starting: ["准备启动", "Starting"],
    waiting_session: ["等待会话绑定", "Binding Session"],
    started: ["已启动", "Started"],
    running: ["运行中", "Running"],
    needs_you: ["等你处理", "Needs You"],
    completed: ["已完成", "Completed"],
    failed: ["失败", "Failed"],
    cancelled: ["已取消", "Cancelled"],
    unknown: ["状态待确认", "Unknown"],
  };
  const pair = map[status];
  return pair ? loc(pair[0], pair[1]) : status;
}

function agentBadgeEl(agentId: string, name: string): HTMLElement {
  const logo = agentLogo(agentId);
  if (logo) {
    const el = h("span", { class: `agent-square sm logo${logo.mono ? " mono" : ""}`, title: name });
    if (logo.color) el.style.color = logo.color;
    el.append(icon(logo.svg, 16));
    return el;
  }
  return h("span", { class: "agent-square sm", text: (name || agentId || "?").slice(0, 2).toUpperCase(), title: name });
}

function callableParticipants(r: Room): RoomParticipant[] {
  if (!["running", "paused"].includes(r.status) || r.end_requested) return [];
  const spoken = new Set((r.messages || []).filter((m) => m.kind === "statement" || m.kind === "system").map((m) => `${m.from}#${m.round}`));
  if (r.speaking && r.round) spoken.add(`${r.speaking}#${r.round}`);
  for (let n = 1; n <= r.rounds; n++) {
    if (r.participants.some((p) => p.status !== "failed" && !spoken.has(`${p.agent}#${n}`))) {
      return r.participants.filter((p) => p.agent !== r.speaking && p.status !== "failed" && !spoken.has(`${p.agent}#${n}`));
    }
  }
  return [];
}

export function renderRooms(ctx: PageContext, query: URLSearchParams): PageResult {
  let disposed = false,
    busy = false,
    epoch = 0;
  let selected = query.get("id") || "";
  let room: Room | null = null;
  let agents: RoomAgentChoice[] = [];
  let projectPaths: string[] = [];
  let loadError = "",
    actionError = "",
    listSignature = "",
    transcriptSignature = "",
    callSignature = "",
    minutesSignature = "",
    actionsSignature = "";
  const actionDrafts = new Map<string, { agent: string; text: string }>();

  const error = h("div", { class: "rooms-error", role: "alert", "aria-live": "polite", hidden: true });
  const list = h("div", { class: "rooms-list", "aria-label": t("rooms.listHead") }, emptyState(t("common.loading")));
  const title = h("h2", { text: t("rooms.selectRoom") });
  const status = h("div", { class: "room-status", "aria-live": "polite" });
  const topic = h("p", { class: "room-topic" });
  const participants = h("div", { class: "room-participants" });
  const transcript = h("div", { class: "room-transcript", "aria-label": t("rooms.title") });
  const minutesHead = h("div", { class: "room-minutes-head" });
  const minutes = h("div", { class: "room-minutes md" });
  const actionsHost = h("div", { class: "room-actions-panel", hidden: true });
  const metadata = h("div", { class: "note room-metadata" });
  const pending = h("div", { class: "note", "aria-live": "polite" });
  const callSelect = h("select", { "aria-label": t("rooms.call") });
  const callButton = button(t("rooms.call"), {
    onClick: () => {
      if (room && callSelect.value) void act(() => endpoints.controlRoom(room!.id, "call", callSelect.value));
    },
  });
  const pause = button(t("rooms.pause"), { onClick: () => control("pause") });
  const resume = button(t("rooms.resume"), { onClick: () => control("resume") });
  const extend = button(t("rooms.extend"), { onClick: () => control("extend") });
  const end = button(t("rooms.end"), { variant: "danger", onClick: () => control("end") });
  const input = h("textarea", {
    rows: 3,
    maxLength: 4000,
    placeholder: t("rooms.sayPlaceholder"),
    "aria-label": t("rooms.say"),
  }) as HTMLTextAreaElement;
  const send = h("button", { class: "btn btn-primary", type: "submit", text: t("rooms.say") });
  input.addEventListener("input", () => updateControls());
  input.addEventListener("keydown", (event) => {
    const ke = event as KeyboardEvent;
    if (ke.isComposing) return;
    if (ke.key === "Enter" && (ke.metaKey || ke.ctrlKey)) {
      ke.preventDefault();
      form.requestSubmit();
    }
  });
  const form = h(
    "form",
    {
      class: "room-message-form",
      onSubmit: (e: Event) => {
        e.preventDefault();
        const text = input.value.trim();
        if (room && text) {
          void act(() => endpoints.sayInRoom(room!.id, text)).then((ok) => {
            if (ok && input.value.trim() === text) input.value = "";
            updateControls();
          });
        }
      },
    },
    input,
    send,
  ) as HTMLFormElement;
  const controls = h(
    "div",
    { class: "room-controls" },
    pending,
    h("div", { class: "room-actions" }, callSelect, callButton, pause, resume, extend, end),
    form,
  );
  const newButton = button(t("rooms.create"), { variant: "primary", onClick: () => showCreate() });
  const dialog = h("dialog", { class: "room-create-dialog", "aria-label": t("rooms.create") }) as HTMLDialogElement;
  const root = h(
    "div",
    { class: "rooms-page" },
    h(
      "div",
      { class: "rooms-head" },
      h("div", null, h("h1", { text: t("rooms.title") }), h("p", { class: "note", text: t("rooms.subtitle") })),
      newButton,
    ),
    error,
    h(
      "div",
      { class: "rooms-grid" },
      card(t("rooms.listHead"), list),
      h(
        "section",
        { class: "card room-discussion", "aria-label": t("rooms.title") },
        h("div", { class: "room-heading" }, title, status),
        topic,
        participants,
        transcript,
        controls,
      ),
      card(t("rooms.minutesHead"), metadata, minutesHead, minutes, actionsHost),
    ),
    dialog,
  );

  function showError() {
    error.textContent = actionError || loadError;
    error.hidden = !error.textContent;
  }
  function control(action: string) {
    if (room) void act(() => endpoints.controlRoom(room!.id, action));
  }

  function updateControls() {
    const active = !!room && ["running", "paused"].includes(room.status) && !room.end_requested;
    controls.hidden = !room;
    pause.hidden = !active || room!.status !== "running";
    resume.hidden = !room || room.status !== "paused";
    resume.textContent = room?.end_requested ? t("rooms.resumeMinutes") : t("rooms.resume");
    extend.hidden = end.hidden = callButton.hidden = callSelect.hidden = !active;
    form.hidden = !active;
    for (const b of [pause, resume, end]) b.disabled = busy;
    extend.disabled = busy || (room?.rounds || 0) >= 5;
    const choices = room ? callableParticipants(room) : [];
    const signature = JSON.stringify(choices.map((p) => [p.agent, p.name]));
    if (signature !== callSignature) {
      const previous = callSelect.value;
      clear(callSelect);
      callSelect.append(
        h("option", {
          value: "",
          text: choices.length ? loc("选择下一位", "Select next speaker") : loc("本轮暂无可点名者", "No callable speaker left"),
        }),
        ...choices.map((p) => h("option", { value: p.agent, text: p.name })),
      );
      if (choices.some((p) => p.agent === previous)) callSelect.value = previous;
      callSignature = signature;
    }
    callSelect.disabled = busy || !choices.length;
    callButton.disabled = busy || !callSelect.value || !choices.some((p) => p.agent === callSelect.value);
    input.disabled = !active;
    send.disabled = busy || !active || !input.value.trim();
    newButton.disabled = busy || agents.length < 2;
    newButton.title = agents.length < 2 ? t("rooms.needTwoAgents") : "";
    pending.textContent =
      room?.status === "ended"
        ? loc("会议已结束，可在右侧查看纪要并派发行动项", "Meeting ended. Review minutes and dispatch action items on the right.")
        : room?.end_requested
          ? loc("当前发言结束后整理纪要", "Minutes will be summarized once the current speaker finishes.")
          : room?.status === "summarizing"
            ? loc("正在整理纪要，暂不接受插话", "Summarizing minutes…")
            : room?.next_speaker
              ? loc(
                  `下一位：${room.participants.find((p) => p.agent === room!.next_speaker)?.name || room.next_speaker}${room.status === "paused" ? "；点继续后执行" : ""}`,
                  `Next: ${room.participants.find((p) => p.agent === room!.next_speaker)?.name || room.next_speaker}${room.status === "paused" ? " (will run on resume)" : ""}`,
                )
              : loc("点名只调整既定顺序，不增加轮次；暂停不会打断当前发言。", "Calling a speaker reorders remaining turns without adding rounds.");
  }
  callSelect.addEventListener("change", updateControls);

  async function dispatchAction(r: Room, item: RoomAction, targetAgent: string, taskText: string) {
    const cleanText = taskText.trim();
    if (!targetAgent || !cleanText) {
      toast(loc("请选择目标 Agent 并填写任务内容", "Select an agent and enter task text"), "bad");
      return;
    }
    const ok = await confirmDialog({
      title: loc("确认派发行动项", "Confirm Action Dispatch"),
      message: loc(
        `将向 ${targetAgent} 派发此行动项，并在独立 Git Worktree 中执行（基准提交：${item.base_commit ? item.base_commit.slice(0, 7) : "当前 HEAD"}）。确认继续？`,
        `Dispatch this action item to ${targetAgent} in an isolated Git Worktree (base: ${item.base_commit ? item.base_commit.slice(0, 7) : "HEAD"})?`,
      ),
      detail: cleanText,
      confirmLabel: t("rooms.dispatchAction"),
    });
    if (!ok || disposed) return;
    const success = await act(() =>
      endpoints.dispatchRoomAction(r.id, item.id, {
        confirmed: true,
        agent: targetAgent,
        text: cleanText,
        base_commit: item.base_commit || undefined,
      }),
    );
    if (success) {
      toast(t("rooms.dispatched"), "ok");
    }
  }

  function paintActions(r: Room) {
    const items = r.actions || [];
    const sig = JSON.stringify(items);
    if (sig === actionsSignature) return;
    if (isContainerBusy(actionsHost)) return;
    actionsSignature = sig;
    clear(actionsHost);
    if (r.status !== "ended" || items.length === 0) {
      actionsHost.hidden = true;
      return;
    }
    actionsHost.hidden = false;
    actionsHost.append(
      h("h3", { style: "margin:0;font-size:14px", text: `${t("rooms.actionsHead")} (${items.length})` }),
      h("p", { class: "note", style: "margin:0", text: t("rooms.actionsHint") }),
    );
    for (const item of items) {
      const editable = item.status === "suggested" || item.status === "prepare_failed";
      const draftKey = `${r.id}:${item.id}`;
      const savedDraft = actionDrafts.get(draftKey) || {
        agent: item.agent || r.participants[0]?.agent || "",
        text: item.text,
      };
      const cardEl = h("div", { class: `room-action-card ${item.status}` });
      cardEl.append(
        h(
          "div",
          { class: "room-action-controls" },
          tag(actionLabel(item.status), actionTone(item.status)),
          item.agent ? h("span", { class: "mono note", text: `@${item.agent}` }) : null,
          item.base_commit ? h("span", { class: "mono note", text: item.base_commit.slice(0, 7), title: item.base_commit }) : null,
          item.session_id
            ? button(t("rooms.openSession"), {
                small: true,
                variant: "quiet",
                onClick: () => ctx.navigate(`/console/sessions?session=${encodeURIComponent(item.session_id!)}`),
              })
            : null,
        ),
      );
      if (editable) {
        const agentSel = h(
          "select",
          { "aria-label": loc("执行 Agent", "Target Agent") },
          ...r.participants.map((p) => h("option", { value: p.agent, text: p.name })),
        ) as HTMLSelectElement;
        agentSel.value = savedDraft.agent || r.participants[0]?.agent || "";
        const taskInput = h("textarea", {
          rows: 2,
          value: savedDraft.text,
          "aria-label": loc("行动项任务", "Action task"),
        }) as HTMLTextAreaElement;
        taskInput.value = savedDraft.text;
        const syncDraft = () => actionDrafts.set(draftKey, { agent: agentSel.value, text: taskInput.value });
        agentSel.addEventListener("change", syncDraft);
        taskInput.addEventListener("input", syncDraft);
        cardEl.append(
          taskInput,
          h(
            "div",
            { class: "room-action-controls" },
            agentSel,
            button(t("rooms.dispatchAction"), {
              small: true,
              variant: "primary",
              disabled: busy,
              onClick: () => void dispatchAction(r, item, agentSel.value, taskInput.value),
            }),
          ),
        );
      } else {
        cardEl.append(h("div", { style: "font-size:13px;color:var(--text)", text: item.text }));
      }
      if (item.error) {
        cardEl.append(h("div", { class: "bad-text note", text: item.error }));
      }
      if (item.result) {
        cardEl.append(h("div", { class: "room-action-result" }, renderMarkdown(item.result)));
      }
      actionsHost.append(cardEl);
    }
  }

  function paintRoom() {
    if (!room) {
      transcript.replaceChildren(emptyState(t("rooms.empty")));
      clear(minutesHead);
      minutes.textContent = loc("结束会议后，这里会显示纪要与行动项。", "Minutes and action items appear here after the meeting ends.");
      actionsHost.hidden = true;
      updateControls();
      return;
    }
    title.textContent = room.title;
    topic.textContent = room.topic;
    clear(status);
    status.append(pill(statusText(room), room.status === "running" ? "run" : room.status === "paused" ? "warn" : "mute"));
    if (room.note) status.append(h("p", { class: "note", text: room.note }));
    clear(participants);
    for (const p of room.participants) {
      const item = h(
        "div",
        { class: `room-participant ${p.status}` },
        h(
          "div",
          { style: "display:flex;align-items:center;gap:6px" },
          agentBadgeEl(p.agent, p.name),
          h("strong", { text: p.role ? `${p.name}（${p.role}）` : p.name }),
        ),
        h("span", { class: "note", text: participantText(p) }),
      );
      if (p.session_id) {
        item.append(
          button(p.status === "needs_you" ? t("rooms.handleSession") : t("rooms.openSession"), {
            small: true,
            onClick: () => ctx.navigate(`/console/sessions?session=${encodeURIComponent(p.session_id!)}`),
          }),
        );
      }
      if (p.status === "failed") {
        item.append(h("span", { class: "note", text: loc("请查看下方失败记录；本场会跳过这位参会者。", "See failure below; skipped for this room.") }));
      }
      participants.append(item);
    }

    const signature = JSON.stringify(room.messages || []);
    if (signature !== transcriptSignature) {
      transcriptSignature = signature;
      clear(transcript);
      for (const m of room.messages || []) {
        if (m.kind === "minutes") continue;
        const bodyEl = m.kind === "system" ? h("div", { class: "room-message-text", text: m.text }) : h("div", { class: "room-message-text md" }, renderMarkdown(m.text));
        transcript.append(
          h(
            "article",
            { class: `room-message ${m.kind}` },
            h(
              "div",
              { class: "room-message-head" },
              h(
                "div",
                { style: "display:flex;align-items:center;gap:6px" },
                m.from && m.from !== "user" && m.from !== "system" ? agentBadgeEl(m.from, m.name || m.from) : null,
                h("strong", { text: m.name || m.from }),
              ),
              h("span", { class: "note", text: t("rooms.round", { n: m.round }) }),
            ),
            bodyEl,
          ),
        );
      }
      if (!transcript.childElementCount) {
        transcript.append(
          emptyState(
            loc("还没有发言", "No statements yet"),
            room.speaking
              ? loc("正在等待当前参会者完成发言…", "Waiting for the current speaker to finish…")
              : loc("会议记录会显示在这里。", "Statements will appear here."),
          ),
        );
      }
    }

    const minSig = `${room.status}|${room.minutes || ""}`;
    if (minSig !== minutesSignature) {
      minutesSignature = minSig;
      clear(minutesHead);
      clear(minutes);
      if (room.minutes) {
        minutesHead.append(
          h("span", { class: "note", text: "MINUTES.md" }),
          button(t("rooms.copyMinutes"), {
            small: true,
            variant: "quiet",
            iconSvg: icons.copy,
            onClick: () => {
              void navigator.clipboard?.writeText(room!.minutes || "").then(
                () => toast(t("common.copied"), "ok"),
                () => toast(t("common.copyFailed"), "bad"),
              );
            },
          }),
        );
        minutes.append(renderMarkdown(room.minutes));
      } else {
        minutes.textContent =
          room.status === "summarizing"
            ? loc("正在整理纪要…", "Summarizing minutes…")
            : loc("结束会议后，这里会显示纪要与行动项。", "Minutes and action items appear here after the meeting ends.");
      }
    }
    paintActions(room);
    metadata.textContent = `${loc("只读讨论", "Read-only discussion")} · ${room.rounds} ${loc("轮", "rounds")}\n${room.cwd}`;
    updateControls();
  }

  async function refresh() {
    if (disposed || busy) return;
    const observed = ++epoch;
    try {
      const data = await endpoints.rooms();
      if (disposed || observed !== epoch) return;
      agents = data.agents || [];
      const rooms = data.rooms || [];
      if (!selected && rooms.length) selected = rooms[0].id;
      const signature = JSON.stringify([selected, rooms]);
      if (signature !== listSignature) {
        listSignature = signature;
        clear(list);
        rooms.sort((a, b) => Number(a.status === "ended") - Number(b.status === "ended"));
        for (const r of rooms) {
          const item = h(
            "button",
            {
              type: "button",
              class: `room-list-item${r.id === selected ? " selected" : ""}`,
              onClick: () => ctx.navigate(`/console/rooms?id=${encodeURIComponent(r.id)}`),
            },
            h("strong", { text: r.title }),
            h("span", { class: "note", text: statusText(r) }),
            h("span", { class: "note", text: r.preview || r.participants.map((p) => p.name).join(" / ") }),
          );
          list.append(item);
        }
        if (!rooms.length) {
          list.append(emptyState(t("rooms.noRooms"), agents.length >= 2 ? t("rooms.inviteTwo") : t("rooms.needTwoAgents")));
        }
      }
      if (selected) {
        const result = await endpoints.room(selected);
        if (disposed || observed !== epoch) return;
        room = result.room;
      }
      loadError = "";
      showError();
      paintRoom();
    } catch (e) {
      if (!disposed && observed === epoch) {
        loadError = String((e as Error).message || e);
        showError();
        updateControls();
      }
    }
  }

  async function act(operation: () => Promise<{ room: Room }>): Promise<boolean> {
    if (busy || disposed) return false;
    busy = true;
    ++epoch;
    actionError = "";
    showError();
    updateControls();
    try {
      const result = await operation();
      if (disposed) return false;
      room = result.room;
      paintRoom();
      return true;
    } catch (e) {
      if (!disposed) {
        actionError = String((e as Error).message || e);
        showError();
        reportError(e);
      }
      return false;
    } finally {
      busy = false;
      if (!disposed) {
        updateControls();
        void refresh();
      }
    }
  }

  function showCreate() {
    if (busy || agents.length < 2) return;
    clear(dialog);
    const topicInput = h("textarea", { rows: 3, maxLength: 4000, required: true, id: "room-topic" }) as HTMLTextAreaElement;
    const datalist = h("datalist", { id: "room-cwd-list" }, ...projectPaths.map((p) => h("option", { value: p })));
    const cwd = h("input", {
      type: "text",
      required: true,
      list: "room-cwd-list",
      placeholder: projectPaths[0] || "/path/to/project",
      value: projectPaths[0] || "",
      id: "room-cwd",
    }) as HTMLInputElement;
    const rounds = h(
      "select",
      { id: "room-rounds" },
      ...[1, 2, 3, 4, 5].map((n) => h("option", { value: String(n), text: loc(`${n} 轮`, `${n} round${n > 1 ? "s" : ""}`) })),
    ) as HTMLSelectElement;
    rounds.value = "2";
    const seats = agents.map((a, i) => ({
      agent: a.id,
      checkbox: h("input", { type: "checkbox", checked: i < 2 }) as HTMLInputElement,
      role: h("input", { type: "text", maxLength: 40, placeholder: loc("角色（可选）", "Role (optional)"), "aria-label": `${a.name} role` }) as HTMLInputElement,
      name: a.name,
    }));
    const problem = h("p", { class: "rooms-error", role: "alert" });
    const cancel = button(t("common.cancel"), { onClick: () => dialog.close() });
    const submit = h("button", { class: "btn btn-primary", type: "submit", text: loc("开始会议", "Start Room") });
    const create = h(
      "form",
      {
        class: "room-create-form",
        onSubmit: async (e: Event) => {
          e.preventDefault();
          if (busy) return;
          const chosen = seats.filter((s) => s.checkbox.checked);
          if (chosen.length < 2 || chosen.length > 4) {
            problem.textContent = loc("请选择 2–4 位参会者", "Select 2–4 participants");
            return;
          }
          busy = true;
          ++epoch;
          updateControls();
          submit.disabled = cancel.disabled = true;
          problem.textContent = "";
          try {
            const result = await endpoints.createRoom({
              topic: topicInput.value.trim(),
              cwd: cwd.value.trim(),
              rounds: Number(rounds.value),
              participants: chosen.map((s) => ({ agent: s.agent, role: s.role.value.trim() })),
            });
            if (!disposed) ctx.navigate(`/console/rooms?id=${encodeURIComponent(result.room.id)}`);
          } catch (err) {
            if (!disposed) problem.textContent = String((err as Error).message || err);
          } finally {
            busy = false;
            submit.disabled = cancel.disabled = false;
            if (!disposed) updateControls();
          }
        },
      },
      h("h2", { text: t("rooms.create") }),
      h("label", { for: "room-topic", text: loc("议题", "Topic") }),
      topicInput,
      h("label", { for: "room-cwd", text: loc("工作目录（只读讨论，派发行动项时自动建 Worktree）", "Project Directory (read-only; actions use Git Worktree)") }),
      cwd,
      datalist,
      h("label", { for: "room-rounds", text: loc("轮数", "Rounds") }),
      rounds,
      h(
        "fieldset",
        null,
        h("legend", { text: loc("参会者（2–4 位）", "Participants (2–4)") }),
        ...seats.map((s) => h("div", { class: "room-create-seat" }, h("label", null, s.checkbox, s.name), s.role)),
      ),
      problem,
      h("div", { class: "room-actions" }, cancel, submit),
    );
    dialog.append(create);
    dialog.showModal();
  }
  dialog.addEventListener("cancel", (e) => {
    if (busy) e.preventDefault();
  });

  // Preload recent project directories for autocomplete
  void endpoints
    .sessions(200)
    .then((payload) => {
      if (disposed) return;
      const seen = new Set<string>();
      for (const s of payload.sessions || []) {
        const cwd = (s.cwd || "").trim();
        if (cwd && !seen.has(cwd)) seen.add(cwd);
      }
      projectPaths = Array.from(seen);
    })
    .catch(() => undefined);

  const stop = poll(refresh, 2500);
  return {
    el: root,
    dispose: () => {
      disposed = true;
      ++epoch;
      stop();
      if (dialog.open) dialog.close();
    },
  };
}
