import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { WatcherBanner } from "./WatcherBanner";
import type { WatcherStatus } from "../hooks/useApi";

vi.mock("../hooks/useApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../hooks/useApi")>();
  return { ...actual, retryWatchers: vi.fn() };
});

import { retryWatchers } from "../hooks/useApi";

beforeEach(() => {
  vi.clearAllMocks();
});

const degraded: WatcherStatus = {
  status: "degraded",
  roots: 1,
  dirWatches: 0,
  fileWatches: 0,
  failed: 2,
  pendingRetries: 2,
  circuitOpen: false,
  totalFailures: 2,
  totalRecovered: 0,
  lastError: "FSEventStreamStart failed",
};

describe("WatcherBanner", () => {
  it("renders nothing when status is null", () => {
    const { container } = render(<WatcherBanner status={null} onClose={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when the watcher is healthy", () => {
    const { container } = render(
      <WatcherBanner status={{ ...degraded, status: "healthy", failed: 0 }} onClose={() => {}} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the degraded message with the failure count and last error", () => {
    render(<WatcherBanner status={degraded} onClose={() => {}} />);
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByText(/Live-reload is degraded\./)).toBeInTheDocument();
    expect(screen.getByText(/2 file watcher registrations failed/)).toBeInTheDocument();
    expect(screen.getByText(/FSEventStreamStart failed/)).toBeInTheDocument();
  });

  it("uses singular wording for a single failure", () => {
    render(<WatcherBanner status={{ ...degraded, failed: 1 }} onClose={() => {}} />);
    expect(screen.getByText(/1 file watcher registration failed/)).toBeInTheDocument();
  });

  it("calls onClose when Dismiss is clicked", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<WatcherBanner status={degraded} onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("toggles the How to fix list", async () => {
    const user = userEvent.setup();
    render(<WatcherBanner status={degraded} onClose={() => {}} />);

    expect(screen.queryByText(/\.mlignore/)).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "How to fix" }));
    expect(screen.getByText(/\.mlignore/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "How to fix" }));
    expect(screen.queryByText(/\.mlignore/)).not.toBeInTheDocument();
  });

  it("retries and closes when the server reports healthy", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    vi.mocked(retryWatchers).mockResolvedValue({ ...degraded, status: "healthy", failed: 0 });
    render(<WatcherBanner status={degraded} onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "Retry now" }));
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
    expect(retryWatchers).toHaveBeenCalledOnce();
  });

  it("stays open when the retry does not recover", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    vi.mocked(retryWatchers).mockResolvedValue(degraded);
    render(<WatcherBanner status={degraded} onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "Retry now" }));
    await waitFor(() => expect(retryWatchers).toHaveBeenCalledOnce());
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toBeInTheDocument();
  });

  it("stays open when the retry request fails", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    vi.mocked(retryWatchers).mockRejectedValue(new Error("boom"));
    render(<WatcherBanner status={degraded} onClose={onClose} />);

    await user.click(screen.getByRole("button", { name: "Retry now" }));
    await waitFor(() => expect(retryWatchers).toHaveBeenCalledOnce());
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toBeInTheDocument();
  });
});
