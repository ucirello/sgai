---
name: project-completion-verification
description: Automatically scans GOAL.md for unchecked items, provides completion status summary, and enables coordinator to mark items as complete. When coordinator needs to verify project completion status or before marking work as complete. When coordinator needs to mark completed items in GOAL.md. Symptoms - manually going through GOAL.md line by line to check task completion, needing quick summary of pending vs completed tasks by category, and verifying all requirements are met before finalizing work.
---

# Project Completion Verification Automation

## Overview

Automates the verification of project completion status by scanning GOAL.md for checked and unchecked items, providing categorized summaries and completion counts.

**Core principle:** Systematic verification over manual checking, evidence-based completion reporting.

**Completion gate rule:** If GOAL frontmatter defines `completionGateScript`, the coordinator must run it successfully before marking the final open items complete or re-marking reopened items complete.

## When to Use

- Before marking work as complete or creating PRs
- When coordinator needs quick project status overview
- When manually checking GOAL.md line by line
- When needing counts of completed vs pending tasks by category
- Before final deliverables or milestone completion
- Before re-marking reopened GOAL items complete after follow-up fixes
- Symptoms: spending significant time manually verifying task completion, uncertainty about project status

## Core Pattern

Use bash commands to parse GOAL.md and generate a completion report:

```bash
# Check for an explicit completion gate in GOAL frontmatter
rg '^completionGateScript:' GOAL.md

# Get overall completion status
rg "\[ \]" GOAL.md -c && rg "\[x\]" GOAL.md -c

# Get completion by section
awk '/^#/{section=$0} /\[ \]/{pending[section]++} /\[x\]/{completed[section]++} END{for(s in pending) print s ": " completed[s] "/" (completed[s]+pending[s]) " completed"}' GOAL.md

# List pending items by section
awk '/^#/{section=$0} /\[ \]/{print section ": " $0}' GOAL.md
```

## Quick Reference

| Task | Command | Purpose |
|------|---------|---------|
| Read completion gate | `rg '^completionGateScript:' GOAL.md | head -n 1` | Discover required final verification command |
| Count pending items | `rg "\[ \]" GOAL.md -c` | Total unchecked tasks |
| Count completed items | `rg "\[x\]" GOAL.md -c` | Total checked tasks |
| Section breakdown | See pattern above | Completion by category |
| List pending items | See pattern above | Specific pending tasks |

## Completion Gate Contract

Before the coordinator marks the last open GOAL items complete, or re-marks previously reopened GOAL items complete, follow this contract:

1. Read GOAL frontmatter and check whether `completionGateScript` is defined.
2. If `completionGateScript` exists, run it from the repository root after the targeted verification relevant to the touched work is already green.
3. Require a successful exit status from `completionGateScript` before editing GOAL.md.
4. Record the command and the key result in `.sgai/PROJECT_MANAGEMENT.md` when completion is claimed.
5. If `completionGateScript` fails, do not mark the items complete. Route the failure back through the normal agent workflow first.

If GOAL frontmatter does not define `completionGateScript`, proceed with the normal completion audit and evidence log.

## Implementation Details

The skill uses ripgrep (`rg`) and `awk` to:

1. Count unchecked items (`[ ]`) and checked items (`[x]`)
2. Group by markdown sections (categories)
3. Provide completion percentages
4. List specific pending items for action
5. Detect whether a final completion gate command must run before GOAL updates

## Marking Items as Complete (Coordinator Only)

**Note:** This capability is for the coordinator agent ONLY. Non-coordinator agents must NOT use this section. Instead, non-coordinator agents should send a message to the coordinator with "GOAL COMPLETE:" prefix when they finish a task.

### Workflow

1. **Run the status check commands above** to see current state of GOAL.md (pending vs completed counts, specific pending items).
2. **Inspect GOAL frontmatter** for `completionGateScript`.
3. **If marking the final open items or re-marking reopened items complete, run `completionGateScript` successfully before editing GOAL.md.**
4. **For each item to mark**, use the Edit tool to change `- [ ]` to `- [x]` for the specific line in GOAL.md.
5. **Re-run status check** to verify the mark was applied correctly.
6. **Log the change** in `.sgai/PROJECT_MANAGEMENT.md` with timestamp, including the completion-gate result when applicable.

### Example

```bash
# Step 1: Check current status
rg "\[ \]" GOAL.md -c && rg "\[x\]" GOAL.md -c

# Step 2: Inspect completion gate
rg '^completionGateScript:' GOAL.md

# Step 3: If this is the final completion mark, run the declared gate
make test

# Step 4: Mark specific item using Edit tool
# oldString: "- [ ] Implement authentication endpoint"
# newString: "- [x] Implement authentication endpoint"

# Step 5: Re-run status check to verify
rg "\[ \]" GOAL.md -c && rg "\[x\]" GOAL.md -c

# Step 6: Log in PROJECT_MANAGEMENT.md
# "2026-02-01: Marked 'Implement authentication endpoint' as complete after make test passed"
```

## Common Mistakes

- Not checking both unchecked `[ ]` and checked `[x]` patterns
- Missing section-level analysis for categorized reporting
- Forgetting to handle edge cases (empty sections, mixed formatting)
- Not providing actionable output (just counts without context)
- Non-coordinator agents attempting to use the marking capability
- Marking items without verifying the work is actually complete
- Forgetting to log the marking in PROJECT_MANAGEMENT.md
- Skipping `completionGateScript` when GOAL frontmatter declares one
- Re-marking reopened items complete without re-running the completion gate

## Expected Output Format

```
Project Completion Status:
Overall: 45/50 tasks completed (90%)

By Category:
- sgai-server: 23/25 completed (92%)
- bug fixes: 8/8 completed (100%)
- code quality issues: 14/17 completed (82%)

Pending Items:
- code quality issues: - [ ] endpoint error handling
- code quality issues: - [ ] test coverage improvements

Completion Gate:
- completionGateScript: make test
- Result: passed
```

## Real-World Impact

Saves significant manual verification time by:
- Eliminating line-by-line GOAL.md scanning
- Providing immediate completion percentages
- Highlighting specific pending work by category
- Enforcing the repository-declared final completion gate before GOAL edits
- Reducing human error in completion verification
