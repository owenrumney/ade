// What a modal's detail is allowed to be. A dialog you have to scroll is one
// that gets dismissed unread, which defeats the confirmation.

// shown is how many lines of a list a modal carries before it summarises the
// rest. Enough to recognise what is about to happen, not enough to scroll.
const shown = 6;

// listed is a list for a modal: the first few, then a count of the rest. The
// count matters more than the tail — you are confirming a number.
export function listed(items: string[], max = shown): string {
  if (items.length <= max) return items.join("\n");
  const rest = items.length - max;
  return [...items.slice(0, max), `… and ${rest} more`].join("\n");
}

// quoted is someone else's output in a modal — git's, usually. Blank lines go
// first: a rejection is mostly spacing, and spacing is what makes it long.
export function quoted(text: string, max = shown): string {
  const lines = text
    .split("\n")
    .map((line) => line.trimEnd())
    .filter((line) => line !== "");
  return listed(lines, max);
}
