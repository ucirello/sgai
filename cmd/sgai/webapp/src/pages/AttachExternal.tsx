import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { api, ApiError } from "@/lib/api";
import { triggerFactoryRefresh, useFactoryState } from "@/lib/factory-state";
import { buildWorkspaceGoalEditPath } from "@/lib/workspace-identity";
import { ArrowLeft, FolderInput, Loader2 } from "lucide-react";
import { Link } from "react-router";
import { cn } from "@/lib/utils";
import type { ApiBrowseDirectoryEntry } from "@/types";

const DEBOUNCE_MS = 300;
const ABSOLUTE_BROWSE_PATH_MESSAGE = "Enter an absolute path to browse directories.";

function isObviouslyAbsolutePath(path: string) {
  return path.startsWith("/") || /^[A-Za-z]:[\\/]/.test(path) || path.startsWith("\\\\");
}

function getPathValidationState(path: string) {
  const trimmedPath = path.trim();
  if (!trimmedPath) {
    return { trimmedPath, validationMessage: null };
  }

  if (isObviouslyAbsolutePath(trimmedPath)) {
    return { trimmedPath, validationMessage: null };
  }

  return {
    trimmedPath,
    validationMessage: ABSOLUTE_BROWSE_PATH_MESSAGE,
  };
}

function getBrowseErrorMessage(error: unknown) {
  if (error instanceof ApiError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim()) {
    return error.message;
  }
  return "Failed to browse directories";
}

export function AttachExternal() {
  const navigate = useNavigate();
  const { workspaces } = useFactoryState();
  const [path, setPath] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [browseError, setBrowseError] = useState<string | null>(null);
  const [suggestions, setSuggestions] = useState<ApiBrowseDirectoryEntry[]>([]);
  const [isFetchingSuggestions, setIsFetchingSuggestions] = useState(false);
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [activeIndex, setActiveIndex] = useState<number>(-1);
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const latestSuggestionsRequestRef = useRef(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const suggestionsRef = useRef<HTMLDivElement>(null);
  const pathValidation = getPathValidationState(path);
  const displayedBrowseError = pathValidation.validationMessage ?? browseError;
  const canSubmit =
    !isSubmitting && Boolean(pathValidation.trimmedPath) && !pathValidation.validationMessage;

  const fetchSuggestions = useCallback(async (currentPath: string) => {
    const validation = getPathValidationState(currentPath);
    latestSuggestionsRequestRef.current += 1;
    const requestId = latestSuggestionsRequestRef.current;

    if (!validation.trimmedPath) {
      setSuggestions([]);
      setShowSuggestions(false);
      setActiveIndex(-1);
      setBrowseError(null);
      setIsFetchingSuggestions(false);
      return;
    }

    if (validation.validationMessage) {
      setSuggestions([]);
      setShowSuggestions(false);
      setActiveIndex(-1);
      setBrowseError(null);
      setIsFetchingSuggestions(false);
      return;
    }

    setIsFetchingSuggestions(true);
    setBrowseError(null);
    try {
      const result = await api.browse.directories(validation.trimmedPath);
      if (requestId !== latestSuggestionsRequestRef.current) {
        return;
      }
      setSuggestions(result.entries ?? []);
      setShowSuggestions((result.entries ?? []).length > 0);
      setActiveIndex(-1);
      setBrowseError(null);
    } catch (err) {
      if (requestId !== latestSuggestionsRequestRef.current) {
        return;
      }
      setSuggestions([]);
      setShowSuggestions(false);
      setActiveIndex(-1);
      setBrowseError(getBrowseErrorMessage(err));
    } finally {
      if (requestId === latestSuggestionsRequestRef.current) {
        setIsFetchingSuggestions(false);
      }
    }
  }, []);

  useEffect(() => {
    if (debounceTimerRef.current !== null) {
      clearTimeout(debounceTimerRef.current);
    }
    debounceTimerRef.current = setTimeout(() => {
      void fetchSuggestions(path);
    }, DEBOUNCE_MS);

    return () => {
      if (debounceTimerRef.current !== null) {
        clearTimeout(debounceTimerRef.current);
      }
    };
  }, [path, fetchSuggestions]);

  const handleSelectSuggestion = useCallback((entry: ApiBrowseDirectoryEntry) => {
    setPath(entry.path);
    setSuggestions([]);
    setShowSuggestions(false);
    setActiveIndex(-1);
    setBrowseError(null);
    inputRef.current?.focus();
  }, []);

  const handlePathChange = useCallback((nextPath: string) => {
    setPath(nextPath);
    setBrowseError(null);
  }, []);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (!showSuggestions || suggestions.length === 0) return;

      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((prev) => (prev < suggestions.length - 1 ? prev + 1 : 0));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((prev) => (prev > 0 ? prev - 1 : suggestions.length - 1));
      } else if (e.key === "Enter" && activeIndex >= 0) {
        e.preventDefault();
        handleSelectSuggestion(suggestions[activeIndex]);
      } else if (e.key === "Escape") {
        setShowSuggestions(false);
        setActiveIndex(-1);
      }
    },
    [showSuggestions, suggestions, activeIndex, handleSelectSuggestion],
  );

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      const validation = getPathValidationState(path);
      if (!validation.trimmedPath || validation.validationMessage || isSubmitting) return;

      setIsSubmitting(true);
      setError(null);
      setBrowseError(null);
      setSuggestions([]);
      setShowSuggestions(false);

      try {
        const result = await api.workspaces.attach(validation.trimmedPath);
        triggerFactoryRefresh();
        navigate(buildWorkspaceGoalEditPath(result, [...workspaces.filter((workspace) => workspace.dir !== result.dir), result]));
      } catch (err) {
        if (err instanceof ApiError) {
          setError(err.message);
        } else {
          setError("Failed to attach workspace");
        }
      } finally {
        setIsSubmitting(false);
      }
    },
    [path, isSubmitting, navigate, workspaces],
  );

  const handleBlur = useCallback((e: React.FocusEvent) => {
    if (suggestionsRef.current?.contains(e.relatedTarget as Node)) {
      return;
    }
    setShowSuggestions(false);
  }, []);

  const handleFocus = useCallback(() => {
    if (suggestions.length > 0) {
      setShowSuggestions(true);
    }
  }, [suggestions.length]);

  const inputDescribedBy = displayedBrowseError
    ? "workspace-path-help workspace-path-browse-error"
    : "workspace-path-help";

  return (
    <div className="max-w-lg mx-auto py-8">
      <Link
        to="/"
        className="text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center gap-1 mb-6"
      >
        <ArrowLeft className="h-3 w-3" />
        Back to Dashboard
      </Link>

      <h1 className="text-2xl font-semibold mb-2">Attach External Repository</h1>

      {error ? (
        <Alert className="mb-4 border-destructive/50 text-destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="workspace-path">Repository Path</Label>
          <div className="relative">
            <Input
              id="workspace-path"
              ref={inputRef}
              value={path}
              onChange={(e) => handlePathChange(e.target.value)}
              onFocus={handleFocus}
              onBlur={handleBlur}
              onKeyDown={handleKeyDown}
              placeholder="/Users/you/src/my-repository"
              autoFocus
              disabled={isSubmitting}
              autoComplete="off"
              role="combobox"
              aria-expanded={showSuggestions}
               aria-autocomplete="list"
               aria-controls="workspace-path-suggestions"
               aria-describedby={inputDescribedBy}
               aria-activedescendant={activeIndex >= 0 ? `suggestion-${activeIndex}` : undefined}
               aria-invalid={displayedBrowseError ? true : undefined}
             />
            {isFetchingSuggestions && (
              <div className="absolute right-3 top-1/2 -translate-y-1/2">
                <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
              </div>
            )}
            {showSuggestions && suggestions.length > 0 && (
              <div
                id="workspace-path-suggestions"
                ref={suggestionsRef}
                className="absolute z-50 mt-1 w-full rounded-md border bg-popover shadow-md"
                role="listbox"
                aria-label="Directory suggestions"
              >
                {suggestions.map((entry, index) => (
                  <button
                    id={`suggestion-${index}`}
                    key={entry.path}
                    type="button"
                    role="option"
                    aria-selected={index === activeIndex}
                    className={cn(
                      "flex w-full items-center gap-2 px-3 py-2 text-sm cursor-pointer text-left",
                      index === activeIndex
                        ? "bg-accent text-accent-foreground"
                        : "hover:bg-accent hover:text-accent-foreground",
                    )}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => handleSelectSuggestion(entry)}
                  >
                    <span className="font-medium">{entry.name}</span>
                    <span className="text-xs text-muted-foreground truncate">{entry.path}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
          <p id="workspace-path-help" className="text-xs text-muted-foreground">
            Enter an absolute path or start typing to browse external repositories already on disk.
          </p>
          {displayedBrowseError ? (
            <Alert
              id="workspace-path-browse-error"
              className="border-destructive/50 py-2 text-destructive"
            >
              <AlertDescription>{displayedBrowseError}</AlertDescription>
            </Alert>
          ) : null}
        </div>

        <Button
          type="submit"
          disabled={!canSubmit}
          className="w-full"
        >
          {isSubmitting ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Attaching...
            </>
          ) : (
            <>
              <FolderInput className="mr-2 h-4 w-4" />
              Attach External Repository
            </>
          )}
        </Button>
      </form>
    </div>
  );
}
