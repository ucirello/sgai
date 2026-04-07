import { useEffect, useMemo } from "react";
import { Outlet, useNavigate } from "react-router";
import { ConnectionStatusBanner } from "./components/ConnectionStatusBanner";
import { NotificationPermissionBar } from "./components/NotificationPermissionBar";
import { TooltipProvider } from "./components/ui/tooltip";
import { useNotifications } from "./hooks/useNotifications";
import { useFactoryState } from "./lib/factory-state";
import { getFirstPendingResponseTarget } from "./lib/pending-response";
import { buildWorkspacePathWithDisambiguator, buildWorkspaceRouteDisambiguators } from "./lib/workspace-identity";

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) {
    return false;
  }

  return Boolean(
    target.closest("input, textarea, select, .monaco-editor")
    || target.closest("[contenteditable]:not([contenteditable='false'])"),
  );
}

function PendingResponseShortcut() {
  const navigate = useNavigate();
  const { workspaces } = useFactoryState();
  const workspaceRouteDisambiguators = useMemo(
    () => buildWorkspaceRouteDisambiguators(workspaces),
    [workspaces],
  );
  const firstPendingResponseTarget = useMemo(
    () => getFirstPendingResponseTarget(workspaces),
    [workspaces],
  );

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented
        || event.key.toLowerCase() !== "i"
        || !(event.metaKey || event.ctrlKey)
        || event.altKey
        || event.shiftKey
        || isEditableTarget(event.target)
        || !firstPendingResponseTarget
      ) {
        return;
      }

      event.preventDefault();
      navigate(buildWorkspacePathWithDisambiguator(firstPendingResponseTarget, workspaceRouteDisambiguators, "respond"));
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [firstPendingResponseTarget, navigate, workspaceRouteDisambiguators]);

  return null;
}

export function App() {
  useNotifications();

  return (
    <TooltipProvider>
      <NotificationPermissionBar />
      <ConnectionStatusBanner />
      <PendingResponseShortcut />
      <div className="min-h-screen bg-background text-foreground">
        <main className="p-4">
          <Outlet />
        </main>
      </div>
    </TooltipProvider>
  );
}
