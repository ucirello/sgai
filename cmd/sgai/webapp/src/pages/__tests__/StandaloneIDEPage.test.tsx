import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { render, screen, cleanup } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";
import * as workspacePageStateModule from "@/lib/workspace-page-state";
import { StandaloneIDEPage } from "../StandaloneIDEPage";

type FetchStatus = "idle" | "fetching" | "error";

interface MockIDEState {
  available: boolean;
  running: boolean;
  reason?: string;
  accessPath?: string;
  proxyPath?: string;
}

interface MockWorkspace {
  name: string;
  dir: string;
  title: string;
  ide?: MockIDEState;
  isRoot: boolean;
  isFork: boolean;
}

let mockWorkspace: MockWorkspace | null = null;
let mockFetchStatus: FetchStatus = "idle";

function createMockWorkspace(overrides: Partial<MockWorkspace> = {}): MockWorkspace {
  return {
    name: "test-workspace",
    dir: "/path/to/test-workspace",
    title: "Test Workspace Title",
    isRoot: false,
    isFork: false,
    ide: {
      available: true,
      running: false,
      accessPath: "/api/v1/workspaces/test-workspace/ide/access",
      proxyPath: "/workspaces/test-workspace/ide-proxy/",
    },
    ...overrides,
  };
}

function renderStandalonePage(workspaceName = "test-workspace") {
  return render(
    <MemoryRouter initialEntries={[`/workspaces/${workspaceName}/ide`]}>
      <Routes>
        <Route path="/workspaces/:name/ide" element={<StandaloneIDEPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  mock.restore();
  cleanup();
});

describe("StandaloneIDEPage", () => {
  beforeEach(() => {
    mockWorkspace = null;
    mockFetchStatus = "idle";

    spyOn(workspacePageStateModule, "useWorkspacePageState").mockImplementation(() => ({
      workspace: mockWorkspace as ReturnType<typeof workspacePageStateModule.useWorkspacePageState>["workspace"],
      fetchStatus: mockFetchStatus,
      lastFetchedAt: mockWorkspace ? Date.now() : null,
    }));
  });

  describe("direct deep-link entry", () => {
    it("shows loading skeleton when store is at idle with null workspace", () => {
      mockWorkspace = null;
      mockFetchStatus = "idle";

      renderStandalonePage();

      expect(screen.getByRole("banner")).toBeTruthy();
      expect(screen.queryByRole("main")).toBeNull();
      expect(screen.queryByRole("link")).toBeNull();
      expect(screen.queryByText(/IDE —/)).toBeNull();
      expect(screen.queryByText("Failed to load workspace.")).toBeNull();
      expect(screen.queryByText("No workspace specified.")).toBeNull();
    });

    it("shows loading skeleton when fetch is in progress", () => {
      mockWorkspace = null;
      mockFetchStatus = "fetching";

      renderStandalonePage();

      expect(screen.getByRole("banner")).toBeTruthy();
      expect(screen.queryByRole("main")).toBeNull();
      expect(screen.queryByRole("link")).toBeNull();
      expect(screen.queryByText(/IDE —/)).toBeNull();
      expect(screen.queryByText("Failed to load workspace.")).toBeNull();
    });

    it("transitions from loading to IDE view when workspace loads", () => {
      mockWorkspace = null;
      mockFetchStatus = "idle";

      const { rerender } = renderStandalonePage();

      expect(screen.getByRole("banner")).toBeTruthy();
      expect(screen.queryByRole("main")).toBeNull();
      expect(screen.queryByText(/IDE —/)).toBeNull();

      mockWorkspace = createMockWorkspace();
      mockFetchStatus = "idle";

      rerender(
        <MemoryRouter initialEntries={["/workspaces/test-workspace/ide"]}>
          <Routes>
            <Route path="/workspaces/:name/ide" element={<StandaloneIDEPage />} />
          </Routes>
        </MemoryRouter>,
      );

      expect(screen.getByRole("banner")).toBeTruthy();
      expect(screen.getByRole("main")).toBeTruthy();
      expect(screen.getByText(/IDE — Test Workspace Title/)).toBeTruthy();
      expect(screen.getByRole("link", { name: /Back to workspace/ })).toBeTruthy();
    });
  });

  describe("error states", () => {
    it("shows error when fetch fails", () => {
      mockWorkspace = null;
      mockFetchStatus = "error";

      renderStandalonePage();

      expect(screen.getByText("Failed to load workspace.")).toBeTruthy();
      expect(screen.queryByRole("main")).toBeNull();
      expect(screen.queryByRole("link", { name: /Back to workspace/ })).toBeNull();
    });
  });

  describe("success state", () => {
    it("renders back-to-workspace link", () => {
      mockWorkspace = createMockWorkspace();
      mockFetchStatus = "idle";

      renderStandalonePage();

      const backLink = screen.getByRole("link", { name: /Back to workspace/ });
      expect(backLink).toBeTruthy();
      expect(backLink.getAttribute("href")).toContain("/workspaces/test-workspace");
    });

    it("displays workspace title in header", () => {
      mockWorkspace = createMockWorkspace({ title: "My Project" });
      mockFetchStatus = "idle";

      renderStandalonePage();

      expect(screen.getByText(/IDE — My Project/)).toBeTruthy();
    });

    it("uses workspace name when title is empty", () => {
      mockWorkspace = createMockWorkspace({ title: "", name: "my-workspace" });
      mockFetchStatus = "idle";

      render(
        <MemoryRouter initialEntries={["/workspaces/my-workspace/ide"]}>
          <Routes>
            <Route path="/workspaces/:name/ide" element={<StandaloneIDEPage />} />
          </Routes>
        </MemoryRouter>,
      );

      expect(screen.getByText(/IDE — my-workspace/)).toBeTruthy();
    });

    it("renders full-page IDE layout with header and main", () => {
      mockWorkspace = createMockWorkspace();
      mockFetchStatus = "idle";

      renderStandalonePage();

      expect(screen.getByRole("banner")).toBeTruthy();
      expect(screen.getByRole("main")).toBeTruthy();
    });
  });
});
