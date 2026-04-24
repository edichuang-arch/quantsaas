import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Trophy } from 'lucide-react';
import { Card, CardHeader } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { StatusBadge } from '@/shared/ui/StatusBadge';
import { CardSkeleton } from '@/shared/ui/Skeleton';
import { evolutionService } from '@/shared/services/evolution';
import { fmtPercent, fmtRelTime } from '@/shared/format';

export function GenomeLibrary() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ['evolution_status'],
    queryFn: () => evolutionService.getStatus('sigmoid-btc', 'BTCUSDT'),
  });

  async function promote(id: number) {
    if (!confirm('確定要將此挑戰者晉升為當前最優?\n原本的冠軍會被歸檔,Redis 快取將立即失效。')) return;
    try {
      await evolutionService.promote(id);
      qc.invalidateQueries({ queryKey: ['evolution_status'] });
      qc.invalidateQueries({ queryKey: ['champion'] });
    } catch (e) {
      alert((e as Error).message);
    }
  }

  if (isLoading) {
    return <div className="grid gap-3 md:grid-cols-2"><CardSkeleton /><CardSkeleton /></div>;
  }

  const challengers = data?.challengers ?? [];

  if (challengers.length === 0) {
    return (
      <Card className="text-center">
        <div className="py-8">
          <div className="text-sm font-medium text-slate-300">基因庫是空的</div>
          <div className="mt-1 text-xs text-slate-500">跑一輪優化後,候選參數會出現在這裡供你審批</div>
        </div>
      </Card>
    );
  }

  return (
    <div className="grid gap-3 md:grid-cols-2">
      {challengers.map((g) => (
        <Card key={g.ID} className={g.Role === 'champion' ? 'border-qs-accent/40' : ''}>
          <div className="flex items-center justify-between">
            <StatusBadge status={g.Role} />
            <div className="text-xs text-slate-500">{fmtRelTime(g.CreatedAt)}</div>
          </div>

          <div className="mt-3 grid grid-cols-2 gap-3">
            <MiniStat label="綜合評分" value={g.ScoreTotal.toFixed(4)} accent />
            <MiniStat label="最大回撤" value={fmtPercent(g.MaxDrawdown)} />
          </div>

          {g.WindowScores && (
            <div className="mt-3 grid grid-cols-4 gap-2 text-xs text-slate-500">
              {Object.entries(g.WindowScores).map(([k, v]) => (
                <div key={k}>
                  <div>{k}</div>
                  <div className="font-mono text-slate-300">{v.toFixed(3)}</div>
                </div>
              ))}
            </div>
          )}

          {g.Role === 'challenger' && (
            <div className="mt-4 flex justify-end">
              <Button size="sm" onClick={() => promote(g.ID)}>
                <Trophy className="h-3.5 w-3.5" /> 晉升為最優
              </Button>
            </div>
          )}
        </Card>
      ))}
    </div>
  );
}

function MiniStat({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className={`mt-0.5 font-mono text-sm ${accent ? 'text-qs-accent' : 'text-slate-200'}`}>{value}</div>
    </div>
  );
}
