# AI Prompts for Upstream Sync

This folder contains AI agent prompts for maintaining local customizations and syncing upstream changes.

Keep READMEs as project entry points, not agent memory. Durable engineering instructions belong in the [backend development guide](../../doc/development.md) ([中文](../../doc/development-zh.md)); deployment details belong in the installation guide, dated task/verification records in sync-diffs, and reusable agent constraints in prompts.

## 当前必读入口（2026-11-19）

先读 [current-customizations.md](./current-customizations.md) 和[主台账第 8 节 B08–B16](../sync-diffs/v1.21.7-local-2026-11-19.md#8-截止后增量至-57973bf2)。最新固定 `57973bf2`：原 `dc46571f` 后新增 2 次提交、259 路径；从同步落点累计 9 次提交（7 非 merge）、277 路径。原 W01 UTC/02170 已随 `a6b11dfe` 提交，仍无真实迁移/回滚/部署验收保证；不要再当成未提交待办。普通开发也必须遵守这些约束，不仅是同步上游时。

[完整增量 Markdown 明细](../sync-diffs/v1.21.7-local-2026-11-19-2220.files.md)覆盖 UTC/本地显示、原 OAuth 头像保留、绑定失败早返回、手机号与身份事务、认证配置、订阅/流量查询、显式清空规则及 README 归档。旧提交/指纹继续保留，核对实际 HEAD/工作树后再接手。

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
