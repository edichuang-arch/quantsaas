import { useState } from 'react';
import { Card, CardHeader } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { EvolutionPanel } from './EvolutionPanel';
import { GenomeLibrary } from './GenomeLibrary';

export function EvolutionPage() {
  const [tab, setTab] = useState<'optimize' | 'library'>('optimize');

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-semibold tracking-wide text-slate-200">參數優化實驗室</h1>
        <p className="mt-1 text-sm text-slate-500">
          透過遺傳演算法在歷史數據上搜尋更優的策略參數（僅 lab / dev 模式開放）
        </p>
      </div>

      <div className="flex gap-2 border-b border-white/5">
        <TabButton active={tab === 'optimize'} onClick={() => setTab('optimize')}>參數優化</TabButton>
        <TabButton active={tab === 'library'} onClick={() => setTab('library')}>基因庫</TabButton>
      </div>

      {tab === 'optimize' ? <EvolutionPanel /> : <GenomeLibrary />}
    </div>
  );
}

function TabButton({ active, children, onClick }: { active: boolean; children: React.ReactNode; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className={`px-4 py-2 text-sm transition ${
        active
          ? 'border-b-2 border-qs-accent text-qs-accent'
          : 'border-b-2 border-transparent text-slate-500 hover:text-slate-200'
      }`}
    >
      {children}
    </button>
  );
}

export function TabCard({ title, subtitle, action, children }: { title: string; subtitle?: string; action?: React.ReactNode; children: React.ReactNode }) {
  return (
    <Card>
      <CardHeader title={title} subtitle={subtitle} action={action} />
      {children}
    </Card>
  );
}

export function EmptyState({ title, hint }: { title: string; hint: string }) {
  return (
    <Card className="text-center">
      <div className="py-8">
        <div className="text-sm font-medium text-slate-300">{title}</div>
        <div className="mt-1 text-xs text-slate-500">{hint}</div>
        <Button className="mt-4" size="sm" variant="secondary" onClick={() => window.location.reload()}>
          重新整理
        </Button>
      </div>
    </Card>
  );
}
