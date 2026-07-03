import { describe, expect, it } from 'vitest';
import { diffLinesLCS } from './linediff';

describe('diffLinesLCS', () => {
  it('no changes', () => {
    const d = diffLinesLCS('a\nb\nc', 'a\nb\nc');
    expect(d.added.size + d.changed.size + d.removedAt.size).toBe(0);
  });

  it('pure additions', () => {
    const d = diffLinesLCS('a\nc', 'a\nb\nc');
    expect([...d.added]).toEqual([2]);
    expect(d.changed.size).toBe(0);
  });

  it('changed line pairs a deletion with an insertion', () => {
    const d = diffLinesLCS('a\nOLD\nc', 'a\nNEW\nc');
    expect([...d.changed]).toEqual([2]);
    expect(d.added.size).toBe(0);
    expect(d.removedAt.size).toBe(0);
  });

  it('deletion marks the position', () => {
    const d = diffLinesLCS('a\nb\nc', 'a\nc');
    expect(d.removedAt.size).toBe(1);
    expect(d.added.size).toBe(0);
  });

  it('trailing additions', () => {
    const d = diffLinesLCS('a', 'a\nx\ny');
    expect([...d.added].sort()).toEqual([2, 3]);
  });

  it('everything replaced', () => {
    const d = diffLinesLCS('a\nb', 'x\ny');
    expect(d.changed.size + d.added.size).toBe(2);
  });
});
