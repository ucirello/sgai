import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { act, fireEvent, render, screen, waitFor, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import * as ReactRouter from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import * as factoryStateModule from "@/lib/factory-state";
import { api } from "@/lib/api";
import * as markdownEditorModule from "@/components/MarkdownEditor";
import { InlineForkEditor } from "../InlineForkEditor";

function deferredValue<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  document.body.style.pointerEvents = "auto";
  sessionStorage.clear();
});

const mockFork = mock(() => Promise.resolve({ name: "new-fork", dir: "/path/to/new-fork" }));
const mockForkTemplate = mock(() => Promise.resolve({ content: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal\n\nDescribe your task" }));
const mockTriggerFactoryRefresh = mock(() => {});
const mockNavigate = mock(() => {});

const mockMarkdownEditor = ({ value, onChange, disabled, placeholder, onSubmitShortcut, ariaLabel }: {
    value: string;
    onChange: (v: string | undefined) => void;
    disabled: boolean;
    placeholder?: string;
    onSubmitShortcut?: () => void;
    ariaLabel?: string;
  }) => (
     <div data-testid="markdown-editor">
       <textarea
         aria-label={ariaLabel}
         value={value}
         onChange={(e) => onChange(e.target.value)}
         onKeyDown={(event) => {
           if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
            event.preventDefault();
            onSubmitShortcut?.();
          }
        }}
        disabled={disabled}
        data-testid="fork-editor-textarea"
        placeholder={placeholder}
      />
    </div>
  );

async function renderInlineForkEditor(workspaceName = "test-workspace") {
  let view: ReturnType<typeof render> | undefined;
  await act(async () => {
    view = render(
      <MemoryRouter>
        <TooltipProvider>
          <InlineForkEditor workspaceName={workspaceName} />
        </TooltipProvider>
      </MemoryRouter>
    );
    await Promise.resolve();
  });
  return view!;
}

afterEach(() => {
  mock.restore();
  cleanup();
});

describe("InlineForkEditor", () => {
  beforeEach(() => {
    mockFork.mockClear();
    mockForkTemplate.mockClear();
    mockTriggerFactoryRefresh.mockClear();
    mockNavigate.mockClear();
    mockFork.mockImplementation(() => Promise.resolve({ name: "new-fork", dir: "/path/to/new-fork" }));
    mockForkTemplate.mockImplementation(() => Promise.resolve({ content: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal\n\nDescribe your task" }));

    spyOn(ReactRouter, "useNavigate").mockImplementation(() => mockNavigate);
    spyOn(factoryStateModule, "triggerFactoryRefresh").mockImplementation(() => mockTriggerFactoryRefresh());
    spyOn(api.workspaces, "fork").mockImplementation((...args) => mockFork(...args));
    spyOn(api.workspaces, "forkTemplate").mockImplementation((...args) => mockForkTemplate(...args));
    spyOn(markdownEditorModule, "MarkdownEditor").mockImplementation((...args) => mockMarkdownEditor(...args));
  });

  function mockSessionStorage(overrides: Partial<Storage>): () => void {
    const original = window.sessionStorage;
    const replacement = {
      ...original,
      length: original.length,
      clear: original.clear.bind(original),
      key: original.key.bind(original),
      getItem: original.getItem.bind(original),
      setItem: original.setItem.bind(original),
      removeItem: original.removeItem.bind(original),
      ...overrides,
    } as Storage;

    Object.defineProperty(window, "sessionStorage", {
      configurable: true,
      value: replacement,
    });

    return () => {
      Object.defineProperty(window, "sessionStorage", {
        configurable: true,
        value: original,
      });
    };
  }

  describe("rendering", () => {
    it("shows body-only fork guidance", async () => {
      await renderInlineForkEditor();

      expect(screen.getByText("New Task")).toBeTruthy();
      expect(screen.getByText(/Write the task body for your new fork/)).toBeTruthy();
      expect(screen.getByText(/Root frontmatter and the inherited title are copied automatically/)).toBeTruthy();
    });

    it("labels the editable field as task body", async () => {
      await renderInlineForkEditor();

      expect(screen.getByText("Task body")).toBeTruthy();
      expect(screen.getByRole("textbox", { name: "Task body" })).toBeTruthy();
    });

    it("shows Create Fork button", async () => {
      await renderInlineForkEditor();

      expect(screen.getByText("Create Fork")).toBeTruthy();
    });

    it("shows markdown editor", async () => {
      await renderInlineForkEditor();

      expect(screen.getByTestId("markdown-editor")).toBeTruthy();
    });

    it("loads fork template on mount", async () => {
      await renderInlineForkEditor();

      await waitFor(() => {
        expect(mockForkTemplate).toHaveBeenCalledWith("test-workspace");
      });
    });

    it("populates the editor with the template body only", async () => {
      await renderInlineForkEditor();

      await waitFor(() => {
        const textarea = screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement;
        expect(textarea.value).toBe("# Goal\n\nDescribe your task");
      });
    });

    it("shows a visible error and retry action when the template load fails", async () => {
      const user = userEvent.setup();
      mockForkTemplate.mockImplementationOnce(() => Promise.reject(new Error("Template unavailable")));

      await renderInlineForkEditor();

      await waitFor(() => {
        expect(screen.getByText("Template unavailable")).toBeTruthy();
      });

      const retryButton = screen.getByRole("button", { name: "Retry template load" });
      expect(retryButton).toBeTruthy();

      await user.click(retryButton);

      await waitFor(() => {
        expect(mockForkTemplate).toHaveBeenCalledTimes(2);
      });
    });

    it("clears stale content and errors immediately when switching workspaces", async () => {
      const user = userEvent.setup();
      const pendingTemplate = deferredValue<{ content: string }>();

      mockForkTemplate.mockImplementation((workspaceName: string) => {
        if (workspaceName === "test-workspace") {
          return Promise.resolve({ content: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# First Goal\n\nFirst workspace task" });
        }
        if (workspaceName === "next-workspace") {
          return pendingTemplate.promise;
        }
        return Promise.resolve({ content: "" });
      });
      mockFork.mockImplementationOnce(() => Promise.reject(new Error("Fork failed")));

      const view = await renderInlineForkEditor();

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# First Goal\n\nFirst workspace task");
      });

      await user.click(screen.getByRole("button", { name: "Create Fork" }));

      await waitFor(() => {
        expect(screen.getByText("Failed to create fork")).toBeTruthy();
      });

      await user.clear(screen.getByTestId("fork-editor-textarea"));

      const createForkButton = screen.getByRole("button", { name: "Create Fork" });
      expect(createForkButton.hasAttribute("disabled")).toBe(true);

      view.rerender(
        <MemoryRouter>
          <TooltipProvider>
            <InlineForkEditor workspaceName="next-workspace" />
          </TooltipProvider>
        </MemoryRouter>
      );

      await waitFor(() => {
        expect(mockForkTemplate).toHaveBeenCalledWith("next-workspace");
      });

      expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("");
      expect(screen.queryByText("Failed to create fork")).toBeNull();
      expect(screen.queryByText("Please write a goal description")).toBeNull();
      expect(screen.getByRole("button", { name: "Create Fork" }).hasAttribute("disabled")).toBe(true);

      await act(async () => {
        pendingTemplate.resolve({ content: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Second Goal\n\nSecond workspace task" });
        await pendingTemplate.promise;
      });

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# Second Goal\n\nSecond workspace task");
      });
      expect(screen.getByRole("button", { name: "Create Fork" }).hasAttribute("disabled")).toBe(false);
    });

    it("replaces prior content with an empty template response", async () => {
      mockForkTemplate.mockImplementation((workspaceName: string) => {
        if (workspaceName === "test-workspace") {
          return Promise.resolve({ content: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# First Goal\n\nFirst workspace task" });
        }
        return Promise.resolve({ content: "" });
      });

      const view = await renderInlineForkEditor();

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# First Goal\n\nFirst workspace task");
      });

      view.rerender(
        <MemoryRouter>
          <TooltipProvider>
            <InlineForkEditor workspaceName="empty-workspace" />
          </TooltipProvider>
        </MemoryRouter>
      );

      await waitFor(() => {
        expect(mockForkTemplate).toHaveBeenCalledWith("empty-workspace");
      });

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("");
      });
      expect(screen.getByRole("button", { name: "Create Fork" }).hasAttribute("disabled")).toBe(true);
    });

    it("shows a loading state and disables editing while the template request is pending", async () => {
      const pendingTemplate = deferredValue<{ content: string }>();
      mockForkTemplate.mockImplementationOnce(() => pendingTemplate.promise);

      await renderInlineForkEditor();

      expect(screen.getByRole("status").textContent).toContain("Loading fork template...");
      expect(screen.getByTestId("fork-editor-textarea").hasAttribute("disabled")).toBe(true);
      expect(screen.getByRole("button", { name: "Create Fork" }).hasAttribute("disabled")).toBe(true);

      await act(async () => {
        pendingTemplate.resolve({ content: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal\n\nLoaded template" });
        await pendingTemplate.promise;
      });

      await waitFor(() => {
        expect(screen.queryByRole("status")).toBeNull();
        expect(screen.getByTestId("fork-editor-textarea").hasAttribute("disabled")).toBe(false);
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# Goal\n\nLoaded template");
      });
    });

    it("preserves an existing draft when retrying a failed template load", async () => {
      const user = userEvent.setup();
      const pendingRetry = deferredValue<{ content: string }>();

      mockForkTemplate
        .mockImplementationOnce(() => Promise.reject(new Error("Template unavailable")))
        .mockImplementationOnce(() => pendingRetry.promise);

      await renderInlineForkEditor();

      await waitFor(() => {
        expect(screen.getByText("Template unavailable")).toBeTruthy();
      });

      await user.type(screen.getByTestId("fork-editor-textarea"), "My saved draft");

      await user.click(screen.getByRole("button", { name: "Retry template load" }));

      expect(screen.getByRole("status").textContent).toContain("Loading fork template...");
      expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("My saved draft");

      await act(async () => {
        pendingRetry.resolve({ content: "---\nflow: |\n  \"a\" -> \"b\"\n---\n# Goal\n\nServer template" });
        await pendingRetry.promise;
      });

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("My saved draft");
      });
    });

    it("shows a visible error when restoring a stored draft fails", async () => {
      const restoreSessionStorage = mockSessionStorage({
        getItem: mock(() => {
          throw new Error("Storage read blocked");
        }) as Storage["getItem"],
      });

      try {
        await renderInlineForkEditor();

        await waitFor(() => {
          expect(screen.getByText("Draft persistence is unavailable while restoring your draft: Storage read blocked")).toBeTruthy();
        });

        await waitFor(() => {
          expect(mockForkTemplate).toHaveBeenCalledWith("test-workspace");
        });
      } finally {
        restoreSessionStorage();
      }
    });

    it("shows a visible error when saving a draft fails", async () => {
      const user = userEvent.setup();
      const restoreSessionStorage = mockSessionStorage({
        setItem: mock(() => {
          throw new Error("Storage write blocked");
        }) as Storage["setItem"],
      });

      try {
        await renderInlineForkEditor();

        await waitFor(() => {
          expect(screen.getByTestId("fork-editor-textarea")).toBeTruthy();
        });

        const textarea = screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement;
        await user.clear(textarea);
        await user.type(textarea, "# Goal\n\nPersist this draft");

        await waitFor(() => {
          expect(screen.getByText("Draft persistence is unavailable while saving your draft: Storage write blocked")).toBeTruthy();
        });
      } finally {
        restoreSessionStorage();
      }
    });

    it("shows a visible error when clearing a draft fails", async () => {
      const user = userEvent.setup();
      const restoreSessionStorage = mockSessionStorage({
        removeItem: mock(() => {
          throw new Error("Storage clear blocked");
        }) as Storage["removeItem"],
      });

      try {
        await renderInlineForkEditor();

        await waitFor(() => {
          expect(screen.getByTestId("fork-editor-textarea")).toBeTruthy();
        });

        const textarea = screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement;
        await user.clear(textarea);

        await waitFor(() => {
          expect(screen.getByText("Draft persistence is unavailable while clearing your draft: Storage clear blocked")).toBeTruthy();
        });
      } finally {
        restoreSessionStorage();
      }
    });
  });

  describe("validation", () => {
    it("disables Create Fork button when body is empty", async () => {
      mockForkTemplate.mockImplementation(() => Promise.resolve({ content: "---\nflow: |\n  \"a\" -> \"b\"\n---\n" }));

      await renderInlineForkEditor();

      await waitFor(() => {
        const button = screen.getByText("Create Fork").closest("button");
        expect(button?.hasAttribute("disabled")).toBe(true);
      });
    });
  });

  describe("fork creation", () => {
    it("calls fork API on submit", async () => {
      const user = userEvent.setup();
      await renderInlineForkEditor();

      await waitFor(() => {
        expect(screen.getByTestId("fork-editor-textarea")).toBeTruthy();
      });

      const button = screen.getByText("Create Fork").closest("button");
      if (button && !button.hasAttribute("disabled")) {
        await user.click(button);

        await waitFor(() => {
          expect(mockFork).toHaveBeenCalledWith("test-workspace", "# Goal\n\nDescribe your task");
        });
      }
    });

    it("triggers factory refresh after successful fork", async () => {
      const user = userEvent.setup();
      await renderInlineForkEditor();

      await waitFor(() => {
        const textarea = screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement;
        expect(textarea.value).toBe("# Goal\n\nDescribe your task");
      });

      const button = screen.getByText("Create Fork").closest("button");
      if (button && !button.hasAttribute("disabled")) {
        await user.click(button);

        await waitFor(() => {
          expect(mockTriggerFactoryRefresh).toHaveBeenCalled();
          expect(mockNavigate).toHaveBeenCalledWith("/workspaces/new-fork/progress");
        });
      }
    });

    it("submits the fork when Ctrl+S is pressed in the editor", async () => {
      const user = userEvent.setup();

      await renderInlineForkEditor();

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# Goal\n\nDescribe your task");
      });

      const textarea = screen.getByTestId("fork-editor-textarea");
      textarea.focus();
      await user.keyboard("{Control>}s{/Control}");

      await waitFor(() => {
        expect(mockFork).toHaveBeenCalledWith("test-workspace", "# Goal\n\nDescribe your task");
        expect(mockNavigate).toHaveBeenCalledWith("/workspaces/new-fork/progress");
      });
    });

    it("submits the fork when Cmd+S is pressed in the editor", async () => {
      await renderInlineForkEditor();

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# Goal\n\nDescribe your task");
      });

      const textarea = screen.getByTestId("fork-editor-textarea");
      textarea.focus();
      fireEvent.keyDown(textarea, { key: "s", metaKey: true });

      await waitFor(() => {
        expect(mockFork).toHaveBeenCalledWith("test-workspace", "# Goal\n\nDescribe your task");
        expect(mockNavigate).toHaveBeenCalledWith("/workspaces/new-fork/progress");
      });
    });

    it("persists the draft to sessionStorage", async () => {
      const user = userEvent.setup();
      await renderInlineForkEditor();

      await waitFor(() => {
        expect(screen.getByTestId("fork-editor-textarea")).toBeTruthy();
      });

      const textarea = screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement;
      await user.clear(textarea);
      await user.type(textarea, "# Goal\n\nPersist this draft");

      await waitFor(() => {
        expect(sessionStorage.getItem("sgai-inline-fork-test-workspace")).toBe("# Goal\n\nPersist this draft");
      });
    });

    it("restores a stored draft instead of reloading the template", async () => {
      sessionStorage.setItem("sgai-inline-fork-test-workspace", "# Goal\n\nRecovered draft");

      await renderInlineForkEditor();

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# Goal\n\nRecovered draft");
      });

      expect(mockForkTemplate).not.toHaveBeenCalled();
    });

    it("round-trips a YAML-looking body draft unchanged", async () => {
      mockForkTemplate.mockImplementation(() => Promise.resolve({ content: "" }));
      const yamlBody = "---\nflow: |\n  \"agent\" -> \"other\"\n---\nFollow this exact body";

      const firstRender = await renderInlineForkEditor();

      await waitFor(() => {
        expect(screen.getByTestId("fork-editor-textarea")).toBeTruthy();
      });

      fireEvent.change(screen.getByTestId("fork-editor-textarea"), {
        target: { value: yamlBody },
      });

      await waitFor(() => {
        expect(sessionStorage.getItem("sgai-inline-fork-test-workspace")).toBe(yamlBody);
      });

      firstRender.unmount();

      await renderInlineForkEditor();

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe(yamlBody);
      });

      expect(mockForkTemplate).toHaveBeenCalledTimes(1);

      fireEvent.click(screen.getByRole("button", { name: "Create Fork" }));

      await waitFor(() => {
        expect(mockFork).toHaveBeenCalledWith("test-workspace", yamlBody);
      });
    });

    it("does not persist untouched template content and refetches it on reopen", async () => {
      const firstRender = await renderInlineForkEditor();

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# Goal\n\nDescribe your task");
      });

      expect(sessionStorage.getItem("sgai-inline-fork-test-workspace")).toBeNull();
      expect(mockForkTemplate).toHaveBeenCalledTimes(1);

      firstRender.unmount();

      await renderInlineForkEditor();

      await waitFor(() => {
        expect(mockForkTemplate).toHaveBeenCalledTimes(2);
      });
      expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# Goal\n\nDescribe your task");
      expect(sessionStorage.getItem("sgai-inline-fork-test-workspace")).toBeNull();
    });

    it("clears sessionStorage after a successful fork", async () => {
      const user = userEvent.setup();
      sessionStorage.setItem("sgai-inline-fork-test-workspace", "# Goal\n\nRecovered draft");

      await renderInlineForkEditor();

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# Goal\n\nRecovered draft");
      });

      await user.click(screen.getByRole("button", { name: "Create Fork" }));

      await waitFor(() => {
        expect(mockFork).toHaveBeenCalledWith("test-workspace", "# Goal\n\nRecovered draft");
        expect(sessionStorage.getItem("sgai-inline-fork-test-workspace")).toBeNull();
      });
    });

    it("keeps fork submission controls disabled until the request settles", async () => {
      const user = userEvent.setup();
      const pendingFork = deferredValue<{ name: string; dir: string }>();
      mockFork.mockImplementationOnce(() => pendingFork.promise);

      await renderInlineForkEditor();

      await waitFor(() => {
        const textarea = screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement;
        expect(textarea.value).toBe("# Goal\n\nDescribe your task");
      });

      const button = screen.getByRole("button", { name: "Create Fork" });
      await user.click(button);

      await waitFor(() => {
        expect(screen.getByRole("button", { name: "Creating Fork..." }).hasAttribute("disabled")).toBe(true);
        expect(screen.getByTestId("fork-editor-textarea").hasAttribute("disabled")).toBe(true);
      });

      await act(async () => {
        pendingFork.resolve({ name: "new-fork", dir: "/path/to/new-fork" });
        await pendingFork.promise;
      });

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith("/workspaces/new-fork/progress");
      });
    });

    it("registers beforeunload protection when the draft is unsaved", async () => {
      const originalAddEventListener = window.addEventListener.bind(window);
      const calls: Array<[string, ...unknown[]]> = [];
      const mockAddEventListener = mock((...args: Parameters<typeof window.addEventListener>) => {
        calls.push([args[0], ...args.slice(1)]);
        return originalAddEventListener(...args);
      });
      window.addEventListener = mockAddEventListener as typeof window.addEventListener;

      try {
        await renderInlineForkEditor();

        await waitFor(() => {
          expect(screen.getByTestId("fork-editor-textarea")).toBeTruthy();
        });

        fireEvent.change(screen.getByTestId("fork-editor-textarea"), {
          target: { value: "# Goal\n\nUnsaved draft" },
        });

        await waitFor(() => {
          const beforeUnloadCall = calls.find(([type]) => type === "beforeunload");
          expect(beforeUnloadCall).toBeTruthy();
        });
      } finally {
        window.addEventListener = originalAddEventListener;
      }
    });

    it("does not block unload until the user edits the template", async () => {
      await renderInlineForkEditor();

      await waitFor(() => {
        expect((screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement).value).toBe("# Goal\n\nDescribe your task");
      });

      const pristineEvent = new Event("beforeunload", { cancelable: true });
      fireEvent(window, pristineEvent);
      expect(pristineEvent.defaultPrevented).toBe(false);
      expect(sessionStorage.getItem("sgai-inline-fork-test-workspace")).toBeNull();

      fireEvent.change(screen.getByTestId("fork-editor-textarea"), {
        target: { value: "# Goal\n\nUnsaved draft" },
      });

      await waitFor(() => {
        expect(sessionStorage.getItem("sgai-inline-fork-test-workspace")).toBe("# Goal\n\nUnsaved draft");
      });

      const editedEvent = new Event("beforeunload", { cancelable: true });
      fireEvent(window, editedEvent);
      expect(editedEvent.defaultPrevented).toBe(true);
    });

    it("shows error when fork creation fails", async () => {
      const user = userEvent.setup();
      mockFork.mockImplementation(() => Promise.reject(new Error("Fork failed")));

      await renderInlineForkEditor();

      await waitFor(() => {
        const textarea = screen.getByTestId("fork-editor-textarea") as HTMLTextAreaElement;
        expect(textarea.value).toBe("# Goal\n\nDescribe your task");
      });

      const button = screen.getByText("Create Fork").closest("button");
      if (button && !button.hasAttribute("disabled")) {
        await user.click(button);

        await waitFor(() => {
          expect(screen.getByText("Failed to create fork")).toBeTruthy();
        });
      }
    });
  });
});
