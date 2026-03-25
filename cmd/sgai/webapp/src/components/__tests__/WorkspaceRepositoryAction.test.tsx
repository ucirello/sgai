import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { api } from "@/lib/api";
import { WorkspaceRepositoryAction } from "../WorkspaceRepositoryAction";
import type {
  ApiRepositoryAction,
  ApiRepositoryActionIcon,
  ApiRepositoryActionPresentation,
  ApiRepositoryActionTone,
  ApiRepositoryOperation,
  ApiWorkspaceEntry,
} from "@/types";

const mockDeleteWorkspace = mock(() => Promise.resolve({ deleted: true }));

function createRepositoryAction(overrides: Partial<ApiRepositoryAction> = {}): ApiRepositoryAction {
  const action: ApiRepositoryAction = {
    repositoryMode: "fork",
    entryPoint: "choose",
    allowedOperations: ["detach", "delete"],
    attachedForkCount: 0,
    running: false,
    presentation: createRepositoryPresentation("demo-fork", {
      repositoryMode: "fork",
      entryPoint: "choose",
      allowedOperations: ["detach", "delete"],
      defaultOperation: undefined,
    }),
    ...overrides,
  };

  if (!overrides.presentation) {
    action.presentation = createRepositoryPresentation("demo-fork", action);
  }

  return action;
}

function createRepositoryPresentation(
  workspaceName: string,
  action: Pick<ApiRepositoryAction, "repositoryMode" | "entryPoint" | "allowedOperations" | "defaultOperation">,
): ApiRepositoryActionPresentation {
  const operationLabel = (operation: ApiRepositoryOperation) => operation === "delete" ? "Delete" : "Detach";
  const operationIcon = (operation: ApiRepositoryOperation): ApiRepositoryActionIcon => operation;
  const operationTone = (operation: ApiRepositoryOperation): ApiRepositoryActionTone => operation === "delete" ? "destructive" : "neutral";

  if (action.entryPoint === "choose") {
    const repositoryNoun = action.repositoryMode === "fork" ? "fork" : "workspace";
    const subject = action.repositoryMode === "fork" ? `fork ${workspaceName}` : workspaceName;

    return {
      detailTriggerLabel: "Choose action",
      treeTriggerLabel: `Choose action for ${subject}`,
      forkRowTriggerLabel: `Choose action for ${subject}`,
      dialogTitle: action.repositoryMode === "fork" ? "Choose fork action" : "Choose workspace action",
      dialogDescription: `Choose what to do with ${repositoryNoun} '${workspaceName}'. ${action.allowedOperations.map((operation) => operation === "delete"
        ? `Delete permanently removes the ${repositoryNoun} from disk.`
        : `Detach removes the ${repositoryNoun} from the SGAI workspace list and keeps the files on disk.`).join(" ")}`,
      icon: "choose" as const,
      tone: "neutral" as const,
      operations: action.allowedOperations.map((operation) => ({
        operation,
        label: operationLabel(operation),
        icon: operationIcon(operation),
        tone: operationTone(operation),
      })),
    };
  }

  const confirmOperation = action.defaultOperation ?? action.allowedOperations[0] ?? "detach";

  return {
    detailTriggerLabel: operationLabel(confirmOperation),
    treeTriggerLabel: `${operationLabel(confirmOperation)} ${workspaceName}`,
    forkRowTriggerLabel: `${operationLabel(confirmOperation)} ${workspaceName}`,
    dialogTitle: confirmOperation === "delete" ? "Delete workspace" : "Detach workspace",
    dialogDescription: confirmOperation === "delete"
      ? `This will permanently delete '${workspaceName}' from disk. This action cannot be undone.`
      : `This will remove '${workspaceName}' from the SGAI workspace list. The files on disk will NOT be deleted.`,
    icon: operationIcon(confirmOperation),
    tone: operationTone(confirmOperation),
    operations: action.allowedOperations.map((operation) => ({
      operation,
      label: operationLabel(operation),
      icon: operationIcon(operation),
      tone: operationTone(operation),
    })),
  };
}

function createWorkspace(
  overrides: Partial<Pick<ApiWorkspaceEntry, "name" | "dir" | "repositoryAction">> = {},
): Pick<ApiWorkspaceEntry, "name" | "dir" | "repositoryAction"> {
  const workspace = {
    name: "demo-fork",
    dir: "/tmp/demo-fork",
    repositoryAction: createRepositoryAction(),
    ...overrides,
  };

  const hasPresentationOverride = Boolean(overrides.repositoryAction && "presentation" in overrides.repositoryAction);

  workspace.repositoryAction = hasPresentationOverride
    ? workspace.repositoryAction
    : {
      ...workspace.repositoryAction,
      presentation: createRepositoryPresentation(workspace.name, workspace.repositoryAction),
    };

  return workspace;
}

async function expectFocusRestoreAfterCancel(
  context: "tree" | "fork-row",
  triggerLabel: string,
) {
  const user = userEvent.setup();

  render(
    <WorkspaceRepositoryAction
      workspace={createWorkspace()}
      context={context}
    />,
  );

  const trigger = screen.getByRole("button", { name: triggerLabel });

  await user.click(trigger);

  expect(screen.getByRole("alertdialog")).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Cancel" }));

  await waitFor(() => {
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });
}

describe("WorkspaceRepositoryAction", () => {
  beforeEach(() => {
    cleanup();
    document.body.style.pointerEvents = "auto";
    mockDeleteWorkspace.mockClear();
    spyOn(api.workspaces, "deleteWorkspace").mockImplementation((...args) => mockDeleteWorkspace(...args));
  });

  afterEach(() => {
    mock.restore();
  });

  it("describes only backend-allowed chooser operations", async () => {
    const user = userEvent.setup();

    render(
      <WorkspaceRepositoryAction
        workspace={createWorkspace({
          repositoryAction: createRepositoryAction({
            allowedOperations: ["delete"],
          }),
        })}
        context="detail"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Choose action" }));

    const dialog = screen.getByRole("alertdialog");
    expect(within(dialog).getByText(/Delete permanently removes the fork from disk/i)).toBeTruthy();
    expect(within(dialog).queryByText(/Detach removes it from the SGAI workspace list/i)).toBeNull();
    expect(within(dialog).queryByRole("button", { name: "Detach" })).toBeNull();
    expect(within(dialog).getByRole("button", { name: "Delete" })).toBeTruthy();
  });

  it("renders chooser buttons in backend-provided order", async () => {
    const user = userEvent.setup();

    render(
      <WorkspaceRepositoryAction
        workspace={createWorkspace({
          repositoryAction: createRepositoryAction({
            allowedOperations: ["delete", "detach"],
          }),
        })}
        context="detail"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Choose action" }));

    const dialog = screen.getByRole("alertdialog");
    const actionButtons = dialog.querySelector('[data-slot="repository-action-buttons"]');

    expect(actionButtons).toBeTruthy();

    const operationButtons = within(actionButtons as HTMLElement)
      .getAllByRole("button")
      .map((button) => button.textContent?.trim())
      .filter((label): label is string => label === "Delete" || label === "Detach");

    expect(operationButtons).toEqual(["Delete", "Detach"]);
  });

  it("keeps chooser operation order backend-controlled on narrow layouts", async () => {
    const user = userEvent.setup();

    render(
      <WorkspaceRepositoryAction
        workspace={createWorkspace({
          repositoryAction: createRepositoryAction({
            allowedOperations: ["detach", "delete"],
          }),
        })}
        context="detail"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Choose action" }));

    const dialog = screen.getByRole("alertdialog");
    const actionButtons = dialog.querySelector('[data-slot="repository-action-buttons"]');

    expect(actionButtons).toBeTruthy();
    expect(actionButtons?.closest('[data-slot="alert-dialog-footer"]')).toBeNull();

    const operationButtons = within(actionButtons as HTMLElement)
      .getAllByRole("button")
      .map((button) => button.textContent?.trim())
      .filter((label): label is string => label === "Detach" || label === "Delete");

    expect(operationButtons).toEqual(["Detach", "Delete"]);
  });

  it("renders chooser labels and descriptions from backend presentation metadata", async () => {
    const user = userEvent.setup();

    render(
      <WorkspaceRepositoryAction
        workspace={createWorkspace({
          repositoryAction: createRepositoryAction({
            presentation: {
              detailTriggerLabel: "Resolve repository",
              treeTriggerLabel: "Resolve repository for fork demo-fork",
              forkRowTriggerLabel: "Resolve repository for fork demo-fork",
              dialogTitle: "Backend chooser title",
              dialogDescription: "Backend chooser description",
              icon: "choose",
              tone: "neutral",
              operations: [
                {
                  operation: "delete",
                  label: "Erase fork",
                  icon: "delete",
                  tone: "destructive",
                },
                {
                  operation: "detach",
                  label: "Unlist fork",
                  icon: "detach",
                  tone: "neutral",
                },
              ],
            },
          } as ApiRepositoryAction),
        })}
        context="detail"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Resolve repository" }));

    const dialog = screen.getByRole("alertdialog");
    expect(within(dialog).getByText("Backend chooser title")).toBeTruthy();
    expect(within(dialog).getByText("Backend chooser description")).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: "Erase fork" })).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: "Unlist fork" })).toBeTruthy();
  });

  it("renders confirm copy from backend presentation metadata", async () => {
    const user = userEvent.setup();

    render(
      <WorkspaceRepositoryAction
        workspace={createWorkspace({
          repositoryAction: createRepositoryAction({
            repositoryMode: "standalone",
            entryPoint: "confirm",
            allowedOperations: ["detach"],
            defaultOperation: "detach",
            presentation: {
              detailTriggerLabel: "Archive workspace",
              treeTriggerLabel: "Archive demo-fork",
              forkRowTriggerLabel: "Archive demo-fork",
              dialogTitle: "Backend confirm title",
              dialogDescription: "Backend confirm description",
              icon: "detach",
              tone: "neutral",
              operations: [
                {
                  operation: "detach",
                  label: "Archive workspace",
                  icon: "detach",
                  tone: "neutral",
                },
              ],
            },
          } as ApiRepositoryAction),
        })}
        context="detail"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Archive workspace" }));

    const dialog = screen.getByRole("alertdialog");
    expect(within(dialog).getByText("Backend confirm title")).toBeTruthy();
    expect(within(dialog).getByText("Backend confirm description")).toBeTruthy();
    expect(within(dialog).getAllByRole("button", { name: "Archive workspace" }).length).toBeGreaterThan(0);
  });

  it("restores focus to the tree trigger after cancel", async () => {
    await expectFocusRestoreAfterCancel("tree", "Choose action for fork demo-fork");
  });

  it("restores focus to the fork-row trigger after cancel", async () => {
    await expectFocusRestoreAfterCancel("fork-row", "Choose action for fork demo-fork");
  });

  it("removes the action trigger after a successful operation", async () => {
    const user = userEvent.setup();

    render(
      <WorkspaceRepositoryAction
        workspace={createWorkspace()}
        context="detail"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Choose action" }));
    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Choose action" })).toBeNull();
    });
  });

  it("passes workspaceDir when running destructive actions", async () => {
    const user = userEvent.setup();

    render(
      <WorkspaceRepositoryAction
        workspace={createWorkspace({ dir: "/tmp/teams/second/demo-fork" })}
        context="detail"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Choose action" }));
    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(mockDeleteWorkspace).toHaveBeenCalledWith("demo-fork", "delete", "/tmp/teams/second/demo-fork");
    });
  });
});
