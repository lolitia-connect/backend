# AI Prompts for Upstream Sync

This folder contains AI agent prompts for syncing upstream changes.

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

1. **Before syncing**: Read `architecture-context.md` to understand the differences
2. **During sync**: Use `sync-upstream.md` for the main workflow
3. **For migrations**: Use `analyze-migration.md` when analyzing SQL migrations
4. **For features**: Use `port-feature.md` when porting specific features
5. **For translation**: Refer to `gorm-to-entgo-translation.md` for code patterns
6. **After sync**: Record results in `.ai/sync-diffs/<last-tag>-to-<new-tag>.md`
