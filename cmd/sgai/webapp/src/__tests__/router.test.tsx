import { describe, it, expect } from "bun:test";
import { Navigate } from "react-router";
import { router } from "../router";

describe("router", () => {
  it("redirects /workspaces/new to the external attachment flow", () => {
    const rootRoute = router.routes[0];
    const newWorkspaceRoute = rootRoute.children?.find((route) => route.path === "workspaces/new");

    expect(newWorkspaceRoute).toBeTruthy();
    expect(newWorkspaceRoute?.element?.type).toBe(Navigate);
    expect(newWorkspaceRoute?.element?.props.to).toBe("/workspaces/attach");
    expect(newWorkspaceRoute?.element?.props.replace).toBe(true);
  });
});
