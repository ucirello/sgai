import { useState, useCallback, useRef, useEffect } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import type { ApiWorkspaceIDEState } from "@/types";

type IDELoadState = "idle" | "starting" | "ready" | "error";

interface IDETabProps {
  workspaceName: string;
  ideState?: ApiWorkspaceIDEState;
  fullPage?: boolean;
}

function IDETabSkeleton({ fullPage }: { fullPage?: boolean }) {
  const heightClass = fullPage ? "h-[calc(100vh-3rem)]" : "h-[60vh]";
  return (
    <div className="space-y-4">
      <Skeleton className="h-8 w-48" />
      <Skeleton className={`${heightClass} w-full rounded-xl`} />
    </div>
  );
}

function IDEUnavailableNotice({ reason }: { reason: string }) {
  return (
    <Alert>
      <AlertDescription>
        <strong>IDE unavailable</strong>
        {reason ? `: ${reason}` : ""}
        <p className="mt-2 text-sm text-muted-foreground">
          The embedded IDE requires Docker to be installed and running on the
          host. Please ensure Docker is available and try again.
        </p>
      </AlertDescription>
    </Alert>
  );
}

function IDEErrorNotice({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <Alert className="border-destructive/50">
      <AlertDescription>
        <p className="text-destructive">
          <strong>IDE error:</strong> {message}
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-3"
          onClick={onRetry}
        >
          Retry
        </Button>
      </AlertDescription>
    </Alert>
  );
}

function IDEStartingNotice({ fullPage }: { fullPage?: boolean }) {
  const heightClass = fullPage ? "h-[calc(100vh-3rem)]" : "h-[60vh]";
  return (
    <div className="space-y-4" aria-busy="true">
      <p className="text-sm text-muted-foreground">
        Starting IDE session…
      </p>
      <Skeleton className={`${heightClass} w-full rounded-xl`} />
    </div>
  );
}

export function IDETab({ workspaceName, ideState, fullPage }: IDETabProps): JSX.Element {
  const [loadState, setLoadState] = useState<IDELoadState>("idle");
  const [errorMessage, setErrorMessage] = useState("");
  const [proxyPath, setProxyPath] = useState<string | null>(null);
  const prevWorkspaceRef = useRef(workspaceName);
  const autoStartedRef = useRef(false);

  const ideAvailable = ideState?.available ?? false;
  const ideAccessPath = ideState?.accessPath;

  useEffect(() => {
    if (prevWorkspaceRef.current !== workspaceName) {
      prevWorkspaceRef.current = workspaceName;
      setLoadState("idle");
      setErrorMessage("");
      setProxyPath(null);
      autoStartedRef.current = false;
    }
  }, [workspaceName]);

  const requestIDEAccess = useCallback(async () => {
    setLoadState("starting");
    setErrorMessage("");
    try {
      const response = await api.workspaces.ideAccess(
        ideAccessPath ?? workspaceName,
      );
      if (response.proxyPath) {
        setProxyPath(response.proxyPath);
        setLoadState("ready");
      } else {
        setLoadState("error");
        setErrorMessage("IDE session started but no proxy path was returned.");
      }
    } catch (err) {
      setLoadState("error");
      setErrorMessage(
        err instanceof Error ? err.message : "Failed to start IDE session",
      );
    }
  }, [ideAccessPath, workspaceName]);

  const handleRetry = useCallback(() => {
    void requestIDEAccess();
  }, [requestIDEAccess]);

  useEffect(() => {
    if (autoStartedRef.current) return;
    if (!ideState || !ideAvailable) return;
    if (loadState !== "idle") return;

    autoStartedRef.current = true;
    void requestIDEAccess();
  }, [ideState, ideAvailable, loadState, requestIDEAccess]);

  if (!ideState) {
    return <IDETabSkeleton fullPage={fullPage} />;
  }

  if (!ideAvailable) {
    return <IDEUnavailableNotice reason={ideState.reason ?? ""} />;
  }

  if (loadState === "error") {
    return <IDEErrorNotice message={errorMessage} onRetry={handleRetry} />;
  }

  if (loadState === "ready" && proxyPath) {
    const iframeHeight = fullPage ? "calc(100vh - 3rem)" : "75vh";
    const iframeRounding = fullPage ? "" : "rounded-xl";
    return (
      <div className="space-y-0">
        <iframe
          src={proxyPath}
          title={`IDE for ${workspaceName}`}
          className={`w-full border ${iframeRounding}`}
          style={{ height: iframeHeight }}
          referrerPolicy="no-referrer"
          allow="clipboard-read; clipboard-write"
        />
      </div>
    );
  }

  return <IDEStartingNotice fullPage={fullPage} />;
}
