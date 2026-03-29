---
name: go-subprocess-lifecycle
description: Use when Go code starts external processes, detaches process groups, or needs cancellation and interrupt handling with os/exec.
---

# Go Subprocess Lifecycle

## Overview

Prevent orphaned subprocesses, lost cancellation errors, and signal-after-exit races when Go code uses `os/exec`.

## When to Use

- Writing or reviewing Go code that starts external commands with `os/exec`
- Using `CommandContext`, `Start`/`Wait`, detached process groups, or custom interrupt handling
- Mapping cancellation to caller-visible CLI behavior or exit codes
- Cleaning up child process trees on cancellation
- Don't use this skill for pure in-process concurrency without external commands

## Process

### 1. Decide who owns process lifetime
- If no custom cleanup or exit-code mapping is needed, `exec.CommandContext` may be sufficient.
- If you need process-group cleanup or must preserve `context.Canceled` at the API boundary, prefer explicit `exec.Command`, `Start`, `Wait`, and your own cancellation path.

### 2. Preserve cancellation semantics at the boundary
- The caller-visible contract matters as much as process shutdown.
- If cancellation should still match `context.Canceled`, do not rely on `CommandContext` defaults that can replace it with an `*exec.ExitError`.
- Return an error chain that keeps `context.Canceled` visible to `errors.Is` at the top-level CLI or API boundary.

### 3. Clean up detached process groups deliberately
- If you set `Setpgid: true` or otherwise detach subprocesses, add an explicit cleanup path.
- Start the command, wait in one goroutine or call path, and coordinate cancellation with process-group termination.
- Keep the cleanup helper private and directly testable.

### 4. Re-check exit state before signaling or escalating
- Cancellation and natural process exit can race.
- Before sending `SIGTERM`, check whether the process already exited.
- Before escalating to `SIGKILL`, check again whether graceful shutdown already completed.
- Never assume that a pending cancellation means the child is still running.

### 5. Write focused lifecycle regression tests
- Add one test for caller-visible cancellation behavior.
- Add one test for descendant cleanup when cancellation happens mid-run.
- Add one test for the no-signal-after-exit race.
- Prefer tests that exercise small private helpers directly when the race is otherwise hard to force from the public entrypoint.

### 6. Verify the whole path
- Run focused Go tests for the lifecycle helpers and boundary behavior first.
- Then run the repository verification required by the project, including lint/build/test commands.
- If the feature is user-facing, add one manual check that proves the final CLI or API behavior matches the intended cancellation contract.

## Common Mistakes

- Assuming `CommandContext` preserves the cancellation error you want to expose
- Detaching a process group without any matching cleanup path
- Sending signals after the process already exited
- Testing only the happy path and missing interrupt races
- Verifying process cleanup but not the error returned to the caller

## Quick Reference

- Need simple cancellation only: consider `exec.CommandContext`
- Need custom cleanup or error shape: use `exec.Command` + `Start`/`Wait`
- Need detached children: add explicit process-group teardown
- Need exit code mapping: keep `context.Canceled` visible through wrapping
- Need confidence: add focused tests for cleanup, error propagation, and exit races
