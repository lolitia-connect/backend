# Sync Diffs Archive

This folder stores version-specific upstream sync analysis and dated fork-customization snapshots.

## 当前入口（2026-11-19）

- **先读**：[当前主台账及第 8 节 B08–B16](./v1.21.7-local-2026-11-19.md#8-截止后增量至-57973bf2)，最新固定 `57973bf2`。新增 UTC/本地显示、头像保护、请求绑定、账号身份事务、认证配置、订阅/流量查询、规则清空与文档归档。
- [原始提交/逐文件/工作树 Markdown 索引](./v1.21.7-local-2026-11-19.files.md)保留 `34fd88c8..dc46571f` 的 19 个路径和旧 W01 指纹；[22:20 截止后增量明细](./v1.21.7-local-2026-11-19-2220.files.md)列全两次提交的 **259 路径**。累计按 Git 重算为 **9 次提交（7 非 merge）、277 路径**，不能把两表相加。
- UTC/02170 已在 `a6b11dfe` 提交；旧“未提交”是历史状态，入库不等于数据库已迁移、可安全回滚或已发布。23:06:02 +08:00 采样时编辑前工作树 clean。
- [当前开发 agent 约束](../prompts/current-customizations.md)与[后续同步流程](../prompts/sync-upstream.md)。长期规则进[开发指南](../../doc/development-zh.md)、部署进[安装指南](../../doc/install-zh.md)，不追加回项目 README。
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
