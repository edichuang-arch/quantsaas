import { useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { Card, CardHeader } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { strategyCatalog, type StrategyMeta } from '@/shared/config/strategyCatalog';
import { instancesService } from '@/shared/services/instances';

type Step = 1 | 2;

export function InstanceCreatePage() {
  const nav = useNavigate();
  const [sp] = useSearchParams();
  const initialTpl = sp.get('template');
  const [step, setStep] = useState<Step>(initialTpl ? 2 : 1);
  const [selected, setSelected] = useState<StrategyMeta | null>(
    initialTpl ? strategyCatalog.find((s) => s.id === initialTpl) ?? null : null
  );
  const [name, setName] = useState('');
  const [initialCap, setInitialCap] = useState('10000');
  const [monthly, setMonthly] = useState('300');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function onSubmit() {
    if (!selected) return;
    setErr(null);
    setSubmitting(true);
    try {
      // 注意：template_id 需要后端 StrategyTemplate 表里对应记录的 ID。
      // 简化：假设后端 seed 了 StrategyID=sigmoid-btc 的模板，ID=1。
      const inst = await instancesService.create({
        template_id: 1,
        name: name || `${selected.name} · ${new Date().toLocaleDateString()}`,
        symbol: selected.symbol,
        initial_capital_usdt: Number(initialCap),
        monthly_inject_usdt: Number(monthly),
      });
      nav(`/?instance=${inst.ID}`);
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <div className="flex items-center gap-2 text-sm text-slate-500">
        <span className={step === 1 ? 'text-qs-accent' : ''}>1. 選擇策略</span>
        <ChevronRight className="h-3.5 w-3.5" />
        <span className={step === 2 ? 'text-qs-accent' : ''}>2. 填寫配置</span>
      </div>

      {step === 1 && (
        <Card>
          <CardHeader title="選擇一個策略模板" subtitle="每個模板對應一個演算法與交易對" />
          <div className="space-y-3">
            {strategyCatalog.map((s) => (
              <button
                key={s.id}
                onClick={() => { setSelected(s); setStep(2); }}
                className={`w-full rounded-lg border px-4 py-3 text-left transition ${
                  selected?.id === s.id
                    ? 'border-qs-accent/40 bg-qs-accent/[0.06]'
                    : 'border-white/5 bg-slate-900/30 hover:bg-white/[0.03]'
                }`}
              >
                <div className="flex items-center justify-between">
                  <div className="text-sm font-medium text-slate-200">{s.name}</div>
                  <div className="font-mono text-xs text-slate-500">{s.symbol}</div>
                </div>
                <div className="mt-1 text-xs text-slate-500">{s.description}</div>
              </button>
            ))}
          </div>
        </Card>
      )}

      {step === 2 && selected && (
        <Card>
          <CardHeader
            title={`配置 ${selected.name}`}
            subtitle={<span className="font-mono">{selected.symbol}</span>}
            action={
              <Button size="sm" variant="ghost" onClick={() => setStep(1)}>
                <ChevronLeft className="h-3.5 w-3.5" /> 換一個
              </Button>
            }
          />

          <div className="grid gap-4 md:grid-cols-2">
            <Input
              label="實例名稱"
              placeholder="例如：主倉 BTC"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <Input
              label="初始資本 (USDT)"
              type="number"
              inputMode="decimal"
              min="10"
              value={initialCap}
              onChange={(e) => setInitialCap(e.target.value)}
              hint="建議 10,000 以上以發揮 Sigmoid 動態天平的效果"
            />
            <Input
              label="月度注資 (USDT)"
              type="number"
              inputMode="decimal"
              min="0"
              value={monthly}
              onChange={(e) => setMonthly(e.target.value)}
              hint="留 0 表示不定期補倉"
            />
          </div>

          {err && <div className="mt-3 text-xs text-qs-danger">{err}</div>}

          <div className="mt-6 flex justify-end">
            <Button onClick={onSubmit} loading={submitting}>建立實例</Button>
          </div>
        </Card>
      )}
    </div>
  );
}
