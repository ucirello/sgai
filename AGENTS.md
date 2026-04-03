THE ONLY ACCEPTABLE PLACE FOR PROJECT_MANAGEMENT.md IS `.sgai/PROJECT_MANAGEMENT.md` -- never place `cmd/sgai/skel/.sgai/PROJECT_MANAGEMENT.md`.

THE ONLY ACCEPTABLE PLACE FOR PR_INFO IS `.sgai/PR_INFO` -- never place `cmd/sgai/skel/.sgai/PR_INFO` and never place `sgai/PR_INFO`.

Every time you are asked to make a source code (or prompt) modification  to `/.sgai` you have to make the modification to `sgai/` (the overlay directory) instead.

When work involves external attachment rules or standalone/root/forked repository behavior, consult the decision records in `PRDs/` before changing code, prompts, tests, or reviews.

In term of Go code style, I prefer total absence of inline comments; organize functions and if blocks in a way that they have intention revealing names, and use that instead.

In term of Go code style, I prefer very private functions over public functions; private struct over public structs; local types and structs over global structs; public functions and structs must have godoc comments.

In terms of Go code style, error variable names must use the err prefix pattern: errSpecificName (e.g., errClose, errRead), not the suffix pattern (closeErr, readErr).

Always run the code reviewer when one is available.

Always make sure you use tmux and playwright to test the changes.

For tests:
use listen address `-listen-addr 127.0.0.1:0` (observe the port number and use that from this moment on)
use directory ./verification
use `make build` to generate the binary

In terms of layout, UI, style, when something doesn't fit a container, use ellipsis with tooltip - refer to https://picocss.com/docs/tooltip

CRITICAL: use playwright screenshots (and the skill to operate playwright) to verify the application is working correctly.

CRITICAL: every Playwright artifact save (`playwright_browser_take_screenshot`, `playwright_browser_snapshot`, console logs, network logs, or similar) must use a repo-relative filename under the current retrospective session's `.sgai/retrospectives/<session-id>/screenshots/` directory resolved from `.sgai/PROJECT_MANAGEMENT.md` frontmatter.

CRITICAL: bare Playwright filenames like `foo.png`, `foo.md`, or `foo.txt` are forbidden because they save into the repository root and pollute the workspace.

For React/TypeScript code in cmd/sgai/webapp/, use bun for building, testing, and running scripts. Run these commands from `cmd/sgai/webapp/`. Build command: `bun run build`. Dev server: `bun run dev.ts`. Tests: `bun test src/`.

Bare repo-root `bun test` is misleading in this repository because it can sweep unrelated root-level TypeScript/Playwright files instead of just the webapp package.

React components must use shadcn/ui components where possible. Do not create custom implementations when a shadcn component exists. Reference: https://ui.shadcn.com/docs

React tests: bun test for unit/component tests (vitest-compatible API), Playwright for E2E tests.

Use useSyncExternalStore for external data sources (SSE store). Use useReducer+Context for app state management. Do NOT use Redux, Zustand, or other state management libraries. No optimistic updates for critical workflow actions.

When modifying cmd/sgai/webapp/, always run `bun run build && make build` to verify both the React build and Go binary compile correctly.

CRITICAL(code quality): ensure good Go code quality by calling `make lint`
CRITICAL(code quality): never use `log.Fatal*` or similar -- because they crash the whole server (and the trick of using `fmt.Fprint*(os.Stderr); os.Exit(1)` is also not allowed, the trick of using `log.Print*(os.Stderr); os.Exit(1)` is also not allowed)

You must use the skill `browser-bug-testing-workflow` - remember to use visual diffs and screenshots to evaluate the problem
You must use the skill `run-long-running-processes-in-tmux`


# Terminology

- Standalone Repository: a repository that has only _one_ `jj workspace` -- itself.
- Root Repository: a repository that has more than one `jj workspace`, and it is the root (it is the one in which `.jj/repo` is a directory and not a file)
- Forked Repository: a repository that is part of a `jj workspace, and it is not the root (it is the one in which `.jj/repo` is a text file, whose content points to the parent).

- Repository Mode: is when a repository is served by SGAI in a way that it can actually run software.
- Forked Mode: is when a root repository has at least one child, it displays the fork (dashboard-style) mode.
**CRITICAL** when a Root Repository run out of children, it must revert back from Forked Mode to Repository Mode.


# Safe Assumptions

"OpenCode" (aka `opencode`) is always installed and available.

When implementing new features that handle external input, interact with the filesystem, or manage concurrent operations, the coordinator should consider invoking the stpa-analyst to identify unsafe control actions and loss scenarios before implementation begins.

When making changes, you can safely assume that backward compatibility is not necessary unless explicitly stated in GOAL.md - therefore, changes that simplify the code can be made by mostly deleting lines.

# Code Auditing Guidance

When auditing for dead routes, check both literal usage (API endpoint calls from frontend) AND semantic liveness (does the route lead to functionality that has been replaced by inline components or other mechanisms). A route that is technically reachable but leads to replaced functionality is dead.
