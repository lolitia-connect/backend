# Sync Diffs Archive

This folder stores version-specific diff analysis for each upstream sync.

## Structure

```
.ai/sync-diffs/
├── README.md                    # This file
├── v1.3.8-to-v1.20.3.md        # Example: diff analysis for v1.3.8 → v1.20.3
├── <last-tag>-to-<new-tag>.md  # Future sync records
```

## File Naming Convention

Use the format: `<last-synced-tag>-to-<new-tag>.md`

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
