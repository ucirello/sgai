---
name: testing-anti-patterns
description: Use when writing or changing tests, adding mocks, tempted to add test-only methods to production code, or considering absence tests for deleted helpers, deleted client members, exported API shape, or removed routes. Prevents testing mock behavior, production pollution with test-only methods, mocking without understanding dependencies, and memorial tests whose sole purpose is to prove deleted internal/client surface stays gone while preserving tests for current observable behavior and current public/server contracts such as 404 removed-route checks.
---

# Testing Anti-Patterns

## Overview

Tests must verify real behavior, not mock behavior. Mocks are a means to isolate, not the thing being tested.

**Core principle:** Test what the code does, not what the mocks do.

**After deletion, test the surviving behavior or public/server contract, not the tombstone of the deleted implementation.**

**Following strict TDD prevents these anti-patterns.**

## The Iron Laws

```
1. NEVER test mock behavior
2. NEVER add test-only methods to production classes
3. NEVER mock without understanding dependencies
4. NEVER add tests whose sole purpose is to prove deleted internal/client surface stays gone
5. DO keep tests for current observable behavior and current public/server contracts after deletion
```

## Anti-Pattern 1: Testing Mock Behavior

**The violation:**
```typescript
// ❌ BAD: Testing that the mock exists
test('renders sidebar', () => {
  render(<Page />);
  expect(screen.getByTestId('sidebar-mock')).toBeInTheDocument();
});
```

**Why this is wrong:**
- You're verifying the mock works, not that the component works
- Test passes when mock is present, fails when it's not
- Tells you nothing about real behavior

**your human partner's correction:** "Are we testing the behavior of a mock?"

**The fix:**
```typescript
// ✅ GOOD: Test real component or don't mock it
test('renders sidebar', () => {
  render(<Page />);  // Don't mock sidebar
  expect(screen.getByRole('navigation')).toBeInTheDocument();
});

// OR if sidebar must be mocked for isolation:
// Don't assert on the mock - test Page's behavior with sidebar present
```

### Gate Function

```
BEFORE asserting on any mock element:
  Ask: "Am I testing real component behavior or just mock existence?"

  IF testing mock existence:
    STOP - Delete the assertion or unmock the component

  Test real behavior instead
```

## Anti-Pattern 2: Test-Only Methods in Production

**The violation:**
```typescript
// ❌ BAD: destroy() only used in tests
class Session {
  async destroy() {  // Looks like production API!
    await this._workspaceManager?.destroyWorkspace(this.id);
    // ... cleanup
  }
}

// In tests
afterEach(() => session.destroy());
```

**Why this is wrong:**
- Production class polluted with test-only code
- Dangerous if accidentally called in production
- Violates YAGNI and separation of concerns
- Confuses object lifecycle with entity lifecycle

**The fix:**
```typescript
// ✅ GOOD: Test utilities handle test cleanup
// Session has no destroy() - it's stateless in production

// In test-utils/
export async function cleanupSession(session: Session) {
  const workspace = session.getWorkspaceInfo();
  if (workspace) {
    await workspaceManager.destroyWorkspace(workspace.id);
  }
}

// In tests
afterEach(() => cleanupSession(session));
```

### Gate Function

```
BEFORE adding any method to production class:
  Ask: "Is this only used by tests?"

  IF yes:
    STOP - Don't add it
    Put it in test utilities instead

  Ask: "Does this class own this resource's lifecycle?"

  IF no:
    STOP - Wrong class for this method
```

## Anti-Pattern 3: Mocking Without Understanding

**The violation:**
```typescript
// ❌ BAD: Mock breaks test logic
test('detects duplicate server', () => {
  // Mock prevents config write that test depends on!
  vi.mock('ToolCatalog', () => ({
    discoverAndCacheTools: vi.fn().mockResolvedValue(undefined)
  }));

  await addServer(config);
  await addServer(config);  // Should throw - but won't!
});
```

**Why this is wrong:**
- Mocked method had side effect test depended on (writing config)
- Over-mocking to "be safe" breaks actual behavior
- Test passes for wrong reason or fails mysteriously

**The fix:**
```typescript
// ✅ GOOD: Mock at correct level
test('detects duplicate server', () => {
  // Mock the slow part, preserve behavior test needs
  vi.mock('MCPServerManager'); // Just mock slow server startup

  await addServer(config);  // Config written
  await addServer(config);  // Duplicate detected ✓
});
```

### Gate Function

```
BEFORE mocking any method:
  STOP - Don't mock yet

  1. Ask: "What side effects does the real method have?"
  2. Ask: "Does this test depend on any of those side effects?"
  3. Ask: "Do I fully understand what this test needs?"

  IF depends on side effects:
    Mock at lower level (the actual slow/external operation)
    OR use test doubles that preserve necessary behavior
    NOT the high-level method the test depends on

  IF unsure what test depends on:
    Run test with real implementation FIRST
    Observe what actually needs to happen
    THEN add minimal mocking at the right level

  Red flags:
    - "I'll mock this to be safe"
    - "This might be slow, better mock it"
    - Mocking without understanding the dependency chain
```

## Anti-Pattern 4: Incomplete Mocks

**The violation:**
```typescript
// ❌ BAD: Partial mock - only fields you think you need
const mockResponse = {
  status: 'success',
  data: { userId: '123', name: 'Alice' }
  // Missing: metadata that downstream code uses
};

// Later: breaks when code accesses response.metadata.requestId
```

**Why this is wrong:**
- **Partial mocks hide structural assumptions** - You only mocked fields you know about
- **Downstream code may depend on fields you didn't include** - Silent failures
- **Tests pass but integration fails** - Mock incomplete, real API complete
- **False confidence** - Test proves nothing about real behavior

**The Iron Rule:** Mock the COMPLETE data structure as it exists in reality, not just fields your immediate test uses.

**The fix:**
```typescript
// ✅ GOOD: Mirror real API completeness
const mockResponse = {
  status: 'success',
  data: { userId: '123', name: 'Alice' },
  metadata: { requestId: 'req-789', timestamp: 1234567890 }
  // All fields real API returns
};
```

### Gate Function

```
BEFORE creating mock responses:
  Check: "What fields does the real API response contain?"

  Actions:
    1. Examine actual API response from docs/examples
    2. Include ALL fields system might consume downstream
    3. Verify mock matches real response schema completely

  Critical:
    If you're creating a mock, you must understand the ENTIRE structure
    Partial mocks fail silently when code depends on omitted fields

  If uncertain: Include all documented fields
```

## Anti-Pattern 5: Integration Tests as Afterthought

**The violation:**
```
✅ Implementation complete
❌ No tests written
"Ready for testing"
```

**Why this is wrong:**
- Testing is part of implementation, not optional follow-up
- TDD would have caught this
- Can't claim complete without tests

**The fix:**
```
TDD cycle:
1. Write failing test
2. Implement to pass
3. Refactor
4. THEN claim complete
```

## Anti-Pattern 6: Memorial Tests for Deleted Code

**The violation:**
```typescript
// ❌ BAD: Tombstone test for deleted client surface
test('openEditorGoal stays deleted', () => {
  expect('openEditorGoal' in api.workspaces).toBe(false);
  expect('openEditorProjectManagement' in api.workspaces).toBe(false);
});
```

**Why this is wrong:**
- It tests dead surface area instead of current behavior
- It locks in code shape after deletion rather than verifying what users or callers observe now
- It duplicates better coverage that should already exist in UI behavior tests or public/server contract tests
- It turns deleted code into a maintenance burden by keeping a tombstone around in test form

**The fix:**
```typescript
// ✅ GOOD: Verify the current product behavior
test('goal panel does not render a file-specific Open in Editor button', () => {
  renderGoalPanel();
  expect(screen.queryByRole('button', { name: /open in editor/i })).not.toBeInTheDocument();
});

// ✅ GOOD: Verify the current server contract after route removal
test('removed file-specific open-editor route returns 404', async () => {
  const response = await server.inject({
    method: 'POST',
    url: '/api/v1/workspaces/test-ws/open-editor/goal',
  });

  expect(response.statusCode).toBe(404);
});
```

### Gate Function

```
BEFORE adding a test after deleting code:
  1. Ask: "What current behavior or contract still matters now that this code is gone?"
  2. Ask: "Does this test describe current observable UI behavior or a current public/server contract?"

  IF yes:
    Keep or add the behavior/contract test
    Examples:
      - Button no longer renders in the UI
      - Removed route now returns 404
      - Current API response changed in a user-visible way

  IF the test only proves a deleted helper, deleted client member, or deleted repo-internal export is absent:
    STOP - Don't add it
    Delete the memorial test

  IF the surface is a documented external public API contract:
    Treat that as a public-contract question, not a deleted-internal-surface tombstone test
```

### Rationalization Table

| Excuse | Reality |
|--------|---------|
| "More tests are always safer" | Extra tests that only pin dead surface increase maintenance cost without protecting real behavior. |
| "It was exported, so we need a regression test proving it stays missing" | Repo-local reachability is not automatically a supported public contract; test observable behavior or actual public/server contract instead. |
| "Without an absence test someone may re-add it" | Re-adding unused surface is a code-review and cleanup problem unless it changes user-visible or public-contract behavior. |
| "404 tests for removed routes also mention deleted code, so they must be equally bad" | A 404 check is valid when 404 is the current server contract after removal. |

## When Mocks Become Too Complex

**Warning signs:**
- Mock setup longer than test logic
- Mocking everything to make test pass
- Mocks missing methods real components have
- Test breaks when mock changes

**your human partner's question:** "Do we need to be using a mock here?"

**Consider:** Integration tests with real components often simpler than complex mocks

## TDD Prevents These Anti-Patterns

**Why TDD helps:**
1. **Write test first** → Forces you to think about what you're actually testing
2. **Watch it fail** → Confirms test tests real behavior, not mocks
3. **Minimal implementation** → No test-only methods creep in
4. **Real dependencies** → You see what the test actually needs before mocking
5. **Clear contract focus after deletion** → You test surviving behavior and current contracts, not deleted implementation shape

**If you're testing mock behavior, you violated TDD** - you added mocks without watching test fail against real code first.

**If you're writing a tombstone test for deleted internals, you violated TDD's focus on current behavior** - you are preserving dead shape instead of describing surviving behavior.

## Quick Reference

| Anti-Pattern | Fix |
|--------------|-----|
| Assert on mock elements | Test real component or unmock it |
| Test-only methods in production | Move to test utilities |
| Mock without understanding | Understand dependencies first, mock minimally |
| Incomplete mocks | Mirror real API completely |
| Tests as afterthought | TDD - tests first |
| Memorial tests for deleted helpers/client members | Keep only tests for current observable behavior or current public/server contract |
| Over-complex mocks | Consider integration tests |

## Red Flags

- Assertion checks for `*-mock` test IDs
- Methods only called in test files
- Mock setup is >50% of test
- Test fails when you remove mock
- Can't explain why mock is needed
- Mocking "just to be safe"
- "Let's assert the deleted member is undefined"
- "More tests are always safer"
- "It used to be exported, so absence itself is the contract"
- "Let's prove the helper stays gone"

## The Bottom Line

**Mocks are tools to isolate, not things to test.**

**Deleted code is not a contract by itself.**

If TDD reveals you're testing mock behavior, you've gone wrong.

If a deletion-focused test only memorializes dead internal or client surface, you've gone wrong.

Fix: Test real behavior, or keep only the current observable/public-contract checks that still matter after deletion.
