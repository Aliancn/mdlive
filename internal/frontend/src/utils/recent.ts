export const RECENT_STORAGE_KEY = "ml-recent-files";
export const RECENT_LIMIT = 5;

export type RecentByGroup = Record<string, string[]>;

export function loadRecentByGroup(): RecentByGroup {
  try {
    const stored = localStorage.getItem(RECENT_STORAGE_KEY);
    if (stored) {
      const parsed = JSON.parse(stored);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        return parsed;
      }
    }
  } catch {
    /* ignore */
  }
  return {};
}

/**
 * Move `fileId` to the front of its group's recent list, dropping duplicates
 * and capping the length. Returns null when nothing would change, so callers
 * can skip both the state update and the localStorage write.
 */
export function pushRecentFile(
  prev: RecentByGroup,
  group: string,
  fileId: string,
): RecentByGroup | null {
  const list = prev[group] ?? [];
  if (list[0] === fileId) return null;
  const next = [fileId, ...list.filter((id) => id !== fileId)].slice(0, RECENT_LIMIT);
  return { ...prev, [group]: next };
}
