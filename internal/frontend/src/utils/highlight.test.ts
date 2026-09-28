import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("shiki", () => ({
  codeToHtml: vi.fn(
    async (code: string, opts: { lang: string }) => `<pre data-lang="${opts.lang}">${code}</pre>`,
  ),
  bundledLanguages: { js: {}, yaml: {} },
}));

import { codeToHtml } from "shiki";
import { highlightCode, isSupportedLanguage, HIGHLIGHT_CACHE_MAX_ENTRIES } from "./highlight";

beforeEach(() => {
  vi.clearAllMocks();
});

describe("isSupportedLanguage", () => {
  it("accepts bundled languages and aliases", () => {
    expect(isSupportedLanguage("js")).toBe(true);
    expect(isSupportedLanguage("yaml")).toBe(true);
  });

  it("accepts the special-cased plain text labels", () => {
    expect(isSupportedLanguage("text")).toBe(true);
    expect(isSupportedLanguage("plaintext")).toBe(true);
  });

  it("rejects unknown languages", () => {
    expect(isSupportedLanguage("notalang")).toBe(false);
  });
});

describe("highlightCode", () => {
  it("highlights and returns the html", async () => {
    await expect(highlightCode("let x", "js")).resolves.toBe('<pre data-lang="js">let x</pre>');
    expect(codeToHtml).toHaveBeenCalledTimes(1);
  });

  it("serves repeat calls from the cache", async () => {
    await highlightCode("cached", "js");
    await highlightCode("cached", "js");
    expect(codeToHtml).toHaveBeenCalledTimes(1);
  });

  it("caches per (lang, code)", async () => {
    await highlightCode("same code", "js");
    await highlightCode("same code", "yaml");
    await highlightCode("other code", "js");
    expect(codeToHtml).toHaveBeenCalledTimes(3);
  });

  it("dedupes concurrent highlights of the same code", async () => {
    let resolveFn!: (html: string) => void;
    vi.mocked(codeToHtml).mockImplementationOnce(
      () =>
        new Promise<string>((res) => {
          resolveFn = res;
        }),
    );
    const p1 = highlightCode("shared", "js");
    const p2 = highlightCode("shared", "js");
    resolveFn("<pre>shared</pre>");
    await Promise.all([p1, p2]);
    expect(codeToHtml).toHaveBeenCalledTimes(1);
  });

  it("re-attempts after a failed highlight", async () => {
    vi.mocked(codeToHtml).mockRejectedValueOnce(new Error("boom"));
    await expect(highlightCode("flaky", "js")).rejects.toThrow("boom");
    await expect(highlightCode("flaky", "js")).resolves.toBe('<pre data-lang="js">flaky</pre>');
    expect(codeToHtml).toHaveBeenCalledTimes(2);
  });

  it("evicts the oldest entry beyond the cap", async () => {
    for (let i = 0; i <= HIGHLIGHT_CACHE_MAX_ENTRIES; i++) {
      await highlightCode(`entry ${i}`, "evict");
    }
    // Storing entry 100 pushed entry 0 (the oldest) past the cap; entry 100
    // (the newest) survives.
    vi.mocked(codeToHtml).mockClear();
    await highlightCode("entry 0", "evict");
    expect(codeToHtml).toHaveBeenCalledTimes(1);
    await highlightCode("entry 100", "evict");
    expect(codeToHtml).toHaveBeenCalledTimes(1);
  });
});
