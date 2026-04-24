import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useSearchParams } from 'react-router-dom';
import { Plus, Settings as SettingsIcon, Play, Pause } from 'lucide-react';
import { Card, CardHeader } from '@/shared/ui/Card';
import { StatusBadge } from '@/shared/ui/StatusBadge';
import { Button } from '@/shared/ui/Button';
import { PnLChart, type PnLPoint } from './PnLChart';
import { CardSkeleton, PnLChartSkeleton } from '@/shared/ui/Skeleton';
import { dashboardService } from '@/shared/services/dashboard';
import { instancesService } from '@/shared/services/instances';
import { fmtUSDT, fmtAsset, fmtRelTime } from '@/shared/format';

export function DashboardPage() {
  const [sp, setSp] = useSearchParams();
  const selectedID = sp.get('instance');

  const { data, isLoading } = useQuery({
    queryKey: ['dashboard_overview'],
    queryFn: dashboardService.overview,
    refetchInterval: 60_000,
  });

  const selected = useMemo(() => {
    if (!data) return null;
    if (selectedID) return data.find((s) => String(s.instance.ID) === selectedID) ?? data[0];
    return data[0];
  }, [data, selectedID]);

  async function toggleInstance(id: number, status: string) {
    try {
      if (status === 'RUNNING') await instancesService.stop(id);
      else await instancesService.start(id);
      window.location.reload();
    } catch (e) {
      alert((e as Error).message);
    }
  }

  if (isLoading) {
    return (
      <div className="qs-bento-grid">
        <CardSkeleton /><CardSkeleton /><CardSkeleton /><CardSkeleton />
      </div>
    );
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[320px_1fr]">
      {/* 左侧实例选择 */}
      <aside className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="text-sm font-semibold tracking-wide text-slate-300">我的實例</div>
          <Link to="/instances/new">
            <Button size="sm" variant="secondary"><Plus className="h-3.5 w-3.5" /> 新建</Button>
          </Link>
        </div>

        {(data ?? []).map((s) => {
          const active = selected?.instance.ID === s.instance.ID;
          return (
            <button
              key={s.instance.ID}
              onClick={() => setSp({ instance: String(s.instance.ID) })}
              className={`w-full rounded-xl border px-4 py-3 text-left transition-colors ${
                active
                  ? 'border-qs-accent/40 bg-qs-accent/[0.06]'
                  : 'border-white/5 bg-slate-900/30 hover:bg-white/[0.03]'
              }`}
            >
              <div className="flex items-center justify-between">
                <div className="text-sm font-medium text-slate-200">{s.instance.Name}</div>
                <StatusBadge status={s.instance.Status} />
              </div>
              <div className="mt-1 font-mono text-xs text-slate-500">{s.instance.Symbol}</div>
              <div className="mt-2 font-mono text-sm text-slate-300">
                {fmtUSDT(s.portfolio?.TotalEquity)} <span className="text-xs text-slate-500">USDT</span>
              </div>
              <div
                className="mt-2 flex gap-1.5"
                onClick={(e) => { e.stopPropagation(); toggleInstance(s.instance.ID, s.instance.Status); }}
              >
                <Button size="sm" variant="ghost">
                  {s.instance.Status === 'RUNNING' ? (
                    <><Pause className="h-3 w-3" /> 暫停</>
                  ) : (
                    <><Play className="h-3 w-3" /> 啟動</>
                  )}
                </Button>
              </div>
            </button>
          );
        })}

        {(data ?? []).length === 0 && (
          <Card className="text-center text-sm text-slate-500">
            <div className="mb-3">還沒有任何實例</div>
            <Link to="/instances/new">
              <Button size="sm"><Plus className="h-3.5 w-3.5" /> 建立第一個</Button>
            </Link>
          </Card>
        )}
      </aside>

      {/* 右侧主内容 */}
      <section className="space-y-4">
        {selected ? (
          <>
            <StrategyOverviewCard instance={selected.instance} portfolio={selected.portfolio} />
            <Card>
              <CardHeader
                title="淨值曲線"
                subtitle="總資產隨時間變化（USDT）"
                action={<Link to="/backtesting"><Button size="sm" variant="ghost">查看回測</Button></Link>}
              />
              {isLoading ? <PnLChartSkeleton /> : <PnLChart points={deriveEquityPoints(selected.instance.CreatedAt, selected.portfolio?.TotalEquity ?? 0)} />}
            </Card>
            <StrategyJourneyCard instance={selected.instance} recentTradeCount={selected.recent_trade_count} />
          </>
        ) : (
          <Card className="flex h-[420px] items-center justify-center text-sm text-slate-500">
            左側選擇一個實例查看概況
          </Card>
        )}
      </section>
    </div>
  );
}

function StrategyOverviewCard({
  instance,
  portfolio,
}: {
  instance: { Name: string; Symbol: string; Status: string; InitialCapitalUSDT: number; CreatedAt: string; LastProcessedBarTime: number };
  portfolio?: { TotalEquity: number; USDTBalance: number; DeadStackAsset: number; FloatStackAsset: number; ColdSealedAsset: number };
}) {
  return (
    <Card>
      <CardHeader
        title={<span>{instance.Name} · <span className="font-mono text-xs text-slate-500">{instance.Symbol}</span></span>}
        subtitle={<>
          {instance.LastProcessedBarTime > 0
            ? `最後決策 ${fmtRelTime(new Date(instance.LastProcessedBarTime).toISOString())}`
            : '尚未開始決策'}
        </>}
        action={<Link to={`/instances`}><Button variant="ghost" size="sm"><SettingsIcon className="h-3.5 w-3.5" /> 配置</Button></Link>}
      />

      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <Metric label="總資產" value={fmtUSDT(portfolio?.TotalEquity)} unit="USDT" accent />
        <Metric label="可用資金" value={fmtUSDT(portfolio?.USDTBalance)} unit="USDT" />
        <Metric label="長期持倉" value={fmtAsset(portfolio?.DeadStackAsset)} unit={instance.Symbol.replace('USDT', '')} />
        <Metric label="活躍倉位" value={fmtAsset(portfolio?.FloatStackAsset)} unit={instance.Symbol.replace('USDT', '')} />
      </div>

      {(portfolio?.ColdSealedAsset ?? 0) > 0 && (
        <div className="mt-3 text-xs text-slate-500">
          封存資產: <span className="font-mono text-slate-400">{fmtAsset(portfolio?.ColdSealedAsset)}</span>
        </div>
      )}
    </Card>
  );
}

function Metric({ label, value, unit, accent }: { label: string; value: string; unit?: string; accent?: boolean }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className={`mt-1 font-mono text-lg ${accent ? 'text-qs-accent' : 'text-slate-200'}`}>{value}</div>
      {unit && <div className="text-xs text-slate-500">{unit}</div>}
    </div>
  );
}

function StrategyJourneyCard({ instance, recentTradeCount }: { instance: { StartedAt?: string; CreatedAt: string }; recentTradeCount: number }) {
  return (
    <Card>
      <CardHeader title="策略旅程" subtitle="關鍵里程碑" />
      <div className="grid gap-3 text-xs text-slate-400 md:grid-cols-3">
        <JourneyItem label="建立時間" value={fmtRelTime(instance.CreatedAt)} />
        <JourneyItem label="首次啟動" value={instance.StartedAt ? fmtRelTime(instance.StartedAt) : '尚未啟動'} />
        <JourneyItem label="累計成交" value={`${recentTradeCount} 筆`} />
      </div>
    </Card>
  );
}

function JourneyItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-white/5 bg-slate-900/30 p-3">
      <div className="text-[11px] text-slate-500">{label}</div>
      <div className="mt-0.5 font-mono text-sm text-slate-200">{value}</div>
    </div>
  );
}

// 临时：根据起始时间与当前权益生成 7 个点的"趋近"曲线。
// 真实系统应从 /api/v1/dashboard/equity-snapshots 拉时间序列。
function deriveEquityPoints(createdAt: string, currentEquity: number): PnLPoint[] {
  if (currentEquity <= 0) return [];
  const days = 7;
  const start = new Date(createdAt).getTime();
  const now = Date.now();
  const step = Math.max(1, Math.floor((now - start) / days));
  return Array.from({ length: days + 1 }, (_, i) => ({
    t: new Date(start + i * step).toISOString(),
    equity: currentEquity * (0.92 + (i * 0.08) / days),
  }));
}
