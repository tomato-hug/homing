import type { PageContext, PageResult } from "../main";
import { h, clear } from "../lib/dom";
import { endpoints, poll, type Room, type RoomAgentChoice, type RoomParticipant } from "../lib/api";
import { button, card, emptyState, pill } from "../lib/ui";

const statusText = (r: Room) => ({ running: r.round ? `进行中 · 第 ${r.round} / ${r.rounds} 轮` : "准备开始", paused: "已暂停", summarizing: "正在整理纪要", ended: "已结束" })[r.status] || r.status;
const participantText = (p: RoomParticipant) => ({ speaking: "发言中", needs_you: "需要你处理", failed: "发言失败", waiting: "等待发言" })[p.status] || p.status;

function callableParticipants(r: Room): RoomParticipant[] {
  if (!["running", "paused"].includes(r.status) || r.end_requested) return [];
  const spoken = new Set((r.messages || []).filter(m => m.kind === "statement" || m.kind === "system").map(m => `${m.from}#${m.round}`));
  if (r.speaking && r.round) spoken.add(`${r.speaking}#${r.round}`);
  for (let n = 1; n <= r.rounds; n++) {
    if (r.participants.some(p => p.status !== "failed" && !spoken.has(`${p.agent}#${n}`))) {
      return r.participants.filter(p => p.agent !== r.speaking && p.status !== "failed" && !spoken.has(`${p.agent}#${n}`));
    }
  }
  return [];
}

export function renderRooms(ctx: PageContext, query: URLSearchParams): PageResult {
  let disposed = false, busy = false, epoch = 0;
  let selected = query.get("id") || "";
  let room: Room | null = null;
  let agents: RoomAgentChoice[] = [];
  let loadError = "", actionError = "", listSignature = "", transcriptSignature = "", callSignature = "";
  const error = h("div", { class: "rooms-error", role: "alert", "aria-live": "polite", hidden: true });
  const list = h("div", { class: "rooms-list", "aria-label": "会议列表" }, emptyState("正在读取会议…"));
  const title = h("h2", { text: "选择一场会议" });
  const status = h("div", { class: "room-status", "aria-live": "polite" });
  const topic = h("p", { class: "room-topic" });
  const participants = h("div", { class: "room-participants" });
  const transcript = h("div", { class: "room-transcript", "aria-label": "会议记录" });
  const minutes = h("div", { class: "room-minutes", text: "结束会议后，这里会显示纪要。" });
  const metadata = h("div", { class: "note room-metadata" });
  const pending = h("div", { class: "note", "aria-live": "polite" });
  const callSelect = h("select", { "aria-label": "下一位发言者" });
  const callButton = button("点名", { onClick: () => { if (room && callSelect.value) void act(() => endpoints.controlRoom(room!.id, "call", callSelect.value)); } });
  const pause = button("暂停", { onClick: () => control("pause") });
  const resume = button("继续", { onClick: () => control("resume") });
  const extend = button("加一轮", { onClick: () => control("extend") });
  const end = button("结束并出纪要", { variant: "danger", onClick: () => control("end") });
  const input = h("textarea", { rows: 3, maxLength: 4000, placeholder: "插话：下一位发言时会看到", "aria-label": "会议发言" });
  const send = h("button", { class: "btn btn-primary", type: "submit", text: "发言" });
  input.addEventListener("input", () => updateControls());
  const form = h("form", { class: "room-message-form", onSubmit: e => {
    e.preventDefault(); const text = input.value.trim();
    if (room && text) void act(() => endpoints.sayInRoom(room!.id, text)).then(ok => { if (ok && input.value.trim() === text) input.value = ""; updateControls(); });
  } }, input, send);
  const controls = h("div", { class: "room-controls" }, pending,
    h("div", { class: "room-actions" }, callSelect, callButton, pause, resume, extend, end), form);
  const newButton = button("开会", { variant: "primary", onClick: () => showCreate() });
  const dialog = h("dialog", { class: "room-create-dialog", "aria-label": "开会" });
  const root = h("div", { class: "rooms-page" },
    h("div", { class: "rooms-head" }, h("div", null, h("h1", { text: "会议室" }), h("p", { class: "note", text: "轮流讨论与纪要。参会 Agent 只读工作目录，写代码请另行派发。" })), newButton), error,
    h("div", { class: "rooms-grid" }, card("会议", list),
      h("section", { class: "card room-discussion", "aria-label": "会议讨论" }, h("div", { class: "room-heading" }, title, status), topic, participants, transcript, controls),
      card("会议纪要", metadata, minutes)), dialog);

  function showError() { error.textContent = actionError || loadError; error.hidden = !error.textContent; }
  function control(action: string) { if (room) void act(() => endpoints.controlRoom(room!.id, action)); }
  function updateControls() {
    const active = !!room && ["running", "paused"].includes(room.status) && !room.end_requested;
    controls.hidden = !room;
    pause.hidden = !active || room!.status !== "running";
    resume.hidden = !room || room.status !== "paused";
    resume.textContent = room?.end_requested ? "继续整理纪要" : "继续";
    extend.hidden = end.hidden = callButton.hidden = callSelect.hidden = !active;
    form.hidden = !active;
    for (const b of [pause, resume, end]) b.disabled = busy;
    extend.disabled = busy || (room?.rounds || 0) >= 5;
    const choices = room ? callableParticipants(room) : [];
    const signature = JSON.stringify(choices.map(p => [p.agent, p.name]));
    if (signature !== callSignature) {
      const previous = callSelect.value; clear(callSelect);
      callSelect.append(h("option", { value: "", text: choices.length ? "选择下一位" : "本轮暂无可点名者" }), ...choices.map(p => h("option", { value: p.agent, text: p.name })));
      if (choices.some(p => p.agent === previous)) callSelect.value = previous;
      callSignature = signature;
    }
    callSelect.disabled = busy || !choices.length;
    callButton.disabled = busy || !callSelect.value || !choices.some(p => p.agent === callSelect.value);
    input.disabled = !active;
    send.disabled = busy || !active || !input.value.trim();
    newButton.disabled = busy || agents.length < 2;
    pending.textContent = room?.status === "ended" ? "会议已结束，可查看会议纪要" : room?.end_requested ? "当前发言结束后整理纪要" : room?.status === "summarizing" ? "正在整理纪要，暂不接受插话" : room?.next_speaker ? `下一位：${room.participants.find(p => p.agent === room!.next_speaker)?.name || room.next_speaker}${room.status === "paused" ? "；点继续后执行" : ""}` : "点名只调整既定顺序，不增加轮次；暂停不会打断当前发言。";
  }
  callSelect.addEventListener("change", updateControls);

  function paintRoom() {
    if (!room) { transcript.replaceChildren(emptyState("选择左侧会议，或开一场新的讨论。")); updateControls(); return; }
    title.textContent = room.title; topic.textContent = room.topic;
    clear(status); status.append(pill(statusText(room), room.status === "running" ? "ok" : room.status === "paused" ? "warn" : "mute"));
    if (room.note) status.append(h("p", { class: "note", text: room.note }));
    clear(participants);
    for (const p of room.participants) {
      const item = h("div", { class: `room-participant ${p.status}` }, h("strong", { text: p.role ? `${p.name}（${p.role}）` : p.name }), h("span", { class: "note", text: participantText(p) }));
      if (p.session_id) item.append(button(p.status === "needs_you" ? "打开会话处理" : "查看会话", { small: true, onClick: () => ctx.navigate(`/console/sessions?session=${encodeURIComponent(p.session_id!)}`) }));
      if (p.status === "failed") item.append(h("span", { class: "note", text: "请查看下方失败记录；本场会跳过这位参会者。" }));
      participants.append(item);
    }
    const signature = JSON.stringify(room.messages || []);
    if (signature !== transcriptSignature) {
      transcriptSignature = signature; clear(transcript);
      for (const m of room.messages || []) {
        if (m.kind === "minutes") continue;
        transcript.append(h("article", { class: `room-message ${m.kind}` }, h("div", { class: "room-message-head" }, h("strong", { text: m.name || m.from }), h("span", { class: "note", text: `第 ${m.round} 轮` })), h("div", { class: "room-message-text", text: m.text })));
      }
      if (!transcript.childElementCount) transcript.append(emptyState("还没有发言", room.speaking ? "正在等待当前参会者完成。" : "会议记录会显示在这里。"));
    }
    minutes.textContent = room.minutes || (room.status === "summarizing" ? "正在整理纪要…" : "结束会议后，这里会显示纪要。");
    metadata.textContent = `只读讨论 · ${room.rounds} 轮\n${room.cwd}`;
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
        listSignature = signature; clear(list);
        rooms.sort((a, b) => Number(a.status === "ended") - Number(b.status === "ended"));
        for (const r of rooms) {
          const item = h("button", { type: "button", class: `room-list-item${r.id === selected ? " selected" : ""}`, onClick: () => ctx.navigate(`/console/rooms?id=${encodeURIComponent(r.id)}`) }, h("strong", { text: r.title }), h("span", { class: "note", text: statusText(r) }), h("span", { class: "note", text: r.preview || r.participants.map(p => p.name).join("、") }));
          list.append(item);
        }
        if (!rooms.length) list.append(emptyState("还没有会议", agents.length >= 2 ? "选一个议题，邀请至少两位 Agent。" : "至少需要两位可后台启动的 Agent；请先在 Agent 页接入。"));
      }
      if (selected) {
        const result = await endpoints.room(selected);
        if (disposed || observed !== epoch) return;
        room = result.room;
      }
      loadError = ""; showError(); paintRoom();
    } catch (e) { if (!disposed && observed === epoch) { loadError = String((e as Error).message || e); showError(); updateControls(); } }
  }
  async function act(operation: () => Promise<{ room: Room }>): Promise<boolean> {
    if (busy || disposed) return false;
    busy = true; ++epoch; actionError = ""; showError(); updateControls();
    try {
      const result = await operation();
      if (disposed) return false;
      room = result.room; paintRoom(); return true;
    } catch (e) { if (!disposed) { actionError = String((e as Error).message || e); showError(); } return false; }
    finally { busy = false; if (!disposed) { updateControls(); void refresh(); } }
  }

  function showCreate() {
    if (busy || agents.length < 2) return;
    clear(dialog);
    const topicInput = h("textarea", { rows: 3, maxLength: 4000, required: true, id: "room-topic" });
    const cwd = h("input", { type: "text", required: true, placeholder: "/path/to/project", id: "room-cwd" });
    const rounds = h("select", { id: "room-rounds" }, ...[1, 2, 3, 4, 5].map(n => h("option", { value: String(n), text: `${n} 轮` }))); rounds.value = "2";
    const seats = agents.map((a, i) => ({ agent: a.id, checkbox: h("input", { type: "checkbox", checked: i < 2 }), role: h("input", { type: "text", maxLength: 40, placeholder: "角色（可选）", "aria-label": `${a.name} 的角色` }), name: a.name }));
    const problem = h("p", { class: "rooms-error", role: "alert" });
    const cancel = button("取消", { onClick: () => dialog.close() });
    const submit = h("button", { class: "btn btn-primary", type: "submit", text: "开始会议" });
    const create = h("form", { class: "room-create-form", onSubmit: async e => {
      e.preventDefault(); if (busy) return;
      const chosen = seats.filter(s => s.checkbox.checked);
      if (chosen.length < 2 || chosen.length > 4) { problem.textContent = "请选择 2–4 位参会者"; return; }
      busy = true; ++epoch; updateControls(); submit.disabled = cancel.disabled = true; problem.textContent = "";
      try {
        const result = await endpoints.createRoom({ topic: topicInput.value.trim(), cwd: cwd.value.trim(), rounds: Number(rounds.value), participants: chosen.map(s => ({ agent: s.agent, role: s.role.value.trim() })) });
        if (!disposed) ctx.navigate(`/console/rooms?id=${encodeURIComponent(result.room.id)}`);
      } catch (e) { if (!disposed) problem.textContent = String((e as Error).message || e); }
      finally { busy = false; submit.disabled = cancel.disabled = false; if (!disposed) updateControls(); }
    } }, h("h2", { text: "开会" }), h("label", { for: "room-topic", text: "议题" }), topicInput,
      h("label", { for: "room-cwd", text: "工作目录（只读）" }), cwd, h("label", { for: "room-rounds", text: "轮数" }), rounds,
      h("fieldset", null, h("legend", { text: "参会者（2–4 位）" }), ...seats.map(s => h("div", { class: "room-create-seat" }, h("label", null, s.checkbox, s.name), s.role))), problem, h("div", { class: "room-actions" }, cancel, submit));
    dialog.append(create); dialog.showModal();
  }
  dialog.addEventListener("cancel", e => { if (busy) e.preventDefault(); });
  const stop = poll(refresh, 2500);
  return { el: root, dispose: () => { disposed = true; ++epoch; stop(); dialog.close(); } };
}
