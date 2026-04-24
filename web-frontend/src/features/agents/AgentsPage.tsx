import { useQuery } from '@tanstack/react-query';
import { Radio, Download, FileKey, CheckCircle2 } from 'lucide-react';
import { Card, CardHeader } from '@/shared/ui/Card';
import { dashboardService } from '@/shared/services/dashboard';

export function AgentsPage() {
  const { data } = useQuery({
    queryKey: ['agent_status'],
    queryFn: dashboardService.agentStatus,
    refetchInterval: 10_000,
  });

  const online = data?.online ?? false;

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-semibold tracking-wide text-slate-200">執行端</h1>
        <p className="mt-1 text-sm text-slate-500">
          執行端是本地運行的輕量程式,持有交易所 API Key(永不上傳雲端)並執行雲端下發的指令
        </p>
      </div>

      {/* 連線狀態 */}
      <Card>
        <div className="flex items-center gap-4">
          <div className="relative">
            <div
              className={`flex h-12 w-12 items-center justify-center rounded-full ${
                online ? 'bg-qs-accent/15 text-qs-accent' : 'bg-slate-700/30 text-slate-500'
              }`}
            >
              <Radio className="h-5 w-5" />
            </div>
            {online && (
              <span className="absolute right-0 top-0 h-3 w-3 rounded-full bg-qs-accent shadow-glow">
                <span className="absolute inset-0 animate-ping rounded-full bg-qs-accent opacity-75" />
              </span>
            )}
          </div>
          <div>
            <div className="text-sm font-medium text-slate-200">
              {online ? '執行端已連線' : '執行端未連線'}
            </div>
            <div className="text-xs text-slate-500">
              {online ? '雲端指令可以被即時執行' : '交易將暫停,請先啟動本地執行端'}
            </div>
          </div>
        </div>
      </Card>

      {/* 設定指引 */}
      <Card>
        <CardHeader title="如何啟動執行端" subtitle="三個步驟,一次設定" />
        <ol className="space-y-4">
          <StepItem
            n={1}
            title="下載 LocalAgent 二進制"
            body={
              <>
                從專案根目錄執行 <code className="rounded bg-slate-800 px-1.5 py-0.5 font-mono text-xs text-qs-accent">make build-agent</code>,
                產出 <code className="rounded bg-slate-800 px-1.5 py-0.5 font-mono text-xs text-qs-accent">bin/agent</code>。
              </>
            }
            icon={Download}
          />
          <StepItem
            n={2}
            title="建立 config.agent.yaml"
            body={
              <>
                複製 <code className="rounded bg-slate-800 px-1.5 py-0.5 font-mono text-xs">config.agent.yaml.example</code> 為
                {' '}<code className="rounded bg-slate-800 px-1.5 py-0.5 font-mono text-xs">config.agent.yaml</code>,填入 Binance API Key 與
                SaaS 帳戶 email / 密碼。
                <div className="mt-2 rounded-md border border-qs-warm/30 bg-qs-warm/5 p-3 text-xs text-slate-300">
                  <strong className="text-qs-warm">鐵律:</strong> API Key 僅存在本地,系統不會上傳到雲端或寫入資料庫。請確保 <code className="font-mono">config.agent.yaml</code> 已在 <code className="font-mono">.gitignore</code> 中。
                </div>
              </>
            }
            icon={FileKey}
          />
          <StepItem
            n={3}
            title="啟動執行端"
            body={
              <>
                終端執行 <code className="rounded bg-slate-800 px-1.5 py-0.5 font-mono text-xs">./bin/agent -config config.agent.yaml</code>,
                它會自動登入 SaaS、建立 WebSocket 長連線、上報餘額快照,並等待指令。
              </>
            }
            icon={CheckCircle2}
          />
        </ol>
      </Card>
    </div>
  );
}

function StepItem({ n, title, body, icon: Icon }: { n: number; title: string; body: React.ReactNode; icon: typeof Download }) {
  return (
    <li className="flex gap-4">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-white/5 bg-slate-900/40 text-xs font-mono text-qs-accent">
        {n}
      </div>
      <div className="flex-1">
        <div className="flex items-center gap-1.5 text-sm font-medium text-slate-200">
          <Icon className="h-3.5 w-3.5 text-qs-accent" /> {title}
        </div>
        <div className="mt-1 text-sm leading-relaxed text-slate-400">{body}</div>
      </div>
    </li>
  );
}
