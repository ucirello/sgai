import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { render, screen, waitFor, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import { api } from "@/lib/api";
import { IDETab } from "../IDETab";
import type { ApiWorkspaceIDEState } from "@/types";

beforeEach(() => {
  document.body.style.pointerEvents = "auto";
});

const mockIDEAccess = mock(() =>
  Promise.resolve({
    available: true,
    running: true,
    accessPath: "/api/v1/workspaces/workspace-id/ide/access",
    proxyPath: "/workspaces/workspace-id/ide-proxy/",
    reused: false,
  }),
);

function renderIDETab(
  props: {
    workspaceName?: string;
    ideState?: ApiWorkspaceIDEState;
  } = {},
) {
  const defaultProps = {
    workspaceName: "test-workspace",
    ideState: {
      available: true,
      running: false,
      accessPath: "/api/v1/workspaces/workspace-id/ide/access",
      proxyPath: "/workspaces/workspace-id/ide-proxy/",
    } as ApiWorkspaceIDEState,
    ...props,
  };

  return render(
    <MemoryRouter>
      <TooltipProvider>
        <IDETab {...defaultProps} />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  mock.restore();
  cleanup();
});

describe("IDETab", () => {
  beforeEach(() => {
    mockIDEAccess.mockClear();
    spyOn(api.workspaces, "ideAccess").mockImplementation((...args) =>
      mockIDEAccess(...args),
    );
  });

  describe("loading state", () => {
    it("shows skeleton when ideState is undefined", () => {
      renderIDETab({ ideState: undefined });

      const skeletons = document.querySelectorAll("[class*='animate-pulse'], [data-slot='skeleton']");
      expect(skeletons.length).toBeGreaterThan(0);
    });
  });

  describe("unavailable state", () => {
    it("shows unavailable notice when Docker is not available", () => {
      renderIDETab({
        ideState: {
          available: false,
          running: false,
          reason: "docker unavailable",
        },
      });

      expect(screen.getByText("IDE unavailable")).toBeTruthy();
      expect(screen.getByText(/docker unavailable/)).toBeTruthy();
      expect(
        screen.getByText(/requires Docker to be installed/),
      ).toBeTruthy();
    });

    it("shows unavailable notice without reason when reason is empty", () => {
      renderIDETab({
        ideState: {
          available: false,
          running: false,
        },
      });

      expect(screen.getByText("IDE unavailable")).toBeTruthy();
    });
  });

  describe("auto-start on mount", () => {
    it("automatically starts IDE when available but not running", async () => {
      renderIDETab({
        ideState: {
          available: true,
          running: false,
          accessPath: "/api/v1/workspaces/workspace-id/ide/access",
          proxyPath: "/workspaces/workspace-id/ide-proxy/",
        },
      });

      await waitFor(() => {
        expect(mockIDEAccess).toHaveBeenCalledWith(
          "/api/v1/workspaces/workspace-id/ide/access",
        );
      });

      await waitFor(() => {
        const iframe = document.querySelector("iframe");
        expect(iframe).toBeTruthy();
        expect(iframe?.getAttribute("src")).toBe(
          "/workspaces/workspace-id/ide-proxy/",
        );
        expect(iframe?.getAttribute("title")).toBe(
          "IDE for test-workspace",
        );
      });
    });

    it("requests access even when session is already running with proxyPath", async () => {
      renderIDETab({
        ideState: {
          available: true,
          running: true,
          accessPath: "/api/v1/workspaces/workspace-id/ide/access",
          proxyPath: "/workspaces/workspace-id/ide-proxy/",
        },
      });

      await waitFor(() => {
        expect(mockIDEAccess).toHaveBeenCalledWith(
          "/api/v1/workspaces/workspace-id/ide/access",
        );
      });

      await waitFor(() => {
        const iframe = document.querySelector("iframe");
        expect(iframe).toBeTruthy();
        expect(iframe?.getAttribute("src")).toBe(
          "/workspaces/workspace-id/ide-proxy/",
        );
      });
    });

    it("mints fresh proxy authorization on direct route entry for running session", async () => {
      renderIDETab({
        ideState: {
          available: true,
          running: true,
          accessPath: "/api/v1/workspaces/workspace-id/ide/access",
          proxyPath: "/workspaces/workspace-id/ide-proxy/",
        },
      });

      await waitFor(() => {
        expect(mockIDEAccess).toHaveBeenCalledTimes(1);
      });

      await waitFor(() => {
        const iframe = document.querySelector("iframe");
        expect(iframe).toBeTruthy();
        expect(iframe?.getAttribute("src")).toBe(
          "/workspaces/workspace-id/ide-proxy/",
        );
      });
    });

    it("calls ideAccess when running but proxyPath is unavailable", async () => {
      renderIDETab({
        ideState: {
          available: true,
          running: true,
          accessPath: "/api/v1/workspaces/workspace-id/ide/access",
        },
      });

      await waitFor(() => {
        expect(mockIDEAccess).toHaveBeenCalledWith(
          "/api/v1/workspaces/workspace-id/ide/access",
        );
      });

      await waitFor(() => {
        const iframe = document.querySelector("iframe");
        expect(iframe).toBeTruthy();
      });
    });

    it("shows starting state during auto-start", async () => {
      let resolveAccess!: (value: unknown) => void;
      mockIDEAccess.mockImplementation(
        () =>
          new Promise((resolve) => {
            resolveAccess = resolve;
          }),
      );

      renderIDETab({
        ideState: {
          available: true,
          running: false,
          proxyPath: "/workspaces/workspace-id/ide-proxy/",
        },
      });

      await waitFor(() => {
        expect(screen.getByText(/Starting IDE session/)).toBeTruthy();
      });

      resolveAccess({
        available: true,
        running: true,
        proxyPath: "/workspaces/workspace-id/ide-proxy/",
      });

      await waitFor(() => {
        const iframe = document.querySelector("iframe");
        expect(iframe).toBeTruthy();
      });
    });

    it("shows error state when auto-start fails", async () => {
      mockIDEAccess.mockImplementation(() =>
        Promise.reject(new Error("Docker daemon not responding")),
      );

      renderIDETab({
        ideState: {
          available: true,
          running: false,
          proxyPath: "/workspaces/workspace-id/ide-proxy/",
        },
      });

      await waitFor(() => {
        expect(screen.getByText(/Docker daemon not responding/)).toBeTruthy();
        expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
      });
    });
  });

  describe("retry after auto-start failure", () => {
    it("retries IDE access on retry button click", async () => {
      const user = userEvent.setup();
      mockIDEAccess
        .mockImplementationOnce(() =>
          Promise.reject(new Error("First failure")),
        )
        .mockImplementation(() =>
          Promise.resolve({
            available: true,
            running: true,
            proxyPath: "/workspaces/workspace-id/ide-proxy/",
          }),
        );

      renderIDETab({
        ideState: {
          available: true,
          running: false,
          proxyPath: "/workspaces/workspace-id/ide-proxy/",
        },
      });

      await waitFor(() => {
        expect(screen.getByText(/First failure/)).toBeTruthy();
      });

      await user.click(screen.getByRole("button", { name: "Retry" }));

      await waitFor(() => {
        const iframe = document.querySelector("iframe");
        expect(iframe).toBeTruthy();
      });

      expect(mockIDEAccess).toHaveBeenCalledTimes(2);
    });
  });

  describe("iframe attributes", () => {
    it("renders iframe without sandbox and with referrer policy", async () => {
      renderIDETab({
        ideState: {
          available: true,
          running: true,
          proxyPath: "/workspaces/workspace-id/ide-proxy/",
        },
      });

      await waitFor(() => {
        const iframe = document.querySelector("iframe");
        expect(iframe).toBeTruthy();
        expect(iframe?.hasAttribute("sandbox")).toBe(false);
        expect(iframe?.getAttribute("referrerPolicy")).toBe("no-referrer");
        expect(iframe?.getAttribute("allow")).toBe(
          "clipboard-read; clipboard-write",
        );
      });
    });
  });

  describe("workspace switching", () => {
    it("resets state when workspace changes", async () => {
      const { rerender } = renderIDETab({
        workspaceName: "workspace-a",
        ideState: {
          available: true,
          running: false,
          accessPath: "/api/v1/workspaces/workspace-a-id/ide/access",
          proxyPath: "/workspaces/workspace-a-id/ide-proxy/",
        },
      });

      await waitFor(() => {
        expect(mockIDEAccess).toHaveBeenCalled();
      });

      rerender(
        <MemoryRouter>
          <TooltipProvider>
            <IDETab
              workspaceName="workspace-b"
              ideState={{
                available: false,
                running: false,
                reason: "docker unavailable",
              }}
            />
          </TooltipProvider>
        </MemoryRouter>,
      );

      expect(screen.getByText("IDE unavailable")).toBeTruthy();
    });
  });

  describe("error alert accessibility", () => {
    it("marks error container with role alert", async () => {
      mockIDEAccess.mockImplementation(() =>
        Promise.reject(new Error("Connection refused")),
      );

      renderIDETab({
        ideState: {
          available: true,
          running: false,
          proxyPath: "/workspaces/workspace-id/ide-proxy/",
        },
      });

      await waitFor(() => {
        const alerts = screen.getAllByRole("alert");
        const errorAlert = alerts.find((el) =>
          el.textContent?.includes("Connection refused"),
        );
        expect(errorAlert).toBeTruthy();
      });
    });
  });
});
