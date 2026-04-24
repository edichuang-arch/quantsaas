import { Link } from 'react-router-dom';
import { Card, CardHeader } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { strategyCatalog } from '@/shared/config/strategyCatalog';
import { CheckCircle2, Dna } from 'lucide-react';

export function TemplatesPage() {
  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-semibold tracking-wide text-slate-200">策略模板</h1>
        <p className="mt-1 text-sm text-slate-500">
          從官方策略庫選擇一個模板,基於它建立你的交易實例
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {strategyCatalog.map((s) => (
          <Card key={s.id} className="flex flex-col">
            <div
              className="-mx-5 -mt-4 mb-4 h-1 rounded-t-xl"
              style={{ backgroundColor: s.accent }}
            />
            <CardHeader
              title={s.name}
              subtitle={<span className="font-mono">{s.exchange.toUpperCase()} · {s.symbol}</span>}
            />
            <p className="flex-1 text-sm leading-relaxed text-slate-400">
              {s.description}
            </p>
            <div className="mt-4 flex items-center justify-between text-xs">
              <div className="flex items-center gap-1.5 text-qs-safe">
                <CheckCircle2 className="h-3.5 w-3.5" /> 現貨策略
              </div>
              {s.supportsEvolution && (
                <div className="flex items-center gap-1.5 text-qs-accent">
                  <Dna className="h-3.5 w-3.5" /> 支援參數優化
                </div>
              )}
            </div>
            <Link to={`/instances/new?template=${s.id}`} className="mt-4">
              <Button className="w-full">建立實例</Button>
            </Link>
          </Card>
        ))}
      </div>
    </div>
  );
}
