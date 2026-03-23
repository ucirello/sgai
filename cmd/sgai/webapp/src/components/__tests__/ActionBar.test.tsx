import { describe, it, expect, beforeEach } from "bun:test";
import { render, screen, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ActionBar } from "../ActionBar";

const createAction = (overrides = {}) => ({
  name: "Run Tests",
  kind: "prompt",
  model: "model-1",
  prompt: "run tests",
  variables: [] as string[],
  description: "Run test suite",
  ...overrides,
});

beforeEach(() => {
  cleanup();
  document.body.style.pointerEvents = "auto";
});

describe("ActionBar", () => {
  it("renders nothing when actions array is empty and there is no config error", () => {
    const { container } = render(
      <TooltipProvider>
        <ActionBar actions={[]} isRunning={false} onActionClick={() => {}} />
      </TooltipProvider>,
    );

    expect(container.innerHTML).toBe("");
  });

  it("renders action buttons", () => {
    const actions = [createAction()];

    render(
      <TooltipProvider>
        <ActionBar actions={actions} isRunning={false} onActionClick={() => {}} />
      </TooltipProvider>,
    );

    expect(screen.getByText("Run Tests")).toBeTruthy();
  });

  it("disables buttons when running", () => {
    const actions = [createAction()];

    render(
      <TooltipProvider>
        <ActionBar actions={actions} isRunning={true} onActionClick={() => {}} />
      </TooltipProvider>,
    );

    expect(screen.getByRole("button", { name: "Run Tests" }).hasAttribute("disabled")).toBe(true);
  });

  it("calls onActionClick when button is clicked", async () => {
    const user = userEvent.setup();
    const calls: unknown[] = [];
    const onActionClick = (action: ReturnType<typeof createAction>, variables: Record<string, string>) => {
      calls.push([action, variables]);
    };
    const actions = [createAction()];

    render(
      <TooltipProvider>
        <ActionBar actions={actions} isRunning={false} onActionClick={onActionClick} />
      </TooltipProvider>,
    );

    await user.click(screen.getByText("Run Tests"));
    expect(calls).toEqual([[actions[0], {}]]);
  });

  it("adds contextual accessible labels when provided", () => {
    const actions = [createAction()];

    render(
      <TooltipProvider>
        <ActionBar
          actions={actions}
          isRunning={false}
          onActionClick={() => {}}
          accessibilityContext="fork test-workspace-fork"
        />
      </TooltipProvider>,
    );

    expect(screen.getByRole("toolbar", { name: "Action buttons for fork test-workspace-fork" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Run Tests for fork test-workspace-fork" })).toBeTruthy();
  });

  it("disables invalid actions and shows their configuration errors", () => {
    const actions = [createAction({ validationError: 'action "Broken" must set exactly one of prompt or script' })];

    render(
      <TooltipProvider>
        <ActionBar actions={actions} isRunning={false} onActionClick={() => {}} />
      </TooltipProvider>,
    );

    expect(screen.getByRole("button", { name: "Run Tests" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/exactly one of prompt or script/i)).toBeTruthy();
  });

  it("shows a workspace-level action configuration error even without actions", () => {
    render(
      <TooltipProvider>
        <ActionBar
          actions={[]}
          actionConfigError="invalid JSON syntax"
          isRunning={false}
          onActionClick={() => {}}
        />
      </TooltipProvider>,
    );

    expect(screen.getByText(/action configuration error/i)).toBeTruthy();
    expect(screen.getByText(/invalid JSON syntax/i)).toBeTruthy();
  });

  it("opens a parameter dialog and submits collected values", async () => {
    const user = userEvent.setup();
    const calls: unknown[] = [];
    const onActionClick = (action: ReturnType<typeof createAction>, variables: Record<string, string>) => {
      calls.push([action, variables]);
    };
    const actions = [createAction({ name: "Deploy", variables: ["Branch", "Environment"] })];

    render(
      <TooltipProvider>
        <ActionBar actions={actions} isRunning={false} onActionClick={onActionClick} />
      </TooltipProvider>,
    );

    await user.click(screen.getByRole("button", { name: "Deploy" }));

    expect(screen.getByLabelText("Branch")).toBeTruthy();
    expect(calls).toEqual([]);

    await user.type(screen.getByLabelText("Branch"), "main");
    await user.type(screen.getByLabelText("Environment"), "staging");
    await user.click(screen.getByRole("button", { name: "Run" }));

    expect(calls).toEqual([[
      actions[0],
      {
        Branch: "main",
        Environment: "staging",
      },
    ]]);
  });
});
