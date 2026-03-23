import { AlertTriangle, RotateCcw } from "lucide-react";
import { Link, isRouteErrorResponse, useRouteError } from "react-router";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

function getErrorStatusCopy(error: unknown): string | null {
  if (!isRouteErrorResponse(error)) {
    return null;
  }

  if (error.status === 404) {
    return "The requested workspace page could not be found.";
  }

  const statusText = error.statusText?.trim();
  return statusText ? `Request failed with ${error.status} ${statusText}.` : `Request failed with status ${error.status}.`;
}

export function AppRouteErrorBoundary() {
  const error = useRouteError();
  const errorStatusCopy = getErrorStatusCopy(error);

  return (
    <section
      aria-labelledby="route-error-title"
      className="mx-auto flex min-h-[calc(100vh-8rem)] w-full max-w-2xl flex-col justify-center gap-6 px-4 py-10"
    >
      <div className="space-y-2">
        <h1 id="route-error-title" className="text-3xl font-semibold tracking-tight">
          Something went wrong
        </h1>
        <p className="text-sm text-muted-foreground">
          The workspace view hit an unexpected error. Reload the page or return to the dashboard to recover safely.
        </p>
      </div>

      <Alert className="border-destructive/40 bg-destructive/5">
        <AlertTriangle className="h-4 w-4" aria-hidden="true" />
        <AlertTitle>Workspace view error</AlertTitle>
        <AlertDescription>
          {errorStatusCopy ?? "The app stopped rendering this screen before it could finish loading."}
        </AlertDescription>
      </Alert>

      <div className="flex flex-wrap gap-3">
        <Button asChild>
          <Link to="/">Back to dashboard</Link>
        </Button>
        <Button type="button" variant="outline" onClick={() => window.location.reload()}>
          <RotateCcw className="h-4 w-4" />
          Reload page
        </Button>
      </div>
    </section>
  );
}
