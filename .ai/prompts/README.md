# AI Prompts for Upstream Sync

This folder contains AI agent prompts for maintaining local customizations and syncing upstream changes.

Keep READMEs as project entry points, not agent memory. Durable engineering instructions belong in the [backend development guide](../../doc/development.md) ([中文](../../doc/development-zh.md)); deployment details belong in the installation guide, dated task/verification records in sync-diffs, and reusable agent constraints in prompts.

## 当前必读入口（2026-11-19）

先读 [current-customizations.md](./current-customizations.md) 和 [v1.21.7 后的定制台账](../sync-diffs/v1.21.7-local-2026-11-19.md)。已提交快照为 `dc46571f`；UTC/02170 等工作树改动单列，不能当成已发布行为。普通开发也必须遵守这些约束，不仅是同步上游时。

- 日常开发/修复：[current-customizations.md](./current-customizations.md)。
- 再次同步上游：[sync-upstream.md](./sync-upstream.md)，使用明确目标 tag/SHA，不沿用旧 v1.3.8 示例范围。
- 迁移/功能模板：[analyze-migration.md](./analyze-migration.md)、[port-feature.md](./port-feature.md)。模板里的历史功能不是新待办；以当前源码和台账为准。
- 对应前端：[frontend current-customizations](../../../frontend/.ai/prompts/current-customizations.md)。

## Usage

These prompts are designed to be used by AI coding assistants (like GitHub Copilot) to automate the upstream sync process.

## Prompt Files

### Core Prompts

- `sync-upstream.md` — Main prompt for analyzing and porting upstream changes (complete workflow)
- `analyze-migration.md` — Prompt for analyzing upstream SQL migrations and translating to entgo
- `port-feature.md` — Template prompt for porting a specific feature from upstream

### Reference Documents

- `architecture-context.md` — Architecture overview and key differences between local and upstream
- `gorm-to-entgo-translation.md` — Translation patterns and examples for converting GORM code to entgo

## How to Use

1. Copy the prompt content
2. Paste into your AI assistant's chat
3. Replace placeholders (e.g., `<TAG>`, `<FEATURE>`) with actual values
4. Let the AI execute the sync workflow

## Example

```
# Sync upstream v1.20.3
Use the prompt in sync-upstream.md with TAG=v1.20.3
```

## Workflow

1. **Before any work**: Read [current-customizations.md](./current-customizations.md), inspect HEAD/worktree, then read [architecture-context.md](./architecture-context.md)
2. **During sync**: Use `sync-upstream.md` for the main workflow
3. **For migrations**: Use `analyze-migration.md` when analyzing SQL migrations
4. **For features**: Use `port-feature.md` when porting specific features
5. **For translation**: Refer to `gorm-to-entgo-translation.md` for code patterns
6. **After sync**: Record results in `.ai/sync-diffs/<last-tag>-to-<new-tag>.md`
