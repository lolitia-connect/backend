# Sync Diffs Archive

This folder stores version-specific upstream sync analysis and dated fork-customization snapshots.

## 当前入口（2026-11-19）

- **先读**：[同步 v1.21.7 后的当前自定义改动](./v1.21.7-local-2026-11-19.md)。固定本地同步落点 `34fd88c8` → HEAD `dc46571f`，并单独记录未提交 UTC 改造。
- [完整提交/逐文件/工作树 Markdown 索引](./v1.21.7-local-2026-11-19.files.md)：逐项说明改动，保留工作树采样指纹；不包含本次文档自身。
- [当前开发 agent 约束](../prompts/current-customizations.md)与[后续同步流程](../prompts/sync-upstream.md)。
- 历史同步：[v1.20.3 → v1.21.7](./v1.20.3-to-v1.21.7.md)、[v1.3.8 → v1.20.3](./v1.3.8-to-v1.20.3.md)。历史待办/验证按当时提交理解，当前行为以新快照和实际代码为准。

## Structure

```
.ai/sync-diffs/
├── README.md                    # This file
├── v1.3.8-to-v1.20.3.md        # Example: diff analysis for v1.3.8 → v1.20.3
├── <last-tag>-to-<new-tag>.md  # Future sync records
```

## File Naming Convention

For an upstream sync, use `<last-synced-tag>-to-<new-tag>.md`.

For local work after a sync, use `<last-synced-tag>-local-<YYYY-MM-DD>.md` with a companion `.files.md`. **Store diff records and prompts only as Markdown**, not separate structured manifests. Describe concrete behavior changes, affected files, compatibility constraints and actual verification, rather than only listing names or commit subjects. Record the official commit, local sync landing, fixed HEAD, capture time, all committed changed paths (including removals/renames), and pre-existing worktree changes separately. Never treat an uncommitted migration as a release or reuse historical test results as current evidence.

Examples:
- `v1.3.8-to-v1.20.3.md`
- `v1.20.3-to-v1.21.0.md`

## What to Include

Each diff file should contain:

1. **Metadata**:
   - Sync date
   - Upstream tag range
   - Number of commits
   - Last local commit before sync

2. **Schema Changes**:
   - New entgo schema files created
   - Modified entgo schema files
   - Hand-written SQL migrations created

3. **Feature Ports**:
   - List of features ported
   - Files created/modified
   - Any breaking changes

4. **Known Issues**:
   - Features not yet ported
   - Known bugs or incomplete implementations
   - TODO items for future syncs

5. **Commands Used**:
   - Git commands for fetching/diffing
   - entgo generation commands
   - Test commands

## Purpose

- Track what has been synced
- Avoid duplicate work in future syncs
- Provide context for debugging sync-related issues
- Serve as a changelog for fork-specific features
