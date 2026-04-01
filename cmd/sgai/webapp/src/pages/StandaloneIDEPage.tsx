import { useParams, Link } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useWorkspacePageState } from "@/lib/workspace-page-state";
import { IDETab } from "@/pages/tabs/IDETab";
import { buildWorkspacePath } from "@/lib/workspace-identity";

function StandaloneIDELoading() {
  return (
    <div className="flex flex-col h-screen bg-background text-foreground">
      <header className="flex items-center gap-3 px-4 py-2 border-b shrink-0">
        <Skeleton className="h-5 w-32" />
        <Skeleton className="h-5 w-24" />
      </header>
      <div className="flex-1 p-4">
        <Skeleton className="h-full w-full" />
      </div>
    </div>
  );
}

function StandaloneIDEError({ message }: { message: string }) {
  return (
    <div className="flex flex-col h-screen bg-background text-foreground">
      <header className="flex items-center gap-3 px-4 py-2 border-b shrink-0">
        <span className="text-sm font-medium">IDE</span>
      </header>
      <div className="flex-1 p-4 flex items-center justify-center">
        <Alert className="max-w-md">
          <AlertDescription>{message}</AlertDescription>
        </Alert>
      </div>
    </div>
  );
}

export function StandaloneIDEPage(): JSX.Element {
  const { name } = useParams<{ name: string }>();
  const workspaceName = name ?? "";

  const { workspace, fetchStatus } = useWorkspacePageState(workspaceName);

  if (!workspaceName) {
    return <StandaloneIDEError message="No workspace specified." />;
  }

  if (!workspace) {
    if (fetchStatus === "error") {
      return <StandaloneIDEError message="Failed to load workspace." />;
    }
    return <StandaloneIDELoading />;
  }

  const workspaceLink = buildWorkspacePath(workspace, "progress");

  return (
    <TooltipProvider>
      <div className="flex flex-col h-screen bg-background text-foreground">
        <header className="flex items-center gap-3 px-4 py-2 border-b shrink-0">
          <Link
            to={workspaceLink}
            className="text-sm text-muted-foreground hover:text-foreground no-underline transition-colors"
          >
            ← Back to workspace
          </Link>
          <span className="text-sm font-medium truncate" title={workspace.name}>
            IDE — {workspace.title || workspace.name}
          </span>
        </header>
        <main className="flex-1 overflow-hidden px-2 py-2">
          <IDETab
            workspaceName={workspaceName}
            ideState={workspace.ide}
            fullPage
          />
        </main>
      </div>
    </TooltipProvider>
  );
}
