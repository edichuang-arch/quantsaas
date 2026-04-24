import { useMemo } from 'react';
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  ResponsiveContainer,
} from 'recharts';

export interface PnLPoint {
  t: string;
  equity: number;
}

// 前端当前还没有真实的 equity 历史端点；使用 trades 的累计估算。
// 等后端补接口后直接替换为真数据源即可。
export function PnLChart({ points }: { points: PnLPoint[] }) {
  const data = useMemo(() => points.map((p) => ({ ...p, t: new Date(p.t).toLocaleDateString('zh-TW') })), [points]);

  if (data.length === 0) {
    return (
      <div className="flex h-72 flex-col items-center justify-center gap-2 text-slate-500">
        <div className="text-sm">暫無數據</div>
        <div className="text-xs">策略啟動後將在此展示淨值曲線</div>
      </div>
    );
  }

  return (
    <div className="h-72 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 10, right: 20, bottom: 0, left: -10 }}>
          <defs>
            <linearGradient id="pnlGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#2dd4bf" stopOpacity={0.4} />
              <stop offset="95%" stopColor="#2dd4bf" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" />
          <XAxis dataKey="t" stroke="#64748b" tick={{ fontSize: 11 }} />
          <YAxis stroke="#64748b" tick={{ fontSize: 11 }} />
          <Tooltip
            contentStyle={{ background: '#0f172a', border: '1px solid #334155', fontSize: 12 }}
            labelStyle={{ color: '#94a3b8' }}
          />
          <Area
            type="monotone"
            dataKey="equity"
            stroke="#2dd4bf"
            strokeWidth={2}
            fill="url(#pnlGrad)"
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
