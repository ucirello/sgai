# External Attachment And Classification Structure

## Purpose

This document records the structural decisions behind external repository attachment and repository classification so future implementation work does not reintroduce the same confusion.

## Structural Decisions

### External attachment

- SGAI accepts any unique absolute external directory for attachment.
- Attachment does not require the target directory to already be classified as standalone, root, or forked before the attach action.
- The only hard guard at attachment time is that the same external directory must not be attached more than once.
- If the attached directory needs SGAI metadata, initialization may happen as part of the attachment flow.
- Attached Repositories are stored in the configuration file and must be remain attached upon server restarts

### Repository classification terminology

- A `Standalone Repository` has one `jj workspace`: itself.
- A `Root Repository` has more than one `jj workspace` and is the root workspace.
- A `Forked Repository` is a child workspace within the same `jj workspace` set and is not the root.

### Product-level classification rule

- Disk structure and product behavior are related but not identical.
- Low-level JJ inspection may still detect that a repository has child workspaces on disk.
- Product behavior must only promote a repository into root or forked mode when at least one child `jj workspace` fork is both present and attached in SGAI.
- Unattached child workspaces do not change the served repository mode.
- If the last attached fork is detached, the repository must fall back from forked mode to repository mode immediately.

## Source Of Truth For Future Changes

- Preserve structural JJ detection for workspace relationships.
- Keep attachment awareness in the product grouping and serving layer, because that is the behavior surfaced to the UI and external interfaces.
- When future changes touch external attachment or workspace grouping, consult this record before changing classification rules.
