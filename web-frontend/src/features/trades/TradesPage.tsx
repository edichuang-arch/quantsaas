import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChevronLeft, ChevronRight, ScrollText, ArrowDownCircle, ArrowUpCircle } from 'lucide-react';
import { Card, CardHeader } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { TableSkeleton } from '@/shared/ui/Skeleton';
import { instancesService, type TradesQuery } from '@/shared/services/instances';
import { fmtUSDT, fmtAsset } from '@/shared/format';

type ActionFilter = '' | 'BUY' | 'SELL';
type EngineFilter = '' | 'MACRO' | 'MICRO';
type LotFilter = '' | 'DEAD_STACK' | 'FLOATING' | 'COLD_SEALED';

const PAGE_SIZE = 50;

export function TradesPage() {
  // 实例列表
  const { data: instances, isLoading: loadingInstances } = useQuery({
    queryKey: ['instances'],
    queryFn: instancesService.list,
  });

  const [instanceID, setInstanceID] = useState<number | null>(null);
  const [page, setPage] = useState(1);
  const [action, setAction] = useState<ActionFilter>('');
  const [engine, setEngine] = useState<EngineFilter>('');
  const [lotType, setLotType] = useState<LotFilter>('');

  // 默认选第一个实例
  const effectiveInstanceID = useMemo(() => {
    if (instanceID) return instanceID;
    return instances?.[0]?.ID ?? null;
  }, [instanceID, instances]);

  const queryOpts: TradesQuery = useMemo(
    () => ({
      page,
      pageSize: PAGE_SIZE,
      ...(action && { action: action as 'BUY' | 'SELL' }),
      ...(engine && { engine: engine as 'MACRO' | 'MICRO' }),
      ...(lotType && { lotType: lotType as 'DEAD_STACK' | 'FLOATING' | 'COLD_SEALED' }),
    }),
    [page, action, engine, lotType],
  );

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['trades', effectiveInstanceID, queryOpts],
    queryFn: () =>
      effectiveInstanceID ? instancesService.trades(effectiveInstanceID, queryOpts) : null,
    enabled: !!effectiveInstanceID,
    refetchInterval: 30_000,
  });

  const totalPages = data ? Math.max(1, Math.ceil(data.total / data.page_size)) : 1;

  // 任何筛选变化都重置页码
  function resetPage() {
    setPage(1);
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="flex items-center gap-2 text-lg font-semibold tracking-wide text-slate-200">
            <ScrollText className="h-4 w-4 text-qs-accent" /> 成交紀錄
          </h1>
          <p className="mt-1 text-sm text-slate-500">所有實例的歷史成交，按時間倒序</p>
        </div>
      </div>

      <Card>
        <CardHeader
          title="篩選"
          subtitle={data ? `共 ${data.total} 筆成交` : loadingInstances ? '載入中…' : '尚無資料'}
        />
        <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
          <FilterSelect
            label="實例"
            value={effectiveInstanceID?.toString() ?? ''}
            onChange={(v) => {
              setInstanceID(Number(v) || null);
              resetPage();
            }}
            options={(instances ?? []).map((i) => ({ value: String(i.ID), label: i.Name }))}
          />
          <FilterSelect
            label="動作"
            value={action}
            onChange={(v) => {
              setAction(v as ActionFilter);
              resetPage();
            }}
            options={[
              { value: '', label: '全部' },
              { value: 'BUY', label: '買入' },
              { value: 'SELL', label: '賣出' },
            ]}
          />
          <FilterSelect
            label="引擎"
            value={engine}
            onChange={(v) => {
              setEngine(v as EngineFilter);
              resetPage();
            }}
            options={[
              { value: '', label: '全部' },
              { value: 'MACRO', label: 'MACRO（宏觀）' },
              { value: 'MICRO', label: 'MICRO（微觀）' },
            ]}
          />
          <FilterSelect
            label="Lot 類型"
            value={lotType}
            onChange={(v) => {
              setLotType(v as LotFilter);
              resetPage();
            }}
            options={[
              { value: '', label: '全部' },
              { value: 'DEAD_STACK', label: 'Dead Stack（死守）' },
              { value: 'FLOATING', label: 'Floating（浮動）' },
              { value: 'COLD_SEALED', label: 'Cold Sealed（冷封）' },
            ]}
          />
        </div>
      </Card>

      <Card>
        <CardHeader
          title="成交清單"
          subtitle={
            data ? `第 ${data.page} / ${totalPages} 頁，每頁 ${data.page_size} 筆` : ''
          }
          action={
            <div className="flex items-center gap-1.5">
              <Button
                size="sm"
                variant="ghost"
                disabled={page <= 1 || isFetching}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
              >
                <ChevronLeft className="h-3.5 w-3.5" /> 上一頁
              </Button>
              <Button
                size="sm"
                variant="ghost"
                disabled={page >= totalPages || isFetching}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              >
                下一頁 <ChevronRight className="h-3.5 w-3.5" />
              </Button>
            </div>
          }
        />

        {isLoading ? (
          <TableSkeleton rows={8} />
        ) : !data || data.data.length === 0 ? (
          <div className="py-10 text-center text-sm text-slate-500">
            {effectiveInstanceID ? '此篩選條件下尚無成交記錄' : '請先建立並啟動一個實例'}
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full text-sm">
              <thead className="text-left text-xs uppercase tracking-wider text-slate-500">
                <tr className="border-b border-white/5">
                  <th className="py-3 pr-4">時間</th>
                  <th className="py-3 pr-4">動作</th>
                  <th className="py-3 pr-4">引擎</th>
                  <th className="py-3 pr-4">Lot</th>
                  <th className="py-3 pr-4 text-right">數量</th>
                  <th className="py-3 pr-4 text-right">成交價 (USDT)</th>
                  <th className="py-3 pr-4 text-right">金額 (USDT)</th>
                  <th className="py-3 pr-4 text-right">手續費</th>
                </tr>
              </thead>
              <tbody>
                {data.data.map((t) => (
                  <tr key={t.ID} className="border-b border-white/5 last:border-0">
                    <td className="py-3 pr-4 text-xs text-slate-400">
                      {new Date(t.CreatedAt).toLocaleString('zh-TW', { hour12: false })}
                    </td>
                    <td className="py-3 pr-4">
                      <ActionBadge action={t.Action} />
                    </td>
                    <td className="py-3 pr-4 font-mono text-xs text-slate-400">{t.Engine}</td>
                    <td className="py-3 pr-4">
                      <LotBadge lot={t.LotType} />
                    </td>
                    <td className="py-3 pr-4 text-right font-mono text-slate-300">
                      {fmtAsset(t.FilledQty)}
                    </td>
                    <td className="py-3 pr-4 text-right font-mono text-slate-300">
                      {fmtUSDT(t.FilledPrice)}
                    </td>
                    <td className="py-3 pr-4 text-right font-mono text-slate-200">
                      {fmtUSDT(t.FilledUSDT)}
                    </td>
                    <td className="py-3 pr-4 text-right font-mono text-xs text-slate-500">
                      {fmtUSDT(t.Fee)}
                      {t.FeeAsset && <span className="ml-1 text-slate-600">{t.FeeAsset}</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}

// --- 子元件 ---

function FilterSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs uppercase tracking-wider text-slate-500">{label}</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded-md border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-200 focus:border-qs-accent/40 focus:outline-none"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value} className="bg-slate-900">
            {o.label}
          </option>
        ))}
      </select>
    </label>
  );
}

function ActionBadge({ action }: { action: 'BUY' | 'SELL' }) {
  if (action === 'BUY') {
    return (
      <span className="inline-flex items-center gap-1 rounded-md border border-emerald-500/20 bg-emerald-500/[0.08] px-2 py-0.5 text-xs font-medium text-emerald-400">
        <ArrowDownCircle className="h-3 w-3" /> 買入
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-rose-500/20 bg-rose-500/[0.08] px-2 py-0.5 text-xs font-medium text-rose-400">
      <ArrowUpCircle className="h-3 w-3" /> 賣出
    </span>
  );
}

function LotBadge({ lot }: { lot: 'DEAD_STACK' | 'FLOATING' | 'COLD_SEALED' }) {
  const map = {
    DEAD_STACK: { label: 'Dead', cls: 'border-amber-500/20 bg-amber-500/[0.08] text-amber-400' },
    FLOATING: { label: 'Float', cls: 'border-sky-500/20 bg-sky-500/[0.08] text-sky-400' },
    COLD_SEALED: { label: 'Cold', cls: 'border-violet-500/20 bg-violet-500/[0.08] text-violet-400' },
  };
  const m = map[lot];
  return (
    <span className={`inline-block rounded-md border px-2 py-0.5 text-xs font-medium ${m.cls}`}>
      {m.label}
    </span>
  );
}
