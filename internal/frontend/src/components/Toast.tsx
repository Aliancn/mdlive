import { createContext, useCallback, useContext, useRef, useState } from "react";

type ShowToast = (message: string) => void;

const ToastContext = createContext<ShowToast>(() => {});

/** Show a transient error message. No-op outside a ToastProvider. */
export function useToast(): ShowToast {
  return useContext(ToastContext);
}

const TOAST_DURATION_MS = 4000;

interface ToastEntry {
  id: number;
  message: string;
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<ToastEntry[]>([]);
  const nextId = useRef(0);

  const showToast = useCallback((message: string) => {
    const id = ++nextId.current;
    setToasts((prev) => [...prev.slice(-2), { id, message }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, TOAST_DURATION_MS);
  }, []);

  return (
    <ToastContext.Provider value={showToast}>
      {children}
      <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            role="alert"
            className="max-w-96 rounded-md border border-gh-danger-border bg-gh-danger-bg px-3 py-2 text-sm text-gh-danger-text shadow-lg"
          >
            {toast.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
