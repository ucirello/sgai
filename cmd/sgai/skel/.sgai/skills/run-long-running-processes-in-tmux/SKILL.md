---
name: run-long-running-processes-in-tmux
description: Use when you need a detached tmux process. Derive one deterministic session name from the absolute current working directory plus a purpose suffix, reuse the matching owned session before creating it, and only kill that session during explicit cleanup or when you verified it is broken.
---

# Run Long Running Processes in Tmux

## Overview

Tmux sessions are workspace-scoped infrastructure. Use one deterministic name per working-directory-and-purpose pair so you can find the same session again, reuse it, and avoid killing healthy long-running processes just because they already exist.

## When to Use

- Running a server, watcher, or other long-lived background process
- Needing a detached process with a TTY
- Reusing the same process across multiple iterations in one repository
- Capturing output from a detached process
- Sending commands into a running shell or process
- Do **not** use generic names like `server`, `webserver`, `testserver`, or `appsession`
- Do **not** default to kill-before-create

## Authoritative Naming Rule

1. Start with the **absolute current working directory**.
2. Replace each run of non-alphanumeric characters with `-`.
3. Trim leading and trailing `-`.
4. Normalize `<purpose>` to lowercase letters, numbers, and dashes only; replace runs of non-alphanumeric characters with `-`, then trim edge dashes.
5. Append `--<purpose>`.
6. Keep `<purpose>` short and stable: `web`, `api`, `worker`, `tests`, `docs`.

Example:

- Working directory: `/Users/ucirello/go/src/github.com/ucirello/slim-gray-i2e3`
- Purpose: `web`
- Session name: `Users-username-src-github-com-repo-slim-gray-i2e3--web`

The exact deterministic name is the ownership boundary for this workflow. If that exact session already exists, treat it as the session you own for that workspace and purpose.

## Process

### Step 1: Derive the exact session name
- [ ] Pick one stable purpose suffix and normalize it before building the final session name.
- [ ] Derive the deterministic session name from the absolute current working directory plus `--<purpose>`.
- [ ] Reuse that exact name consistently for the whole task.

### Step 2: Reuse before create
- [ ] Check whether the exact session already exists with `tmux has-session -t "$session_name"`.
- [ ] If it exists, reuse it and inspect it with `tmux capture-pane` or `tmux attach`.
- [ ] If it is missing, create it with `tmux new-session -d -s "$session_name" "command"`.

### Step 3: Verify and interact
- [ ] Verify startup or health with `tmux capture-pane`.
- [ ] If inspection proves the exact session is broken or a fresh `tmux new-session` failed to start correctly, treat that proof as explicit recovery authorization: kill that exact session and recreate it once with the same deterministic name.
- [ ] If the recreated session still fails inspection, stop and report the failure instead of silently looping.
- [ ] Send commands with `tmux send-keys` when needed.
- [ ] Capture pane output when logs matter.

### Step 4: Clean up only when cleanup is explicit
- [ ] Kill the session only during an explicit cleanup phase.
- [ ] A healthy reused session stays alive.
- [ ] Recreate only when you verified the existing session is broken and a fresh process is required.

## Rules

1. **Deterministic names only** - Session names come from the current working directory plus a normalized purpose suffix.
2. **Reuse before create** - Check for the exact matching session first and reuse it when present.
3. **No kill-before-create default** - Unconditional `tmux kill-session ... || true` is no longer the standard pattern.
4. **One purpose per session** - Use separate suffixes for separate long-running roles.

## Quick Reference

| Goal | Pattern |
|---|---|
| Derive session name | `workspace-slug--purpose` |
| Check for reusable session | `tmux has-session -t "$session_name"` |
| Create when missing | `tmux new-session -d -s "$session_name" "command"` |
| Inspect output | `tmux capture-pane -t "$session_name" -S - -p` |
| Send command | `tmux send-keys -t "$session_name" "command" C-m` |
| Explicit cleanup | `tmux kill-session -t "$session_name"` |

## Rationalization Table

| Excuse | Reality |
|---|---|
| "Killing and recreating is cleaner." | The default is reuse-before-create. Restarting a healthy session throws away useful state and slows the next iteration. |
| "`webserver` is easier to remember." | Generic names collide across repositories and break deterministic reuse. |
| "Fresh is safer." | Safety means inspect first, then restart only if the session is missing or proven broken. |
| "The existing session has the wrong name, but it's probably mine." | Ownership is the exact deterministic name. If you want reuse, use the authoritative name. |

## Red Flags - STOP

- `tmux kill-session -t "$session_name" 2>/dev/null || true` before checking reuse
- Session names like `server`, `webserver`, `testserver`, or `appsession`
- Restarting a healthy session just because it already exists
- Creating a second session for the same workspace and purpose instead of reusing the first

## Examples

### Good Example

```bash
session_name="Users-username-src-github-com-repo-slim-gray-i2e3--web"

if tmux has-session -t "$session_name" 2>/dev/null; then
  tmux capture-pane -t "$session_name" -S - -p
else
  tmux new-session -d -s "$session_name" "bun run dev"
fi
```

### Bad Example

```bash
tmux kill-session -t webserver 2>/dev/null || true
tmux new-session -d -s webserver "bun run dev"
```

Why it is wrong: it uses a generic name and kills before checking whether a healthy reusable session already exists.

## Checklist

Before completing, verify:

- [ ] I used the deterministic current-directory-plus-purpose session name.
- [ ] I normalized the purpose suffix before deriving the final session name.
- [ ] I checked for an exact matching session before creating one.
- [ ] I reused a healthy matching session instead of killing it.
- [ ] I avoided generic session names.
- [ ] I only killed the session during explicit cleanup or after proving it was broken.
