interface ErrorNoticeProps {
  message: string;
  onRetry?: () => void;
}

/** A centered error message with an optional retry button. */
export function ErrorNotice({ message, onRetry }: ErrorNoticeProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-sm text-gh-text-secondary">
      <p>{message}</p>
      {onRetry && (
        <button
          type="button"
          className="rounded-md border border-gh-border bg-gh-bg-secondary px-3 py-1.5 text-sm cursor-pointer text-gh-text transition-colors duration-150 hover:bg-gh-bg-hover"
          onClick={onRetry}
        >
          Retry
        </button>
      )}
    </div>
  );
}
