import { describe, it, expect, spyOn } from "bun:test";
import { render, screen } from "@testing-library/react";
import { Navigate, RouterProvider, createMemoryRouter } from "react-router";
import { router } from "../router";

function findAppRoute() {
  return router.routes.find((route) => route.path === "/")!;
}

describe("router", () => {
  it("redirects /workspaces/new to the external attachment flow", () => {
    const rootRoute = findAppRoute();
    const newWorkspaceRoute = rootRoute.children?.find((route) => route.path === "workspaces/new");

    expect(newWorkspaceRoute).toBeTruthy();
    expect(newWorkspaceRoute?.element?.type).toBe(Navigate);
    expect(newWorkspaceRoute?.element?.props.to).toBe("/workspaces/attach");
    expect(newWorkspaceRoute?.element?.props.replace).toBe(true);
  });

  it("keeps workspace detail on the catch-all workspace route", () => {
    const rootRoute = findAppRoute();
    const workspaceRoute = rootRoute.children?.find((route) => route.path === "workspaces/:name/*");

    expect(workspaceRoute).toBeTruthy();
  });

  it("defines custom error boundaries for the app shell and workspace routes", () => {
    const rootRoute = findAppRoute();
    const workspaceRoute = rootRoute.children?.find((route) => route.path === "workspaces/:name/*");

    expect(rootRoute.errorElement).toBeTruthy();
    expect(workspaceRoute?.errorElement).toBeTruthy();
  });

  it("defines standalone IDE page route outside the app shell", () => {
    const ideRoute = router.routes.find((route) => route.path === "/workspaces/:name/ide");

    expect(ideRoute).toBeTruthy();
    expect(ideRoute?.errorElement).toBeTruthy();
  });

  it("renders a product-safe recovery UI instead of the default developer error page", async () => {
    const rootRoute = findAppRoute();
    const consoleErrorSpy = spyOn(console, "error").mockImplementation(() => {});

    function Boom() {
      throw new Error("boom");
    }

    const memoryRouter = createMemoryRouter([
      {
        path: "/",
        errorElement: rootRoute.errorElement,
        children: [
          {
            path: "boom",
            element: <Boom />,
          },
        ],
      },
    ], {
      initialEntries: ["/boom"],
    });

    try {
      render(<RouterProvider router={memoryRouter} />);

      expect(await screen.findByRole("heading", { name: "Something went wrong" })).toBeTruthy();
      expect(screen.getByText(/The workspace view hit an unexpected error/i)).toBeTruthy();
      expect(screen.queryByText("Unexpected Application Error!")).toBeNull();
    } finally {
      consoleErrorSpy.mockRestore();
    }
  });
});
