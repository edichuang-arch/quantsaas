import { Card, CardHeader } from '@/shared/ui/Card';
import { Beaker } from 'lucide-react';

export function BacktestingPage() {
  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-semibold tracking-wide text-slate-200">回測分析</h1>
        <p className="mt-1 text-sm text-slate-500">
          對指定的基因參數在歷史數據上重新跑一次回測,查看淨值曲線與各時段分解
        </p>
      </div>

      <Card>
        <div className="py-12 text-center">
          <div className="mx-auto mb-3 inline-flex h-12 w-12 items-center justify-center rounded-full bg-qs-info/10 text-qs-info">
            <Beaker className="h-5 w-5" />
          </div>
          <div className="text-sm font-medium text-slate-300">回測端點尚未實作</div>
          <div className="mt-1 text-xs text-slate-500">
            `/api/v1/backtests` 路由規劃在 Phase 11 之後由 Lab 模式觸發;
            <br />
            目前可在「參數優化」頁的冠軍卡片查看最新一輪的適應度分數。
          </div>
        </div>
      </Card>
    </div>
  );
}
