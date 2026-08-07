/** @vitest-environment jsdom */
import { beforeEach, describe, expect, it } from 'vitest';
import type { WizardState } from './wizard';
import { EMPTY, resetWizard, rubricProgress, updateWizard } from './wizard';

// the store is module-level and localStorage-backed; each test starts clean
beforeEach(() => {
  localStorage.clear();
  resetWizard('w');
});

// what actually survives a reload — the persisted document, not the in-memory copy
const read = (repo = 'w'): WizardState => JSON.parse(localStorage.getItem(`specquill-wizard:${repo}`)!);

describe('wizard store', () => {
  it('persists patches per repo', () => {
    updateWizard('w', { stage: 'interview', intent: 'seven years', family: 'spec' });
    expect(read()).toMatchObject({ stage: 'interview', intent: 'seven years', family: 'spec' });

    updateWizard('other', { intent: 'unrelated' });
    expect(read().intent).toBe('seven years');
    expect(read('other').intent).toBe('unrelated');
  });

  it('accepts a functional patch derived from current state', () => {
    updateWizard('w', { touched: ['Overview'] });
    updateWizard('w', (s) => ({ touched: [...s.touched, 'Edge cases'] }));
    expect(read().touched).toEqual(['Overview', 'Edge cases']);
  });

  it('reset clears everything back to a blank intent step', () => {
    updateWizard('w', { stage: 'review', sections: [{ name: 'Overview', content: 'x' }], intent: 'y' });
    resetWizard('w');
    expect(read()).toEqual(EMPTY);
  });

  it('a persisted state from an older shape still yields every field', () => {
    // fields added after a user last ran the wizard must not come back
    // undefined — components index into them without guards
    localStorage.setItem('specquill-wizard:legacy', JSON.stringify({ stage: 'review', intent: 'old' }));
    updateWizard('legacy', {});
    expect(read('legacy')).toMatchObject({ sections: [], rubric: [], intent: 'old', stage: 'review' });
  });

  it('corrupt persisted state starts fresh instead of throwing', () => {
    localStorage.setItem('specquill-wizard:broken', '{not json');
    expect(() => updateWizard('broken', { intent: 'x' })).not.toThrow();
    expect(read('broken').intent).toBe('x');
  });
});

describe('rubricProgress', () => {
  it('counts met criteria', () => {
    expect(rubricProgress([{ criterion: 'a', met: true }, { criterion: 'b', met: false }])).toEqual({ met: 1, total: 2 });
    expect(rubricProgress([])).toEqual({ met: 0, total: 0 });
  });
});
