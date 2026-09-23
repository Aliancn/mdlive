import { useEffect, useLayoutEffect, useRef } from "react";
import type { WatcherStatus } from "./useApi";

interface SSECallbacks {
  onUpdate: () => void;
  onFileChanged?: (fileId: string) => void;
  /** Receives the watcher health on connect and on every status change. */
  onWatcherStatus?: (status: WatcherStatus) => void;
}

export function useSSE(callbacks: SSECallbacks) {
  const callbacksRef = useRef(callbacks);
  useLayoutEffect(() => {
    callbacksRef.current = callbacks;
  });

  useEffect(() => {
    let disposed = false;
    let es: EventSource | null = null;
    let retryDelay = 1000;
    const maxRetryDelay = 30000;
    let serverPid: number | null = null;

    function connect() {
      if (disposed) return;

      es = new EventSource("/_/events");

      es.addEventListener("started", (e) => {
        try {
          const data = JSON.parse(e.data);
          if (typeof data.pid !== "number") return;
          if (serverPid !== null && data.pid !== serverPid) {
            window.location.reload();
            return;
          }
          serverPid = data.pid;
        } catch {
          // ignore
        }
      });

      es.addEventListener("update", () => {
        callbacksRef.current.onUpdate();
      });

      es.addEventListener("file-changed", (e) => {
        try {
          const data = JSON.parse(e.data);
          callbacksRef.current.onFileChanged?.(data.id);
        } catch {
          // ignore malformed data
        }
      });

      es.addEventListener("watcher", (e) => {
        try {
          const data = JSON.parse(e.data) as WatcherStatus;
          if (typeof data?.status !== "string") return;
          callbacksRef.current.onWatcherStatus?.(data);
        } catch {
          // ignore malformed data
        }
      });

      es.onopen = () => {
        retryDelay = 1000;
      };

      es.onerror = () => {
        es?.close();
        if (!disposed) {
          setTimeout(connect, retryDelay);
          retryDelay = Math.min(retryDelay * 2, maxRetryDelay);
        }
      };
    }

    connect();

    return () => {
      disposed = true;
      es?.close();
    };
  }, []);
}
