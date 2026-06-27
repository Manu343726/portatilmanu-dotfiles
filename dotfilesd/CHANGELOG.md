# Changelog — Plugin RPC Architecture Rewrite

## [Step 0.0] — Document: Development Workflow Rules

**Commit:** `37786d6`
**Date:** 2026-06-27

### Changes
- `docs/plugin-rpc-architecture.md`: Added §18 (Development Workflow) with rules
  for one-commit-per-step, changelog updates, ask-when-unsure, safe rollback,
  and pre-flight checklist.
- `CHANGELOG.md`: Created this file.

### State
- [x] Document updated
- [ ] Build passes (N/A — doc-only change)
- [ ] Daemon starts (N/A)

### Notes
First changelog entry. Previous commits (a26978c, 0faa6a3) are the document
foundation but are not tracked here since the changelog didn't exist yet.

---

## [Step 0] — Scaffold Plugin Directories

**Commit:** `ba1dc27`
**Date:** 2026-06-27

### Changes
- `.config/dotfilesd/plugins/resources/proto/resources/.gitkeep`: Created proto
  directory for resources plugin.
- `.config/dotfilesd/plugins/tmuxbar/`: Created new plugin directory.
- `.config/dotfilesd/plugins/tmuxbar/go.mod`: Module `plugins/tmuxbar` with
  replace directives for `dotfilesd` and `plugins/resources`.
- `.config/dotfilesd/plugins/tmuxbar/proto/tmuxbar/.gitkeep`: Created proto
  directory for tmuxbar plugin.

### State
- [ ] Build passes (N/A — no code yet, directories only)
- [ ] Daemon starts (N/A)

### Notes
Weather plugin directory was already scaffolded (proto/ and go.mod existed from
earlier work). Resources plugin had go.mod but no proto/ — created it. Tmuxbar
is entirely new. Empty proto dirs use `.gitkeep` so git tracks the structure.
