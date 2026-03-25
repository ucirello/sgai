---
name: browser-bug-testing-workflow
description: Use when debugging a web UI bug or validating a multi-step browser flow. Start or reuse background servers through run-long-running-processes-in-tmux using the deterministic current-directory-plus-purpose session name, not generic tmux names or kill-before-create setup. Prevent repo-root Playwright artifact leaks by resolving the retrospective screenshots directory once and prefixing every saved filename with it.
metadata:
  requires:
    - run-long-running-processes-in-tmux
---

# Browser Bug Testing Workflow

## Overview

Turn a browser bug into a repeatable loop: start or reuse the correct background server, drive the UI with Playwright, capture visual evidence, and preserve reusable tmux sessions across iterations.

Core principle: resolve the retrospective `screenshots/` directory before the first Playwright save, then prefix every saved filename with that exact repo-relative directory. Bare filenames are evidence leaks.

## When to Use

- Debugging interactive browser bugs
- Reproducing a multi-step user workflow
- Verifying a UI fix with snapshots and screenshots
- Running a dev server, API server, or Storybook in the background while testing
- Do **not** use this skill when no browser interaction is needed
- Do **not** invent tmux naming rules here; inherit them from `run-long-running-processes-in-tmux`

## Tmux Rule Inheritance

`run-long-running-processes-in-tmux` is authoritative for tmux session naming and lifecycle.

- Derive the session name from the absolute current working directory plus `--<purpose>`.
- Preferred purpose suffixes: `web`, `api`, `storybook`, `e2e`.
- Check for the exact matching session first and reuse it when present.
- Create only when that exact session is missing.
- Do **not** use generic names like `webserver` or `testserver`.
- Do **not** kill a healthy reused session during routine browser-testing iterations.

## Evidence Output Path Preflight

Before the first Playwright save, determine the authoritative output directory and make sure it already exists.

- Read `.sgai/PROJECT_MANAGEMENT.md`.
- If its frontmatter declares `Retrospective Session: .sgai/retrospectives/<session-id>`, treat `<that path>/screenshots/` as the required browser-evidence directory for the current session.
- Create or verify that exact `screenshots/` directory before the first screenshot, snapshot, console log, or network log save.
- Resolve one repo-relative `evidence_dir` value such as `.sgai/retrospectives/2026-03-29-09-20.ngyj/screenshots` and reuse it for **every** Playwright `filename` parameter in the run.
- Prefix every saved filename with that exact directory: screenshots, snapshots, console logs, network logs, and any other browser artifact saves.
- Use that declared retrospective path exactly; do **not** improvise alternate layouts like `.sgai/retrospectives/screenshots/<session-id>/`.
- Do **not** pass bare filenames like `result.png`, `state.md`, or `console.txt`; from repo root they land in the repo root and pollute the workspace.
- The Playwright hint to prefer relative filenames means repo-relative paths under `evidence_dir`, **not** basenames with no directory.
- If no retrospective-session frontmatter exists, still preflight the intended output directory before the first Playwright save instead of assuming the path already exists.

## Workflow

### Step 1: Start or reuse the server
- [ ] Choose a stable purpose suffix.
- [ ] Derive the exact deterministic tmux session name via `run-long-running-processes-in-tmux`.
- [ ] Reuse the matching session if it exists; otherwise create it.
- [ ] Verify readiness with `tmux capture-pane` or equivalent logs.

```bash
session_name="Users-username-src-github-com-repo-slim-gray-i2e3--web"

if tmux has-session -t "$session_name" 2>/dev/null; then
  tmux capture-pane -t "$session_name" -S - -p
else
  tmux new-session -d -s "$session_name" "bun run dev"
fi
```

### Step 2: Preflight evidence outputs
- [ ] Read `.sgai/PROJECT_MANAGEMENT.md` for the current session path when it exists.
- [ ] Resolve the required browser-evidence directory.
- [ ] Create or verify that directory before the first Playwright save.
- [ ] Build exact repo-relative filenames under that directory before the first Playwright save.

### Step 3: Capture a baseline in the browser
- [ ] Navigate to the target page.
- [ ] Take an accessibility snapshot.
- [ ] Take a screenshot when visual layout or styling matters.
- [ ] Save both with filenames prefixed by the resolved browser-evidence directory.

### Step 4: Perform the interaction
- [ ] Click, type, select, or submit using exact element refs from snapshots.
- [ ] Prefer condition-based waits over arbitrary sleeps.

### Step 5: Verify the result
- [ ] Wait for the expected text or state change.
- [ ] Capture another snapshot.
- [ ] Take a screenshot of the rendered result.
- [ ] Capture console or network output when it helps explain the bug.
- [ ] Save every resulting artifact with filenames prefixed by the resolved browser-evidence directory.

### Step 6: Preserve evidence and clean up deliberately
- [ ] Capture tmux pane output when server logs matter.
- [ ] Leave a healthy reused session running for later iterations.
- [ ] Kill the tmux session only during explicit cleanup.

## Rules

1. **Tmux naming is inherited, not reinvented** - Use the authoritative naming and reuse rule from `run-long-running-processes-in-tmux`.
2. **Visual evidence is the default** - Prefer snapshots and screenshots over textual guesses about the UI.
3. **Reuse keeps feedback loops fast** - Do not restart a healthy reusable server by default.
4. **Cleanup must be deliberate** - No unconditional `tmux kill-session` at the end of every browser pass.
5. **Evidence paths must be preflighted** - If `.sgai/PROJECT_MANAGEMENT.md` declares a retrospective session, use that session's `screenshots/` directory and ensure it exists before saving artifacts.
6. **Bare filenames are forbidden** - Every Playwright `filename` must include the resolved repo-relative browser-evidence directory prefix.

## Rationalization Table

| Excuse | Reality |
|---|---|
| "`webserver` is easier to remember." | Generic names collide across repositories and bypass the deterministic reuse rule. |
| "I'll restart to be safe before every browser run." | Reuse first, inspect logs, and restart only when the session is missing or broken. |
| "This is a one-off browser test, so naming does not matter." | Stable naming is what makes the next iteration reusable instead of disposable. |
| "Playwright will create the parent directory for me." | Parent directories often do not exist in session-specific evidence paths; preflight them before the first save. |
| "The tool says to prefer relative filenames, so `shot.png` is fine." | A basename is repo-root relative here; the safe relative filename is `.sgai/retrospectives/<session-id>/screenshots/shot.png`. |
| "I already used the right directory for snapshots/logs, so one bare screenshot name is harmless." | One bare filename is enough to pollute the repo root and fail the hygiene goal. |

## Red Flags - STOP

- `tmux new-session -d -s webserver ...`
- `tmux kill-session -t webserver` as routine cleanup
- Browser setup that bypasses `run-long-running-processes-in-tmux`
- Repeated sleeps instead of condition-based waits and visual checks
- Saving Playwright artifacts into an unverified path
- Ignoring the `Retrospective Session:` frontmatter path in `.sgai/PROJECT_MANAGEMENT.md`
- `playwright_browser_take_screenshot(filename: 'result.png', ...)`
- `playwright_browser_snapshot(filename: 'state.md', ...)`
- `playwright_browser_console_messages(filename: 'console.txt', ...)`

## Examples

### Good Example

```bash
session_name="Users-username-src-github-com-repo-slim-gray-i2e3--web"
evidence_dir=".sgai/retrospectives/2026-03-29-09-20.ngyj/screenshots"

if ! tmux has-session -t "$session_name" 2>/dev/null; then
  tmux new-session -d -s "$session_name" "bun run dev"
fi

tmux capture-pane -t "$session_name" -S - -p
mkdir -p "$evidence_dir"
```

Then save Playwright artifacts as paths like:

- `.sgai/retrospectives/2026-03-29-09-20.ngyj/screenshots/baseline.md`
- `.sgai/retrospectives/2026-03-29-09-20.ngyj/screenshots/baseline.png`
- `.sgai/retrospectives/2026-03-29-09-20.ngyj/screenshots/console.txt`

### Bad Example

```bash
tmux new-session -d -s webserver "npm run dev"
sleep 3
playwright save "result.png"
playwright save ".sgai/retrospectives/screenshots/2026-03-29-09-20.ngyj/result.md"
tmux kill-session -t webserver
```

Why it is wrong: it uses a generic session name, recreates blindly, writes one artifact to the repo root and another to an improvised evidence path, and tears down the session instead of preserving a reusable browser-testing loop.

## Checklist

Before completing, verify:

- [ ] I inherited tmux naming from `run-long-running-processes-in-tmux`.
- [ ] I checked for an exact reusable tmux session before creating one.
- [ ] I did not use `webserver`, `testserver`, or other generic session names.
- [ ] I preflighted the browser-evidence output directory before the first Playwright save.
- [ ] If `.sgai/PROJECT_MANAGEMENT.md` declared a retrospective session, I used that session's `screenshots/` directory exactly.
- [ ] I prefixed every Playwright `filename` with the resolved repo-relative browser-evidence directory.
- [ ] I did not pass any bare Playwright filename basenames.
- [ ] I captured snapshots and screenshots for browser evidence.
- [ ] I only killed the tmux session during explicit cleanup.
