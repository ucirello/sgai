# Repository Mode Behavior And Validation

## Purpose

This document records the product decisions, expected behavior, and validation criteria for standalone, root, and forked repository behavior.

## Product Decisions

### Attachment policy

- The external attachment flow remains available for any unique absolute external directory.
- Users do not need to satisfy a pre-existing root or fork classification before attaching a directory.

### Mode switching policy

- A repository behaves as standalone unless at least one child `jj workspace` fork is attached.
- A repository with child workspaces on disk but no attached child forks still behaves as standalone in the product.
- Attaching a child fork promotes the attached root into forked mode.
- Detaching the last attached child fork demotes the repository back to standalone repository mode.
- Fork creation UI remains scoped by the current product mode decisions already captured in `GOAL.md`.

## Expected Behavior

### Standalone repository

- Shows repository mode behavior.
- Does not appear as a root-only dashboard merely because extra workspaces exist on disk.
- Can use the standalone fork creation entry points defined elsewhere in the product requirements.

### Root repository with attached forks

- Shows forked mode behavior.
- Lists and serves attached child forks.
- Keeps root-specific fork dashboard behavior only while at least one child fork remains attached.

### Forked repository

- Behaves as an attached child workspace, not as the root workspace.
- Does not expose standalone fork-creation affordances that are reserved for standalone repositories.

## Validation Criteria

The work is correct when all of the following are true:

- Attaching a unique absolute external directory still works.
- A repository with no attached child forks is presented as standalone even if child workspaces exist on disk.
- Attaching a child fork promotes the repository into root or forked mode.
- Detaching the last attached fork demotes the repository back to standalone repository mode.
- UI and API behavior agree on the same attached-fork rule.

## Required Evidence For Future Changes

- Targeted verification that covers unattached child workspaces, attached child forks, and detach-last-fork transitions.
- Verification reports should state the commands or scenarios exercised and the observed result for each mode transition.
- If UI behavior is affected, include browser verification evidence for the served mode shown to the user.
