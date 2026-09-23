import { useCallback, useState } from "react";
import { retryWatchers, type WatcherStatus } from "../hooks/useApi";

interface WatcherBannerProps {
  status: WatcherStatus | null;
  onClose: () => void;
}

/**
 * Banner shown when the server's file watcher is degraded: some watch
 * registrations failed, so new or changed files may not appear until the
 * background retries (or a manual retry) succeed.
 */
export function WatcherBanner({ status, onClose }: WatcherBannerProps) {
  const [expanded, setExpanded] = useState(false);
  const [retrying, setRetrying] = useState(false);

  const handleRetry = useCallback(async () => {
    if (retrying) return;
    setRetrying(true);
    try {
      const next = await retryWatchers();
      if (next.status === "healthy") {
        onClose();
      }
    } catch {
      // The retry endpoint is best-effort; the banner stays until the
      // server reports healthy over SSE.
    } finally {
      setRetrying(false);
    }
  }, [retrying, onClose]);

  if (status == null || status.status !== "degraded") {
    return null;
  }

  const failures = status.failed;
  const lastError = status.lastError ? status.lastError : undefined;

  return (
    <div
      role="alert"
      data-testid="watcher-banner"
      className="shrink-0 flex flex-col gap-1 px-4 py-2 border-b border-gh-attention-border bg-gh-attention-bg text-gh-attention-text text-sm"
    >
      <div className="flex items-center gap-2">
        <svg
          className="size-4 shrink-0 text-gh-attention-icon"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"
          />
        </svg>
        <span className="flex-1">
          <strong>Live-reload is degraded.</strong>{" "}
          {failures === 1
            ? "1 file watcher registration failed"
            : `${failures} file watcher registrations failed`}
          {lastError != null && <> ({lastError})</>}. New or changed files may not appear until it
          recovers.
        </span>
        <button
          type="button"
          className="shrink-0 bg-transparent border border-gh-attention-border rounded-md px-2 py-0.5 cursor-pointer text-gh-attention-text transition-colors duration-150 hover:bg-gh-attention-border/30 disabled:opacity-50"
          onClick={handleRetry}
          disabled={retrying}
        >
          {retrying ? "Retrying…" : "Retry now"}
        </button>
        <button
          type="button"
          className="shrink-0 bg-transparent border border-gh-attention-border rounded-md px-2 py-0.5 cursor-pointer text-gh-attention-text transition-colors duration-150 hover:bg-gh-attention-border/30"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
        >
          How to fix
        </button>
        <button
          type="button"
          className="shrink-0 bg-transparent border border-gh-attention-border rounded-md px-2 py-0.5 cursor-pointer text-gh-attention-text transition-colors duration-150 hover:bg-gh-attention-border/30"
          onClick={onClose}
        >
          Dismiss
        </button>
      </div>
      {expanded && (
        <ul className="list-disc pl-10 text-gh-attention-text/90">
          <li>
            Failed registrations usually mean the OS watch limit was hit. Re-run{" "}
            <code>ml --status</code> to see the current watcher health.
          </li>
          <li>
            Narrow the discovery with a <code>.mlignore</code> file (one glob per line) and re-run{" "}
            <code>ml --reload</code>.
          </li>
          <li>
            Remove stale registrations with <code>ml --unwatch &lt;pattern&gt;</code>, or restart
            the server with <code>ml --restart</code>.
          </li>
        </ul>
      )}
    </div>
  );
}
