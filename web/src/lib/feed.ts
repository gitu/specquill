/**
 * Run-activity lines are persisted verbatim into the alignment report, so the
 * server stamps them in UTC (`19:00:04Z [1/2] specs/a.md`). On screen they
 * belong in the reader's own clock — this rewrites just that leading stamp.
 */

const STAMP = /^(\d{2}):(\d{2}):(\d{2})Z /;

/**
 * Localize one feed line. `startedAt` (unix seconds, the run's start) supplies
 * the calendar day the bare time-of-day belongs to; a line that reads earlier
 * than the start has crossed UTC midnight, so it belongs to the next day.
 */
export function localizeActivity(line: string, startedAt: number): string {
  const m = STAMP.exec(line);
  if (!m) return line;
  const start = new Date(startedAt * 1000);
  const at = new Date(Date.UTC(
    start.getUTCFullYear(), start.getUTCMonth(), start.getUTCDate(),
    Number(m[1]), Number(m[2]), Number(m[3]),
  ));
  if (startedAt > 0 && at.getTime() < start.getTime() - 60_000) {
    at.setUTCDate(at.getUTCDate() + 1);
  }
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(at.getHours())}:${p(at.getMinutes())}:${p(at.getSeconds())} ` +
    line.slice(m[0].length);
}

/** localizeActivity over a whole feed. */
export function localizeFeed(lines: string[], startedAt: number): string[] {
  return lines.map((l) => localizeActivity(l, startedAt));
}
