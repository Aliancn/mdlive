import { describe, it, expect, beforeEach } from "vitest";
import { loadRecentByGroup, pushRecentFile, RECENT_LIMIT, RECENT_STORAGE_KEY } from "./recent";

beforeEach(() => {
  localStorage.clear();
});

describe("loadRecentByGroup", () => {
  it("returns an empty map when nothing is stored", () => {
    expect(loadRecentByGroup()).toEqual({});
  });

  it("loads a stored map", () => {
    localStorage.setItem(RECENT_STORAGE_KEY, JSON.stringify({ default: ["a", "b"] }));
    expect(loadRecentByGroup()).toEqual({ default: ["a", "b"] });
  });

  it("falls back to an empty map on invalid JSON", () => {
    localStorage.setItem(RECENT_STORAGE_KEY, "{not json");
    expect(loadRecentByGroup()).toEqual({});
  });

  it("falls back to an empty map on non-object JSON", () => {
    localStorage.setItem(RECENT_STORAGE_KEY, JSON.stringify(["a", "b"]));
    expect(loadRecentByGroup()).toEqual({});
  });
});

describe("pushRecentFile", () => {
  it("creates a per-group list on first sight", () => {
    expect(pushRecentFile({}, "default", "a")).toEqual({ default: ["a"] });
  });

  it("moves an existing id to the front", () => {
    const prev = { default: ["a", "b", "c"] };
    expect(pushRecentFile(prev, "default", "c")).toEqual({ default: ["c", "a", "b"] });
  });

  it("drops duplicates instead of listing a file twice", () => {
    const prev = { default: ["a", "b"] };
    expect(pushRecentFile(prev, "default", "b")).toEqual({ default: ["b", "a"] });
  });

  it("caps the list length", () => {
    const prev = { default: ["1", "2", "3", "4", "5"] };
    expect(pushRecentFile(prev, "default", "6")).toEqual({
      default: ["6", "1", "2", "3", "4"],
    });
    expect(pushRecentFile(prev, "default", "6")!.default).toHaveLength(RECENT_LIMIT);
  });

  it("returns null when the head is unchanged", () => {
    const prev = { default: ["a", "b"] };
    expect(pushRecentFile(prev, "default", "a")).toBeNull();
  });

  it("keeps other groups untouched", () => {
    const prev = { default: ["a"], docs: ["x"] };
    expect(pushRecentFile(prev, "docs", "y")).toEqual({ default: ["a"], docs: ["y", "x"] });
  });
});
