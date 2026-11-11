import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const here = path.dirname(fileURLToPath(import.meta.url));
const sourcePath = path.join(here, "../src/lib/mentions.ts");
const vectorsPath = path.join(here, "../../internal/orchestrator/testdata/mentions.json");
const source = fs.readFileSync(sourcePath, "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
}).outputText;
const module = { exports: {} };
new Function("exports", "module", "require", compiled)(module.exports, module, () => {
  throw new Error("mentions.ts must stay dependency-free for the shared vector check");
});

const { parseMentions } = module.exports;
const vectors = JSON.parse(fs.readFileSync(vectorsPath, "utf8"));
for (const vector of vectors.cases) {
  const plan = parseMentions(vector.input, vectors.dispatchable_agents);
  assert.equal(plan.message, vector.message, `${vector.name}: parent message`);
  assert.equal(plan.mentions.length, vector.mentions.length, `${vector.name}: mention count`);
  vector.mentions.forEach((expected, index) => {
    const got = plan.mentions[index];
    assert.equal(got.agent, expected.agent, `${vector.name}: agent`);
    assert.equal(got.preset, expected.preset, `${vector.name}: preset`);
    assert.equal(got.text, expected.text, `${vector.name}: text`);
    assert.equal(got.dispatchable, expected.dispatchable, `${vector.name}: dispatchable`);
  });
}
console.log(`mention vectors: ${vectors.cases.length} cases passed`);
