---
# Required: this is the repository title shown in the UI.
title: Title of your Goal
flow: |
  "go-developer" -> "go-reviewer"
  "react-developer" -> "react-reviewer"
  "general-purpose"
  "project-critic-council"
  "skill-writer"
  "stpa-analyst"
alias:
  # "go-developer-lite": "go-developer"
models:
  "coordinator": "anthropic/claude-opus-4-6 (max)"
  "go-developer": "anthropic/claude-opus-4-6"
  "go-reviewer": "anthropic/claude-opus-4-6"
  "general-purpose": "anthropic/claude-opus-4-6"
  "react-developer": "anthropic/claude-opus-4-6"
  "react-reviewer": "anthropic/claude-opus-4-6"
  "stpa-analyst": "anthropic/claude-opus-4-6"
  "project-critic-council": ["anthropic/claude-opus-4-6"]
  "skill-writer": "anthropic/claude-opus-4-6 (max)"
  # "go-developer-lite": "anthropic/claude-haiku-4-5"
---

One or two paragraphs explaining what you want to do. The `title` above is the only user-facing repository label. If `GOAL.md` is missing or has no frontmatter, SGAI falls back to the repository directory name instead of deriving a label from markdown headings.

- [ ] a list of verifiable checks to help agents to communicate their progress
  - [ ] they can even be nested
