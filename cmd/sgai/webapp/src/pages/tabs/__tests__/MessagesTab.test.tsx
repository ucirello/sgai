import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { act, render, screen, waitFor, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as workspacePageStateModule from "@/lib/workspace-page-state";
import { api } from "@/lib/api";
import { TooltipProvider } from "@/components/ui/tooltip";
import { MessagesTab } from "../MessagesTab";

const mockDeleteMessage = mock(() => Promise.resolve({ deleted: true }));
const mockTriggerWorkspacePageRefresh = mock(() => {});

beforeEach(() => {
  document.body.style.pointerEvents = "auto";
});

function deferredValue<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("MessagesTab", () => {
  beforeEach(() => {
    mockDeleteMessage.mockClear();
    mockTriggerWorkspacePageRefresh.mockClear();

    spyOn(api.workspaces, "deleteMessage").mockImplementation((...args) => mockDeleteMessage(...args));
    spyOn(workspacePageStateModule, "triggerWorkspacePageRefresh").mockImplementation(() => mockTriggerWorkspacePageRefresh());
  });

  afterEach(() => {
    mock.restore();
    cleanup();
  });

  it("relies on SSE instead of forcing a refresh after deleting a message", async () => {
    const user = userEvent.setup();

    render(
      <TooltipProvider>
        <MessagesTab
          workspaceName="test-workspace"
          messages={[{
            id: 1,
            fromAgent: "coordinator",
            toAgent: "react-developer",
            subject: "hello",
            body: "body",
            read: false,
          }]}
        />
      </TooltipProvider>,
    );

    await user.click(screen.getByRole("button", { name: /delete message from coordinator/i }));

    await waitFor(() => {
      expect(mockDeleteMessage).toHaveBeenCalledWith("test-workspace", 1);
    });

    expect(mockTriggerWorkspacePageRefresh).not.toHaveBeenCalled();
  });

  it("keeps the delete action disabled until the request settles", async () => {
    const user = userEvent.setup();
    const pendingDelete = deferredValue<{ deleted: boolean }>();
    mockDeleteMessage.mockImplementationOnce(() => pendingDelete.promise);

    render(
      <TooltipProvider>
        <MessagesTab
          workspaceName="test-workspace"
          messages={[{
            id: 1,
            fromAgent: "coordinator",
            toAgent: "react-developer",
            subject: "hello",
            body: "body",
            read: false,
          }]}
        />
      </TooltipProvider>,
    );

    const button = screen.getByRole("button", { name: /delete message from coordinator/i });
    await user.click(button);

    await waitFor(() => {
      expect(button.hasAttribute("disabled")).toBe(true);
    });

    await act(async () => {
      pendingDelete.resolve({ deleted: true });
      await pendingDelete.promise;
    });

    await waitFor(() => {
      expect(button.hasAttribute("disabled")).toBe(false);
    });
  });
});
