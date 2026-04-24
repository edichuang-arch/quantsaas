import type { ReactNode } from 'react';

const STATUS_MAP: Record<string, { color: string; dot: string; label: string }> = {
  RUNNING: { color: 'text-qs-accent border-qs-accent/40 bg-qs-accent/10', dot: 'bg-qs-accent', label: '運行中' },
  STOPPED: { color: 'text-slate-400 border-slate-600/40 bg-slate-600/10', dot: 'bg-slate-500', label: '已暫停' },
  ERROR: { color: 'text-qs-danger border-qs-danger/40 bg-qs-danger/10', dot: 'bg-qs-danger', label: '異常' },
  pending: { color: 'text-qs-warn border-qs-warn/40 bg-qs-warn/10', dot: 'bg-qs-warn', label: '待處理' },
  running: { color: 'text-qs-accent border-qs-accent/40 bg-qs-accent/10', dot: 'bg-qs-accent', label: '進行中' },
  done: { color: 'text-qs-safe border-qs-safe/40 bg-qs-safe/10', dot: 'bg-qs-safe', label: '完成' },
  failed: { color: 'text-qs-danger border-qs-danger/40 bg-qs-danger/10', dot: 'bg-qs-danger', label: '失敗' },
  champion: { color: 'text-qs-accent border-qs-accent/40 bg-qs-accent/10', dot: 'bg-qs-accent', label: '當前最優' },
  challenger: { color: 'text-qs-warm border-qs-warm/40 bg-qs-warm/10', dot: 'bg-qs-warm', label: '候選參數' },
  retired: { color: 'text-slate-400 border-slate-600/40 bg-slate-600/10', dot: 'bg-slate-500', label: '已歸檔' },
};

export function StatusBadge({ status, label }: { status: string; label?: ReactNode }) {
  const m = STATUS_MAP[status] ?? { color: 'text-slate-300', dot: 'bg-slate-500', label: status };
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs ${m.color}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${m.dot}`} />
      {label ?? m.label}
    </span>
  );
}
