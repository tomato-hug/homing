# Agent directory snapshot sources

The machine-readable catalog in `magpie-agents.json` is a small metadata
snapshot, not a Magpie runtime dependency. Homing never imports Magpie code or
calls Magpie/npm while serving the console.

- Magpie source and supported-agent table:
  `https://github.com/yetone/magpie`, revision
  `03a800ac698445234c2a996bf0298fd6677d3ba0` (MIT), snapshot 2026-11-10.
- Magpie community Agent registry:
  `https://github.com/magpie-community/plugins`, registry
  `https://raw.githubusercontent.com/magpie-community/plugins/main/registry.json`
  (MIT), snapshot 2026-11-10.
- Third-party descriptors referenced by the registry (metadata only):
  `xuzhougeng/magpie-agents`, `YuchenWell/magpie-plugin-pink`,
  `shlroland/magpie-agent-kaku`.

The snapshot records names, vendor pages, detection commands, default config
paths, install commands and the fact that Magpie's plugin exists. It does not
claim that Magpie model-routing plugins can read sessions or notify Homing.
Those capabilities are Homing-side integrations and are graded conservatively
in `catalog.go`.
