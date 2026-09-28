import { codeToHtml, bundledLanguages } from "shiki";

// Both themes are rendered in one pass: the inline colors are the light
// theme, and the dark colors ride along as --shiki-dark CSS variables that
// app.css applies under [data-theme="dark"]. Toggling the theme therefore
// needs no re-highlight.
const SHIKI_THEMES = { light: "github-light", dark: "github-dark" } as const;

// `text`/`plaintext` are special-cased by codeToHtml at runtime and are not
// keys of bundledLanguages; every other supported language and alias is.
export function isSupportedLanguage(lang: string): boolean {
  return lang === "text" || lang === "plaintext" || lang in bundledLanguages;
}

export const HIGHLIGHT_CACHE_MAX_ENTRIES = 100;

// Code blocks re-highlight on every mount (file switch, raw-view toggle,
// search re-render), and re-highlighting is the slowest part of that render.
// Memoize by (lang, code); Map iterates in insertion order, so the oldest
// entry is evicted first.
const cache = new Map<string, string>();
const inflight = new Map<string, Promise<string>>();

function cacheKey(lang: string, code: string): string {
  return `${lang}\n${code}`;
}

function store(key: string, html: string): void {
  cache.set(key, html);
  while (cache.size > HIGHLIGHT_CACHE_MAX_ENTRIES) {
    const oldest = cache.keys().next().value;
    if (oldest == null) break;
    cache.delete(oldest);
  }
}

/** Highlight `code` with both themes in one pass, memoized by (lang, code). */
export function highlightCode(code: string, lang: string): Promise<string> {
  const key = cacheKey(lang, code);
  const hit = cache.get(key);
  if (hit != null) return Promise.resolve(hit);

  const pending = inflight.get(key);
  if (pending != null) return pending;

  const promise = codeToHtml(code, { lang, themes: SHIKI_THEMES })
    .then((html) => {
      inflight.delete(key);
      store(key, html);
      return html;
    })
    .catch((err) => {
      inflight.delete(key);
      throw err;
    });
  inflight.set(key, promise);
  return promise;
}
