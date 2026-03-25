import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { render, screen, waitFor, fireEvent, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import * as ReactRouter from "react-router";
import * as factoryStateModule from "@/lib/factory-state";
import { api } from "@/lib/api";
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
let mockWorkspaces: Array<{ name: string; dir: string }> = [];

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
    mockWorkspaces = [];
    mockNavigate.mockClear();
    mockAttach.mockClear();
    mockBrowseDirectories.mockClear();
    mockTriggerFactoryRefresh.mockClear();

    spyOn(ReactRouter, "useNavigate").mockImplementation(() => mockNavigate);
    spyOn(factoryStateModule, "useFactoryState").mockImplementation(() => ({
      workspaces: mockWorkspaces,
      fetchStatus: "idle",
      lastFetchedAt: Date.now(),
    }));
    spyOn(factoryStateModule, "triggerFactoryRefresh").mockImplementation(() => mockTriggerFactoryRefresh());
    spyOn(api.workspaces, "attach").mockImplementation((...args) => mockAttach(...args));
    spyOn(api.browse, "directories").mockImplementation((...args) => mockBrowseDirectories(...args));
  });

  afterEach(() => {
    mock.restore();
  });

  it("renders the external-only repository attachment copy", async () => {
    renderAttachExternal();

    expect(screen.getByRole("heading", { name: "Attach External Repository" })).toBeTruthy();
    expect(screen.getByText(/browse external repositories already on disk/i)).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Repository Path" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Attach External Repository" })).toBeTruthy();
    expect(screen.queryByText("Attach Workspace")).toBeNull();
  });

  it("attaches an external repository and routes to GOAL editing", async () => {
    const user = userEvent.setup();
    mockAttach.mockResolvedValueOnce({ name: "attached-repo", dir: "/Users/you/src/attached-repo", hasGoal: false });
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

  it("routes duplicate basename attachments to a unique goal-edit path", async () => {
    const user = userEvent.setup();
    mockWorkspaces = [{ name: "attached-repo", dir: "/Users/you/src/first/attached-repo" }];
    mockAttach.mockResolvedValueOnce({ name: "attached-repo", dir: "/Users/you/src/second/attached-repo", hasGoal: false });
    renderAttachExternal();

    const input = screen.getByRole("combobox", { name: "Repository Path" });
    fireEvent.change(input, { target: { value: "/Users/you/src/second/attached-repo" } });

    await user.click(screen.getByRole("button", { name: "Attach External Repository" }));

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith("/workspaces/second%2Fattached-repo/goal/edit");
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

  it("surfaces browse failures instead of swallowing them", async () => {
    mockBrowseDirectories.mockRejectedValueOnce(
      new Error("directory does not exist: /Users/you/src/missing"),
    );

    renderAttachExternal();

    const input = screen.getByRole("combobox", { name: "Repository Path" });
    fireEvent.change(input, { target: { value: "/Users/you/src/missing" } });

    await waitFor(() => {
      expect(screen.getByText("directory does not exist: /Users/you/src/missing")).toBeTruthy();
    });

    expect(screen.queryByRole("option")).toBeNull();
  });

  it("keeps invalid relative paths non-submittable and shows guidance instead", async () => {
    const user = userEvent.setup();
    renderAttachExternal();

    const input = screen.getByRole("combobox", { name: "Repository Path" });
    fireEvent.change(input, { target: { value: "relative/path" } });

    await waitFor(() => {
      expect(screen.getByText("Enter an absolute path to browse directories.")).toBeTruthy();
    });

    const submitButton = screen.getByRole("button", {
      name: "Attach External Repository",
    }) as HTMLButtonElement;

    expect(submitButton.disabled).toBe(true);

    await user.click(submitButton);

    expect(mockBrowseDirectories).not.toHaveBeenCalled();
    expect(mockAttach).not.toHaveBeenCalled();
    expect(screen.getByText("Enter an absolute path to browse directories.")).toBeTruthy();
  });
});
