import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, "../src/lib/session-start.ts"), "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
}).outputText;
const module = { exports: {} };
new Function("exports", "module", "require", compiled)(module.exports, module, () => {
  throw new Error("session-start must not depend on runtime modules");
});
const { startedSessionSummary } = module.exports;
const started = Object.freeze({ sessionID: "demo-session-1", source: "codex", cwd: "/tmp/demo-project", text: "Demo task", permissionMode: "read_only" });
const fresh = startedSessionSummary([], started);
assert.equal(fresh?.session_id, "demo-session-1", "an index miss must still open the confirmed session");
assert.equal(fresh?.source, "codex");
assert.equal(fresh?.cwd, "/tmp/demo-project");
assert.equal(fresh?.title, "Demo task");
assert.equal(fresh?.permission_mode, "read_only");
assert.equal(fresh?.running, true);
const other = Object.freeze({ session_id: "demo-session-1", source: "claude", title: "Other Agent" });
assert.equal(startedSessionSummary([other], started)?.source, "codex", "another Agent's same ID must never be selected");
const indexed = Object.freeze({ session_id: "demo-session-1", source: "codex", title: "Indexed result", status: "done", running: false });
assert.equal(startedSessionSummary([indexed], started), indexed, "preserve real indexed state rather than replacing it with launch state");
console.log("session creation: cache miss, Agent identity, and indexed state passed");
