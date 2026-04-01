import { Suspense, lazy } from "react";
import { createBrowserRouter, Navigate } from "react-router";
import { App } from "./App";
import { AppRouteErrorBoundary } from "./components/AppRouteErrorBoundary";
import { NotYetAvailable } from "./components/NotYetAvailable";
import { Skeleton } from "./components/ui/skeleton";

const Dashboard = lazy(() =>
  import("./pages/Dashboard").then((m) => ({ default: m.Dashboard })),
);
const EmptyState = lazy(() =>
  import("./pages/EmptyState").then((m) => ({ default: m.EmptyState })),
);
const WorkspaceDetail = lazy(() =>
  import("./pages/WorkspaceDetail").then((m) => ({ default: m.WorkspaceDetail })),
);
const AgentList = lazy(() =>
  import("./pages/AgentList").then((m) => ({ default: m.AgentList })),
);
const SkillList = lazy(() =>
  import("./pages/SkillList").then((m) => ({ default: m.SkillList })),
);
const SkillDetail = lazy(() =>
  import("./pages/SkillDetail").then((m) => ({ default: m.SkillDetail })),
);
const SnippetList = lazy(() =>
  import("./pages/SnippetList").then((m) => ({ default: m.SnippetList })),
);
const SnippetDetail = lazy(() =>
  import("./pages/SnippetDetail").then((m) => ({ default: m.SnippetDetail })),
);
const ResponseMultiChoice = lazy(() =>
  import("./pages/ResponseMultiChoice").then((m) => ({ default: m.ResponseMultiChoice })),
);
const AttachExternal = lazy(() =>
  import("./pages/AttachExternal").then((m) => ({ default: m.AttachExternal })),
);
const EditGoal = lazy(() =>
  import("./pages/EditGoal").then((m) => ({ default: m.EditGoal })),
);
const AdhocOutput = lazy(() =>
  import("./pages/AdhocOutput").then((m) => ({ default: m.AdhocOutput })),
);
const StandaloneIDEPage = lazy(() =>
  import("./pages/StandaloneIDEPage").then((m) => ({ default: m.StandaloneIDEPage })),
);

function PageSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-8 w-48" />
      <div className="grid grid-cols-[repeat(auto-fit,minmax(300px,1fr))] gap-4">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-32 rounded-xl" />
        ))}
      </div>
    </div>
  );
}

function DashboardSkeleton() {
  return (
    <div className="flex flex-col md:flex-row gap-0 h-[calc(100vh-4rem)]">
      <aside className="w-full md:w-[280px] border-b md:border-b-0 md:border-r p-2 space-y-2">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-8 w-full rounded" />
        ))}
      </aside>
      <main className="flex-1 pt-4 md:pt-0 md:pl-4">
        <Skeleton className="h-8 w-48 mb-4" />
        <Skeleton className="h-32 w-full rounded-xl" />
      </main>
    </div>
  );
}

function withSuspense(Component: React.ComponentType) {
  return (
    <Suspense fallback={<PageSkeleton />}>
      <Component />
    </Suspense>
  );
}

function withDashboardSuspense(Component: React.ComponentType) {
  return (
    <Suspense fallback={<DashboardSkeleton />}>
      <Component />
    </Suspense>
  );
}

function createRouteErrorElement() {
  return <AppRouteErrorBoundary />;
}

export const router = createBrowserRouter([
  {
    path: "/workspaces/:name/ide",
    element: withSuspense(StandaloneIDEPage),
    errorElement: createRouteErrorElement(),
  },
  {
    path: "/",
    element: <App />,
    errorElement: createRouteErrorElement(),
    children: [
      {
        index: true,
        element: withDashboardSuspense(DashboardWithEmpty),
        errorElement: createRouteErrorElement(),
      },
      {
        path: "trees",
        element: <Navigate to="/" replace />,
      },
      {
        path: "workspaces/new",
        element: <Navigate to="/workspaces/attach" replace />,
      },
      {
        path: "workspaces/attach",
        element: withSuspense(AttachExternal),
      },
      {
        path: "workspaces/:name/agents",
        element: withSuspense(AgentList),
      },
      {
        path: "workspaces/:name/skills",
        element: withSuspense(SkillList),
      },
      {
        path: "workspaces/:name/skills/*",
        element: withSuspense(SkillDetail),
      },
      {
        path: "workspaces/:name/snippets",
        element: withSuspense(SnippetList),
      },
      {
        path: "workspaces/:name/snippets/:lang/:fileName",
        element: withSuspense(SnippetDetail),
      },
      {
        path: "workspaces/:name/goal/edit",
        element: withSuspense(EditGoal),
      },
      {
        path: "workspaces/:name/goal",
        element: withSuspense(EditGoal),
      },
      {
        path: "workspaces/:name/adhoc",
        element: withSuspense(AdhocOutput),
      },

      {
        path: "workspaces/:name/*",
        element: withDashboardSuspense(DashboardWithWorkspace),
        errorElement: createRouteErrorElement(),
      },
      {
        path: "workspaces/:name",
        element: withDashboardSuspense(DashboardWithWorkspace),
        errorElement: createRouteErrorElement(),
      },
      {
        path: "workspaces/:name/respond",
        element: withSuspense(ResponseMultiChoice),
      },
      {
        path: "*",
        element: <NotYetAvailable pageName="Page" />,
      },
    ],
  },
]);

function DashboardWithEmpty() {
  return <Dashboard><EmptyState /></Dashboard>;
}

function DashboardWithWorkspace() {
  return <Dashboard><WorkspaceDetail /></Dashboard>;
}
