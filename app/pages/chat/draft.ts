// The composer's text, kept per conversation: the base carries `/s/<session>`.

export type Store = Pick<Storage, "getItem" | "setItem" | "removeItem">;

export function draftKey(base: string): string {
  return `ade.draft:${base}`;
}

// In a try: a page with storage denied must not lose a keystroke over it.
export function keepDraft(store: Store, base: string, text: string): void {
  try {
    if (text.trim()) store.setItem(draftKey(base), text);
    else store.removeItem(draftKey(base));
  } catch {
    void 0;
  }
}

export function draftOf(store: Store, base: string): string {
  try {
    return store.getItem(draftKey(base)) ?? "";
  } catch {
    return "";
  }
}
