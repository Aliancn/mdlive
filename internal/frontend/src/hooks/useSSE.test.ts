import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useSSE } from "./useSSE";

type Listener = (e: MessageEvent) => void;

class MockEventSource {
  listeners: Record<string, Listener[]> = {};
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  addEventListener(type: string, listener: Listener) {
    if (!this.listeners[type]) this.listeners[type] = [];
    this.listeners[type].push(listener);
  }

  close() {
    this.closed = true;
  }

  emit(type: string, data: string) {
    for (const listener of this.listeners[type] ?? []) {
      listener({ data } as MessageEvent);
    }
  }

  simulateOpen() {
    this.onopen?.();
  }
}

let instances: MockEventSource[] = [];
const originalLocation = window.location;

beforeEach(() => {
  instances = [];
  vi.stubGlobal(
    "EventSource",
    vi.fn().mockImplementation(function () {
      const es = new MockEventSource();
      instances.push(es);
      return es;
    }),
  );
  Object.defineProperty(window, "location", {
    value: { ...originalLocation, reload: vi.fn() },
    writable: true,
    configurable: true,
  });
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  Object.defineProperty(window, "location", {
    value: originalLocation,
    writable: true,
    configurable: true,
  });
});

describe("useSSE started event", () => {
  it("does not reload on first connection", () => {
    renderHook(() => useSSE({ onUpdate: vi.fn() }));

    const es = instances[0];
    es.emit("started", JSON.stringify({ pid: 1000 }));

    expect(window.location.reload).not.toHaveBeenCalled();
  });

  it("does not reload on reconnect with same PID", () => {
    vi.useFakeTimers();
    renderHook(() => useSSE({ onUpdate: vi.fn() }));

    // First connection
    const es1 = instances[0];
    es1.emit("started", JSON.stringify({ pid: 1000 }));

    // Simulate disconnect and reconnect
    es1.onerror?.();
    vi.advanceTimersByTime(1000);

    // Second connection with same PID
    const es2 = instances[1];
    es2.emit("started", JSON.stringify({ pid: 1000 }));

    expect(window.location.reload).not.toHaveBeenCalled();
  });

  it("reloads on reconnect with different PID", () => {
    vi.useFakeTimers();
    renderHook(() => useSSE({ onUpdate: vi.fn() }));

    // First connection
    const es1 = instances[0];
    es1.emit("started", JSON.stringify({ pid: 1000 }));

    // Simulate disconnect and reconnect
    es1.onerror?.();
    vi.advanceTimersByTime(1000);

    // Second connection with different PID
    const es2 = instances[1];
    es2.emit("started", JSON.stringify({ pid: 2000 }));

    expect(window.location.reload).toHaveBeenCalledOnce();
  });
});

describe("useSSE watcher event", () => {
  it("invokes onWatcherStatus with a valid payload", () => {
    const onWatcherStatus = vi.fn();
    renderHook(() => useSSE({ onUpdate: vi.fn(), onWatcherStatus }));

    instances[0].emit(
      "watcher",
      JSON.stringify({
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
      }),
    );

    expect(onWatcherStatus).toHaveBeenCalledOnce();
    const status = onWatcherStatus.mock.calls[0][0];
    expect(status.status).toBe("degraded");
    expect(status.failed).toBe(2);
  });

  it("ignores a malformed watcher payload", () => {
    const onWatcherStatus = vi.fn();
    renderHook(() => useSSE({ onUpdate: vi.fn(), onWatcherStatus }));

    instances[0].emit("watcher", "not json{");

    expect(onWatcherStatus).not.toHaveBeenCalled();
  });

  it("ignores a payload without a status string", () => {
    const onWatcherStatus = vi.fn();
    renderHook(() => useSSE({ onUpdate: vi.fn(), onWatcherStatus }));

    instances[0].emit("watcher", JSON.stringify({ failed: 1 }));

    expect(onWatcherStatus).not.toHaveBeenCalled();
  });

  it("does not throw when onWatcherStatus is omitted", () => {
    renderHook(() => useSSE({ onUpdate: vi.fn() }));

    expect(() => instances[0].emit("watcher", JSON.stringify({ status: "healthy" }))).not.toThrow();
  });
});
