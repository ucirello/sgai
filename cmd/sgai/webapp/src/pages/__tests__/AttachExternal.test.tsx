import { describe, it, expect, beforeEach, mock } from "bun:test";
import { render, screen, waitFor, fireEvent, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { AttachExternal } from "../AttachExternal";

function deferredValue<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const mockNavigate = mock(() => {});
const mockAttach = mock(() => Promise.resolve({ name: "attached-repo", hasGoal: true }));
const mockBrowseDirectories = mock(() => Promise.resolve({ entries: [] }));
const mockTriggerFactoryRefresh = mock(() => {});

mock.module("react-router", () => ({
  ...require("react-router"),
  useNavigate: () => mockNavigate,
}));

mock.module("@/lib/api", () => ({
  api: {
    workspaces: {
      attach: mockAttach,
    },
    browse: {
      directories: mockBrowseDirectories,
    },
  },
  ApiError: class ApiError extends Error {
    constructor(public status: number, message: string) {
      super(message);
      this.name = "ApiError";
    }
  },
}));

mock.module("@/lib/factory-state", () => ({
  triggerFactoryRefresh: mockTriggerFactoryRefresh,
}));

function renderAttachExternal() {
  return render(
    <MemoryRouter>
      <AttachExternal />
    </MemoryRouter>,
  );
}

describe("AttachExternal", () => {
  beforeEach(() => {
    cleanup();
    mockNavigate.mockClear();
    mockAttach.mockClear();
    mockBrowseDirectories.mockClear();
    mockTriggerFactoryRefresh.mockClear();
  });

  it("renders the external-only repository attachment copy", async () => {
    renderAttachExternal();

    expect(screen.getByRole("heading", { name: "Attach External Repository" })).toBeTruthy();
    expect(screen.getByText(/accepts repositories only through this external attachment flow/i)).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Repository Path" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Attach External Repository" })).toBeTruthy();
    expect(screen.queryByText("Attach Workspace")).toBeNull();
  });

  it("attaches an external repository and routes to GOAL editing when the repo already has one", async () => {
    const user = userEvent.setup();
    renderAttachExternal();

    const input = screen.getByRole("combobox", { name: "Repository Path" });
    fireEvent.change(input, { target: { value: "/Users/you/src/repo" } });

    await user.click(screen.getByRole("button", { name: "Attach External Repository" }));

    await waitFor(() => {
      expect(mockAttach).toHaveBeenCalledWith("/Users/you/src/repo");
      expect(mockTriggerFactoryRefresh).toHaveBeenCalled();
      expect(mockNavigate).toHaveBeenCalledWith("/workspaces/attached-repo/goal/edit");
    });
  });

  it("ignores stale directory suggestions that resolve after a newer request", async () => {
    const oldRequest = deferredValue<{ entries: Array<{ name: string; path: string }> }>();
    const newRequest = deferredValue<{ entries: Array<{ name: string; path: string }> }>();

    mockBrowseDirectories.mockImplementation((value: string) => {
      if (value === "/Users/you/src/old") {
        return oldRequest.promise;
      }
      if (value === "/Users/you/src/new") {
        return newRequest.promise;
      }
      return Promise.resolve({ entries: [] });
    });

    renderAttachExternal();

    const input = screen.getByRole("combobox", { name: "Repository Path" });
    fireEvent.change(input, { target: { value: "/Users/you/src/old" } });

    await waitFor(() => {
      expect(mockBrowseDirectories).toHaveBeenCalledWith("/Users/you/src/old");
    });

    fireEvent.change(input, { target: { value: "/Users/you/src/new" } });

    await waitFor(() => {
      expect(mockBrowseDirectories).toHaveBeenCalledWith("/Users/you/src/new");
    });

    newRequest.resolve({
      entries: [{ name: "new-repo", path: "/Users/you/src/new-repo" }],
    });

    await waitFor(() => {
      expect(screen.getByRole("option", { name: /new-repo/i })).toBeTruthy();
    });

    oldRequest.resolve({
      entries: [{ name: "old-repo", path: "/Users/you/src/old-repo" }],
    });

    await waitFor(() => {
      expect(screen.getByRole("option", { name: /new-repo/i })).toBeTruthy();
    });

    expect(screen.queryByRole("option", { name: /old-repo/i })).toBeNull();
  });
});
