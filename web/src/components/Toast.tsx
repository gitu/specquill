import { createContext, useCallback, useContext, useRef, useState, ReactNode } from 'react';
import { sx } from '../lib/sx';

export interface ToastInput {
  text: string;
  kind?: 'info' | 'success' | 'warn' | 'error';
  action?: { label: string; onClick: () => void };
  /** ms; 0 = sticky until dismissed */
  duration?: number;
}

interface ToastItem extends ToastInput {
  id: number;
}

const Ctx = createContext<{ push: (t: ToastInput) => void }>({ push: () => {} });
export const useToasts = () => useContext(Ctx);

const KIND_STYLE: Record<string, string> = {
  info: 'border-color:var(--border-2)',
  success: 'border-color:var(--data-line);background:var(--data-bg)',
  warn: 'border-color:var(--reg-line);background:var(--reg-bg)',
  error: 'border-color:var(--reg-line);background:var(--del-bg)',
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const seq = useRef(0);

  const push = useCallback((t: ToastInput) => {
    const id = ++seq.current;
    setItems((xs) => [...xs.slice(-3), { ...t, id }]);
    const ms = t.duration ?? 5000;
    if (ms > 0) setTimeout(() => setItems((xs) => xs.filter((x) => x.id !== id)), ms);
  }, []);

  const dismiss = (id: number) => setItems((xs) => xs.filter((x) => x.id !== id));

  return (
    <Ctx.Provider value={{ push }}>
      {children}
      <div style={sx('position:fixed;right:16px;bottom:16px;z-index:90;display:flex;flex-direction:column;gap:8px;max-width:380px')}>
        {items.map((t) => (
          <div key={t.id} style={sx('display:flex;align-items:center;gap:10px;padding:10px 13px;background:var(--surface);border:1px solid var(--border);border-radius:10px;box-shadow:var(--shadow-lg);font-size:12.5px;color:var(--text);' + (KIND_STYLE[t.kind || 'info'] || ''))}>
            <span style={sx('flex:1;line-height:1.5')}>{t.text}</span>
            {t.action && (
              <button
                onClick={() => { t.action!.onClick(); dismiss(t.id); }}
                style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--prod);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer;white-space:nowrap')}
              >
                {t.action.label}
              </button>
            )}
            <span onClick={() => dismiss(t.id)} style={sx('cursor:pointer;color:var(--text-3);font-size:14px;line-height:1')}>×</span>
          </div>
        ))}
      </div>
    </Ctx.Provider>
  );
}
