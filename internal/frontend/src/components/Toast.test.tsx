import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import { ToastProvider, useToast } from "./Toast";

function ToastButton({ message }: { message: string }) {
  const showToast = useToast();
  return <button onClick={() => showToast(message)}>Show toast</button>;
}

describe("Toast", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows a toast and dismisses it after the delay", () => {
    vi.useFakeTimers();
    render(
      <ToastProvider>
        <ToastButton message="Something failed" />
      </ToastProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Show toast" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Something failed");
    act(() => {
      vi.advanceTimersByTime(4000);
    });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows the latest toasts and drops the oldest beyond three", () => {
    vi.useFakeTimers();
    render(
      <ToastProvider>
        <ToastButton message="one" />
        <ToastButton message="two" />
        <ToastButton message="three" />
        <ToastButton message="four" />
      </ToastProvider>,
    );
    const buttons = screen.getAllByRole("button", { name: "Show toast" });
    for (const button of buttons) {
      fireEvent.click(button);
    }
    const alerts = screen.getAllByRole("alert");
    expect(alerts).toHaveLength(3);
    expect(alerts[0]).toHaveTextContent("two");
    expect(alerts[2]).toHaveTextContent("four");
  });

  it("is a no-op outside a provider", () => {
    render(<ToastButton message="ignored" />);
    fireEvent.click(screen.getByRole("button", { name: "Show toast" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
