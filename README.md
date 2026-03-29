# SGAI

SGAI is a goal-driven, multi-agent software factory you run locally. You define the outcome in `GOAL.md`, approve the plan, and let specialized agents build and verify the work inside your repository.

## Canonical operator flow

Use opencode as the primary operator path. Ask it to inspect the repository and then follow the hosted installation guide:

```bash
opencode --prompt "Check the contents of https://github.com/ucirello/sgai and be prepared to answer questions about scope, use cases, purpose, and operator workflow. Then follow the installation instructions from https://github.com/ucirello/sgai/blob/main/INSTALLATION.md and tell me when SGAI is ready to use."
```

That keeps installation agent-driven instead of manual. If you prefer Claude, Codex, or another agent harness, use the same pattern: point the agent at the repository URL and the hosted installation guide.

## What SGAI is

- A local system for goal-driven software delivery
- A coordinator plus specialist agents that plan, implement, review, and validate work
- A `GOAL.md`-driven workflow with explicit scope, routing, and completion checks
- A web UI plus MCP/HTTP surfaces for supervision and automation

## Advanced links

- Installation guide: https://github.com/ucirello/sgai/blob/main/INSTALLATION.md
- GOAL example: https://github.com/ucirello/sgai/blob/main/cmd/sgai/GOAL.example.md
- Skills entrypoint: https://github.com/ucirello/sgai/blob/main/cmd/sgai/skel/.sgai/skills/using-skills/SKILL.md
- Demo video: https://youtu.be/NYmjhwLUg8Q
- Issues: https://github.com/ucirello/sgai/issues
- Discussions: https://github.com/ucirello/sgai/discussions
