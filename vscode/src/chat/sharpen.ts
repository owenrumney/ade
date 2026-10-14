// A question the sharpener has about the prompt, as a choice.
export interface Question {
  ask: string;
  options: string[];
}

// idle is how long the box sits still before the prompt is looked at.
export const idle = 1200;

// answered is the prompt with one answer added as a line of its own.
export function answered(prompt: string, ask: string, option: string): string {
  const body = prompt.replace(/\s+$/, "");
  return `${body}${body ? "\n" : ""}${ask} ${option}`;
}

// questionsOf reads the server's answer; anything else is no questions.
export function questionsOf(text: string | undefined): Question[] {
  try {
    const said = JSON.parse(text || "{}") as { questions?: Question[] };
    return (said.questions || []).filter(
      (q) => q?.ask && Array.isArray(q.options) && q.options.length > 1,
    );
  } catch {
    return [];
  }
}
