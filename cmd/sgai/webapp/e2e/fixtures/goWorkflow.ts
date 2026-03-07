export const GO_WORKFLOW_GOAL = `---
flow: |
  "coordinator" -> "go-developer"
  "go-developer" -> "go-reviewer"
models:
  "coordinator": "opencode/glm-5"
  "go-developer": "opencode/glm-5"
  "go-reviewer": "opencode/glm-5"
completionGateScript: make test
---

# Test Goal

## Task: Implement Feature X

- [ ] Design the API
- [ ] Write tests
- [ ] Implement the feature
`;
