import type { LogRecord } from "./client";

// logLine is one record as the terminal would have printed it.
export function logLine(record: LogRecord): string {
  const at = record.at.slice(11, 19);
  const attrs = (record.attrs ?? [])
    .map((attr) => `${attr.key}=${attr.value}`)
    .join(" ");
  return [at, record.level.padEnd(5), record.msg, attrs]
    .filter(Boolean)
    .join(" ");
}
