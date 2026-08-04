import { describe, expect, it } from 'vitest';
import { localizeActivity, localizeFeed } from './feed';

// The stored stamp is UTC; the rendered one is the reader's clock. Asserting
// against a fixed offset would pin the suite to one timezone, so the tests
// compare against what the browser itself makes of the same instant.
const localOf = (iso: string) => {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
};
const startedAt = Date.UTC(2026, 7, 2, 19, 0, 0) / 1000; // 2026-08-02 19:00:00Z

describe('localizeActivity', () => {
  it('renders the UTC stamp in local time and keeps the rest of the line', () => {
    expect(localizeActivity('19:00:04Z [1/2] specs/a.md', startedAt))
      .toBe(`${localOf('2026-08-02T19:00:04Z')} [1/2] specs/a.md`);
  });

  it('carries a line past UTC midnight onto the next day', () => {
    expect(localizeActivity('00:12:30Z ✓ done', startedAt))
      .toBe(`${localOf('2026-08-03T00:12:30Z')} ✓ done`);
  });

  it('leaves lines without a stamp alone', () => {
    for (const line of ['… 12 earlier lines trimmed', '  · read ~reg/rules.md', '']) {
      expect(localizeActivity(line, startedAt)).toBe(line);
    }
  });

  it('leaves an unmarked (legacy, already-local) stamp alone', () => {
    expect(localizeActivity('19:00:04 [1/2] specs/a.md', startedAt))
      .toBe('19:00:04 [1/2] specs/a.md');
  });

  it('maps the whole feed', () => {
    expect(localizeFeed(['19:00:04Z a', 'no stamp'], startedAt))
      .toEqual([`${localOf('2026-08-02T19:00:04Z')} a`, 'no stamp']);
  });
});
