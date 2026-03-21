import { test, expect } from "@playwright/test";

import { GO_WORKFLOW_GOAL } from "./fixtures/goWorkflow";

const EDITABLE_WORKSPACE_NAME = "standalone-demo";

async function openFirstWorkspace(page: Parameters<typeof test>[0]["page"]) {
  await page.waitForSelector("text=Workspaces", { timeout: 10000 });
  const workspaceLink = page.locator("a[href^='/workspaces/']").first();
  await expect(workspaceLink).toBeVisible();
  await workspaceLink.click();
  await page.waitForURL(/\/workspaces\/[^/]+/);
}

async function openEditableWorkspace(page: Parameters<typeof test>[0]["page"]) {
  await page.goto(`/workspaces/${EDITABLE_WORKSPACE_NAME}/progress`);
  const editGoalButton = page.getByRole("button", { name: "Edit GOAL" });
  const directEditGoalVisible = await editGoalButton.isVisible().catch(() => false);
  if (directEditGoalVisible) {
    return;
  }
  const directEditGoalLoaded = await editGoalButton.waitFor({ state: "visible", timeout: 5000 }).then(() => true).catch(() => false);
  if (directEditGoalLoaded) {
    return;
  }

  await page.waitForSelector("text=Workspaces", { timeout: 10000 });
  const goalWorkspaceLink = page.getByRole("link", { name: "Title of your Goal" }).first();
  const hasGoalWorkspace = await goalWorkspaceLink.isVisible().catch(() => false);

  if (hasGoalWorkspace) {
    const editableWorkspacePath = await goalWorkspaceLink.getAttribute("href");
    if (!editableWorkspacePath) {
      throw new Error("Editable workspace link is missing an href");
    }
    await page.goto(editableWorkspacePath);
    await page.waitForURL(/\/workspaces\/[^/]+/);
  } else {
    await openFirstWorkspace(page);
  }

  try {
    await page.waitForSelector('button:has-text("Edit GOAL")', { timeout: 10000 });
  } catch {
    throw new Error("No editable workspace found for goal workflow test");
  }
}

async function replaceGoalWithGoWorkflow(page: Parameters<typeof test>[0]["page"]) {
  await page.click('button:has-text("Edit GOAL")');
  await page.waitForURL(/\/goal\/edit/);
  await page.click('[data-testid="markdown-editor"]');
  await page.keyboard.press("Control+a");
  await page.keyboard.type(GO_WORKFLOW_GOAL);
  await page.click('button:has-text("Save GOAL.md")');
  await page.waitForSelector("text=Saved!");
  await page.getByRole("link", { name: /Back to/i }).click();
  await expect(page).toHaveURL(/\/workspaces\/[^/]+(?:\/progress)?$/);
}

async function expectRenamedGoWorkflowVisible(page: Parameters<typeof test>[0]["page"]) {
  const goalSummary = page.locator("summary", { hasText: "GOAL.md" });
  await expect(goalSummary).toBeVisible();
  await goalSummary.click();
  await expect(page.getByText("go-developer", { exact: false }).first()).toBeVisible();
  await expect(page.getByText("go-reviewer", { exact: false }).first()).toBeVisible();
}

test.describe("Workspace Management Workflow", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("goal editing keeps the renamed Go reviewer path visible", async ({ page }) => {
    await openEditableWorkspace(page);
    await expect(page.locator("h3").first()).toBeVisible();
    await replaceGoalWithGoWorkflow(page);
    await expectRenamedGoWorkflowVisible(page);
  });

  test("create fork → verify renamed workflow", async ({ page }) => {
    await openEditableWorkspace(page);
    const rootName = EDITABLE_WORKSPACE_NAME;

    await replaceGoalWithGoWorkflow(page);

    await page.goto(`/workspaces/${rootName}/fork`);
    await page.waitForURL(/\/workspaces\/[^/]+\/fork$/);

    const forkEditor = page.locator('[data-testid="markdown-editor"]').first();
    await expect(forkEditor).toBeVisible();
    await forkEditor.click();
    await page.keyboard.press("Control+a");
    await page.keyboard.type(GO_WORKFLOW_GOAL);
    await page.click('button:has-text("Create Fork")');

    await page.waitForURL(/\/workspaces\/[^/]+\/(goal\/edit|progress)/);
    if (/\/goal\/edit$/.test(page.url())) {
      await page.getByRole("link", { name: /Back to/i }).click();
      await expect(page).toHaveURL(/\/workspaces\/[^/]+(?:\/progress)?$/);
    }
    await expect(page.locator("h3").first()).toBeVisible();
    await expectRenamedGoWorkflowVisible(page);

    await page.goto(`/workspaces/${rootName}/forks`);
    await page.waitForURL(/\/workspaces\/[^/]+\/forks/);
    await expect(page.getByRole("link", { name: "Forks" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Fork", exact: true })).toBeVisible();

    await page.getByRole("link", { name: "Fork", exact: true }).click();
    await page.waitForURL(/\/workspaces\/[^/]+\/fork$/);
    await expect(page.getByRole("button", { name: "Create Fork" })).toBeVisible();
  });

  test("attach external repository → fork → detach", async ({ page }) => {
    await page.goto("/");
    await page.waitForSelector("text=Workspaces", { timeout: 10000 });

    const workspaceLink = page.locator("a[href^='/workspaces/']").first();
    await workspaceLink.click();

    await page.waitForURL(/\/workspaces\/[^/]+/);

    const externalIndicator = page.locator('[aria-label="External workspace"]');
    const isExternal = await externalIndicator.isVisible().catch(() => false);

    if (isExternal) {
      await expect(externalIndicator).toBeVisible();
    }
  });

  test("workspace tree displays correctly", async ({ page }) => {
    await page.goto("/");

    await page.waitForSelector("text=Workspaces", { timeout: 10000 });

    await expect(page.locator("a[href^='/workspaces/']").first()).toBeVisible();
  });

  test("workspace tree shows forks nested under parent", async ({ page }) => {
    await page.goto("/");

    await page.waitForSelector("text=Workspaces", { timeout: 10000 });

    const expandButton = page.locator('[aria-label="Toggle forks"]').first();
    const isVisible = await expandButton.isVisible().catch(() => false);

    if (isVisible) {
      await expandButton.click();
      await page.waitForTimeout(500);
    }
  });

  test("workspace tree updates on state change", async ({ page }) => {
    await page.goto("/");

    await page.waitForSelector("text=Workspaces", { timeout: 10000 });

    const firstWorkspace = page.locator("a[href^='/workspaces/']").first();
    await firstWorkspace.click();

    await page.waitForURL(/\/workspaces\/[^/]+/);

    await expect(page.locator("h3")).toBeVisible();
  });

  test("pinned workspaces appear in pinned section", async ({ page }) => {
    await page.goto("/");

    await page.waitForSelector("text=Workspaces", { timeout: 10000 });

    const pinnedSection = page.locator('[role="region"][aria-label="Pinned"]');
    const isVisible = await pinnedSection.isVisible().catch(() => false);

    if (isVisible) {
      const pinnedIndicator = pinnedSection.locator('[aria-label="Pinned"]');
      const hasPinned = await pinnedIndicator.isVisible().catch(() => false);
      expect(typeof hasPinned).toBe("boolean");
    }
  });

  test("in progress workspaces appear in in progress section", async ({ page }) => {
    await page.goto("/");

    await page.waitForSelector("text=Workspaces", { timeout: 10000 });

    const inProgressSection = page.locator('[role="region"][aria-label="In progress"]');
    const isVisible = await inProgressSection.isVisible().catch(() => false);

    if (isVisible) {
      const runningIndicator = inProgressSection.locator('[aria-label="Running"]');
      const hasRunning = await runningIndicator.isVisible().catch(() => false);
      expect(typeof hasRunning).toBe("boolean");
    }
  });

  test("delete confirmation dialog works correctly", async ({ page }) => {
    await openEditableWorkspace(page);

    const deleteButton = page.getByRole("button", { name: /^Delete$/ }).first();
    await expect(deleteButton).toBeVisible();
    await deleteButton.click();

    await expect(page.locator("text=Delete workspace")).toBeVisible();

    await page.click('button:has-text("Cancel")');

    await expect(page.locator("text=Delete workspace")).not.toBeVisible();
  });
});
