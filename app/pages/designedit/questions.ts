// An open-questions table, recognised by its header. The Accept column is the
// editor's own, so accepting leaves no mark in the document until it is folded.

export interface Question {
  ref: string;
  question: string;
  options: string;
  recommendation: string;
}

export interface Columns {
  ref: number;
  question: number;
  options: number;
  recommendation: number;
}

function find(header: string[], want: string): number {
  return header.findIndex((cell) => cell.trim().toLowerCase() === want);
}

// columnsOf reads the header, or nothing when this is an ordinary table. Two
// of the three names is some other table that happens to share a word.
export function columnsOf(header: string[] | undefined): Columns | undefined {
  if (!header) return undefined;
  const question = find(header, "question");
  const options = find(header, "options");
  const recommendation = find(header, "recommendation");
  if (question < 0 || options < 0 || recommendation < 0) return undefined;
  return { ref: find(header, "#"), question, options, recommendation };
}

// questionsIn is the body rows of a recommendation table. A row with nothing
// recommended is not offered: there is nothing to accept.
export function questionsIn(rows: string[][]): Question[] {
  const columns = columnsOf(rows[0]);
  if (!columns) return [];
  const out: Question[] = [];
  rows.slice(1).forEach((row, i) => {
    const recommendation = (row[columns.recommendation] ?? "").trim();
    const question = (row[columns.question] ?? "").trim();
    if (!recommendation || !question) return;
    const ref = columns.ref >= 0 ? (row[columns.ref] ?? "").trim() : "";
    out.push({ ref: ref || String(i + 1), question, options: (row[columns.options] ?? "").trim(), recommendation });
  });
  return out;
}
