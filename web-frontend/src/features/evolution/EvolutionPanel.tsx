import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Play, Square } from 'lucide-react';
import { Card, CardHeader } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { StatusBadge } from '@/shared/ui/StatusBadge';
import { evolutionService } from '@/shared/services/evolution';
import { fmtPercent } from '@/shared/format';

export function EvolutionPanel() {
  const { data, isLoading } = useQuery({
    queryKey: ['evolution_status'],
    queryFn: () => evolutionService.getStatus('sigmoid-btc', 'BTCUSDT'),
    refetchInterval: 5_000,
  });

  const running = data?.current?.Status === 'running';

  return (
    <div className="space-y-4">
      {running ? <ProgressCard task={data!.current!} /> : <StartCard />}
      <ChampionCard />
    </div>
  );
}

function ProgressCard({ task }: { task: { CurrentGen: number; MaxGenerations: number; BestScore: number; PopSize: number; Status: string } }) {
  const pct = task.MaxGenerations > 0 ? (task.CurrentGen / task.MaxGenerations) * 100 : 0;
  return (
    <Card>
      <CardHeader
        title="進化任務執行中"
        subtitle={`種群大小 ${task.PopSize} · 目標代數 ${task.MaxGenerations}`}
        action={<StatusBadge status={task.Status} />}
      />
      <div className="space-y-3">
        <div>
          <div className="flex justify-between text-xs text-slate-500">
            <span>第 {task.CurrentGen} / {task.MaxGenerations} 代</span>
            <span>{pct.toFixed(0)}%</span>
          </div>
          <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-slate-800">
            <div className="h-full bg-qs-accent transition-all" style={{ width: `${pct}%` }} />
          </div>
        </div>
        <div className="grid grid-cols-2 gap-4">
          <Stat label="當前最優評分" value={task.BestScore.toFixed(4)} />
          <Stat label="狀態" value={task.Status.toUpperCase()} />
        </div>
      </div>
    </Card>
  );
}

function StartCard() {
  const [pop, setPop] = useState('50');
  const [gen, setGen] = useState('10');
  const [testMode, setTestMode] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function start() {
    setErr(null);
    setSubmitting(true);
    try {
      await evolutionService.createTask({
        strategy_id: 'sigmoid-btc',
        symbol: 'BTCUSDT',
        pop_size: Number(pop),
        max_generations: Number(gen),
        spawn_mode: 'inherit',
        test_mode: testMode,
        initial_usdt: 10000,
        monthly_inject: 300,
        lot_step: 0.00001,
        lot_min: 0.00001,
        warmup_days: 30,
      });
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Card>
      <CardHeader title="啟動新一輪優化" subtitle="遺傳演算法在歷史 K 線上搜尋更優參數" />

      <div className="grid gap-4 md:grid-cols-2">
        <Input label="種群大小" type="number" value={pop} onChange={(e) => setPop(e.target.value)} min="10" max="500" />
        <Input label="最大代數" type="number" value={gen} onChange={(e) => setGen(e.target.value)} min="3" max="50" />
        <label className="flex items-center gap-2 text-sm text-slate-400 md:col-span-2">
          <input
            type="checkbox"
            className="h-4 w-4 rounded border-slate-600 bg-slate-900 text-qs-accent focus:ring-qs-accent"
            checked={testMode}
            onChange={(e) => setTestMode(e.target.checked)}
          />
          測試模式（快速跑完,Pop=10 Gen=3）
        </label>
      </div>

      {err && <div className="mt-3 text-xs text-qs-danger">{err}</div>}

      <div className="mt-4">
        <Button onClick={start} loading={submitting}>
          <Play className="h-3.5 w-3.5" /> 啟動優化
        </Button>
      </div>
    </Card>
  );
}

function ChampionCard() {
  const { data } = useQuery({
    queryKey: ['champion'],
    queryFn: () => evolutionService.champion('sigmoid-btc', 'BTCUSDT'),
  });

  if (!data) {
    return (
      <Card>
        <div className="py-6 text-center text-sm text-slate-500">
          尚未有冠軍參數,先跑一輪優化並人工晉升挑戰者
        </div>
      </Card>
    );
  }

  return (
    <Card className="border-qs-accent/30">
      <CardHeader
        title="當前最優參數"
        subtitle="驅動實盤實例的冠軍基因"
        action={<StatusBadge status="champion" />}
      />
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <Stat label="綜合評分" value={data.ScoreTotal.toFixed(4)} accent />
        <Stat label="最大回撤" value={fmtPercent(data.MaxDrawdown)} />
        <Stat label="啟用時間" value={data.ActivatedAt ? new Date(data.ActivatedAt).toLocaleString('zh-TW') : '—'} />
        <Stat label="策略" value={data.StrategyID} />
      </div>
    </Card>
  );
}

function Stat({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div>
      <div className="text-xs text-slate-500">{label}</div>
      <div className={`mt-0.5 font-mono text-sm ${accent ? 'text-qs-accent' : 'text-slate-200'}`}>{value}</div>
    </div>
  );
}
