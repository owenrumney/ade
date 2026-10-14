import type { Server } from "./discover";
import { tell } from "./launcher";

// Card mirrors launcher.Card: what the concierge proposes and only you may
// authorise. The text is what would be sent, exactly as it is shown.
export interface Card {
  id: string;
  kind: "say" | "start";
  session?: string;
  dir?: string;
  name?: string;
  design?: boolean;
  text: string;
  at?: string;
  drafts?: number;
}

export async function waiting(server: Server): Promise<Card[]> {
  const res = await fetch(
    `${server.base}/api/concierge/cards?t=${encodeURIComponent(server.token)}`,
    { headers: { "X-Ade-Token": server.token } },
  );
  if (!res.ok) throw new Error(`cards: ${res.status}`);
  const body = (await res.json()) as { cards: Card[] | null };
  return body.cards ?? [];
}

export async function press(
  server: Server,
  id: string,
  action: "start" | "cancel" | "edit",
  said = "",
): Promise<Card | undefined> {
  return tell<Card>(server, `/api/concierge/cards/${encodeURIComponent(id)}`, {
    action,
    said,
  });
}

// title is the card's one line: what would happen, and where. The text is read
// beneath it, so this says only the thing the text cannot.
export function title(card: Card): string {
  const where = card.name || card.dir || card.session || "somewhere";
  if (card.kind === "say") return `Queue a turn for ${where}?`;
  return card.design
    ? `Start a design in ${where}?`
    : `Start a conversation in ${where}?`;
}

// detail is what the press would send. A redrafted card says so, because a
// card that changed while you were reading it has to admit it.
export function detail(card: Card): string {
  const drafts = card.drafts ?? 0;
  return drafts > 0 ? `${card.text}\n\n(rewritten ${drafts}×)` : card.text;
}
