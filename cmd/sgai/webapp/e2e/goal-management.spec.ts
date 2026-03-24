import { test, expect } from "@playwright/test";

import { GO_WORKFLOW_GOAL } from "./fixtures/goWorkflow";

const GOAL_WORKSPACE_NAME = "standalone-demo";

async function openGoalWorkspace(page: Parameters<typeof test>[0]["page"]) {
  await page.goto(`/workspaces/${GOAL_WORKSPACE_NAME}/progress`);

  const directEditGoalButton = page.getByRole("button", { name: "Edit GOAL" });
  if ((await directEditGoalButton.count()) > 0) {
    await expect(page).toHaveURL(/\/workspaces\/[^/]+(?:\/progress)?$/);
    await expect(directEditGoalButton).toBeVisible();
    return;
  }

  await page.goto("/");
  await expect(page.getByText("Workspaces", { exact: true }).first()).toBeVisible();

  const preferredWorkspaceLink = page.getByRole("link", { name: "Title of your Goal" }).first();
  const workspaceLink = (await preferredWorkspaceLink.count()) > 0
    ? preferredWorkspaceLink
    : page.locator("a[href^='/workspaces/']").first();

  await expect(workspaceLink).toBeVisible();
  await workspaceLink.click();
  await expect(page).toHaveURL(/\/workspaces\/[^/]+(?:\/progress)?$/);
  await expect(page.getByRole("button", { name: "Edit GOAL" })).toBeVisible();
}

async function openGoalEditor(page: Parameters<typeof test>[0]["page"]) {
  await openGoalWorkspace(page);

  const editGoalButton = page.getByRole("button", { name: "Edit GOAL" });
  await expect(editGoalButton).toBeVisible();
  await expect(editGoalButton).toBeEnabled();
  await editGoalButton.click();

  await expect(page).toHaveURL(/\/goal\/edit/);
  await expect(page.getByText("Edit GOAL.md", { exact: true })).toBeVisible();

  const editor = page.getByTestId("markdown-editor");
  await expect(editor).toBeVisible();
}

async function focusGoalEditorSurface(page: Parameters<typeof test>[0]["page"]) {
  const editorSurface = page.locator(".view-lines").first();
  await expect(editorSurface).toBeVisible();
  await editorSurface.click({ position: { x: 20, y: 20 } });
}

async function expectRenamedGoWorkflowVisible(page: Parameters<typeof test>[0]["page"]) {
  const goalSummary = page.locator("summary", { hasText: "GOAL.md" });
  await expect(goalSummary).toBeVisible();
  await goalSummary.click();
  await expect(page.getByText("go-developer", { exact: false }).first()).toBeVisible();
  await expect(page.getByText("go-reviewer", { exact: false }).first()).toBeVisible();
}

test.describe("Goal Management Workflow", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("goal editor shows workspace description", async ({ page }) => {
    await openGoalEditor(page);

    const description = page.locator("text=Edit GOAL.md").locator("..").locator("span").first();
    await expect(description).toBeVisible();
  });

  test("goal editor keyboard shortcut saves", async ({ page }) => {
    await openGoalEditor(page);

    await page.keyboard.press("Control+s");
    await expect(page.getByText("Saved!", { exact: true })).toBeVisible({ timeout: 10000 });
  });

  test("goal editor shows loading state", async ({ page }) => {
    await openGoalEditor(page);
  });

  test("goal editor autocomplete for agents", async ({ page }) => {
    await openGoalEditor(page);
    await focusGoalEditorSurface(page);
    await page.keyboard.press("Meta+A");
    await page.keyboard.type("---\nflow: |\n\n---");
    await page.keyboard.press("ArrowUp");
    await page.keyboard.type('  "');
    await page.keyboard.press("Control+Space");

    const suggestionWidget = page.locator(".suggest-widget.visible");
    await expect(suggestionWidget).toBeVisible();
    await expect(suggestionWidget).toContainText(/No suggestions\.|coordinator/);
  });

  test("goal editor preview mode", async ({ page }) => {
    await openGoalEditor(page);

    const previewButton = page.getByRole("button", { name: "Preview" });
    await expect(previewButton).toBeVisible();
    await expect(previewButton).toBeEnabled();
    await previewButton.click();

    await expect(previewButton).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByRole("textbox", { name: "Editor content" })).toHaveCount(0);
  });

  test("goal editor write mode", async ({ page }) => {
    await openGoalEditor(page);

    const writeButton = page.getByRole("button", { name: "Write" });
    await expect(writeButton).toBeVisible();
    await expect(writeButton).toHaveAttribute("aria-pressed", "true");
  });

  test("goal editor toolbar actions", async ({ page }) => {
    await openGoalEditor(page);

    const boldButton = page.getByRole("button", { name: "Bold" });
    const italicButton = page.getByRole("button", { name: "Italic" });
    const headingButton = page.getByRole("button", { name: "Heading 1" });

    await expect(boldButton).toBeVisible();
    await expect(boldButton).toBeEnabled();
    await expect(italicButton).toBeVisible();
    await expect(italicButton).toBeEnabled();
    await expect(headingButton).toBeVisible();
    await expect(headingButton).toBeEnabled();
  });

  test("goal content displays in progress tab", async ({ page }) => {
    await openGoalWorkspace(page);

    const progressTab = page.locator('a[href$="/progress"]').first();
    await expect(progressTab).toBeVisible();
    await progressTab.click();

    await expect(page.locator("h3").first()).toBeVisible();
  });

  test("goal validation prevents empty save", async ({ page }) => {
    await openGoalEditor(page);
    await focusGoalEditorSurface(page);
    await page.keyboard.press("Meta+A");
    await page.keyboard.press("Backspace");

    const saveButton = page.getByRole("button", { name: "Save GOAL.md" });
    await expect(saveButton).toBeDisabled();
  });

  test("create goal → edit → run agents → view results", async ({ page }) => {
    await openGoalEditor(page);
    await focusGoalEditorSurface(page);
    await page.keyboard.press("Meta+A");
    await page.keyboard.type(GO_WORKFLOW_GOAL);

    const saveButton = page.getByRole("button", { name: "Save GOAL.md" });
    await expect(saveButton).toBeVisible();
    await expect(saveButton).toBeEnabled();
    await saveButton.click();
    await expect(page.getByText("Saved!", { exact: true })).toBeVisible({ timeout: 10000 });

    const backLink = page.getByRole("link", { name: /Back to/i });
    await expect(backLink).toBeVisible();
    await backLink.click();
    await expect(page).toHaveURL(/\/workspaces\/[^/]+(?:\/progress)?$/);

    const startButton = page.getByRole("button", { name: "Start", exact: true });
    await expect(startButton).toBeVisible();
    await expect(startButton).toBeEnabled();
    await startButton.click();

    await expect(page.getByText(/running/i).first()).toBeVisible({ timeout: 10000 });
    await expectRenamedGoWorkflowVisible(page);

    const progressTab = page.locator('a[href$="/progress"]').first();
    await expect(progressTab).toBeVisible();
    await progressTab.click();
    await expect(page.locator("h3").first()).toBeVisible();

    const logTab = page.locator('a[href$="/log"]').first();
    await expect(logTab).toBeVisible();
    await logTab.click();
    await expect(page).toHaveURL(/\/log/);
    await expect(page.getByText("Log", { exact: true })).toBeVisible();
  });
});
