import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Trash2, ArrowUpRight, Dna } from 'lucide-react';
import { Card, CardHeader } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { StatusBadge } from '@/shared/ui/StatusBadge';
import { TableSkeleton } from '@/shared/ui/Skeleton';
import { instancesService } from '@/shared/services/instances';
import { fmtUSDT, fmtRelTime } from '@/shared/format';

export function InstanceListPage() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ['instances'],
    queryFn: instancesService.list,
    refetchInterval: 30_000,
  });
  const [confirmDel, setConfirmDel] = useState<number | null>(null);

  async function remove(id: number) {
    try {
      await instancesService.remove(id);
      qc.invalidateQueries({ queryKey: ['instances'] });
      setConfirmDel(null);
    } catch (e) {
      alert((e as Error).message);
    }
  }

  async function toggle(id: number, status: string) {
    try {
      if (status === 'RUNNING') await instancesService.stop(id);
      else await instancesService.start(id);
      qc.invalidateQueries({ queryKey: ['instances'] });
    } catch (e) {
      alert((e as Error).message);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold tracking-wide text-slate-200">我的實例</h1>
          <p className="mt-1 text-sm text-slate-500">你所有策略實例的集中管理頁</p>
        </div>
        <Link to="/instances/new">
          <Button><Plus className="h-4 w-4" /> 建立新實例</Button>
        </Link>
      </div>

      <Card>
        <CardHeader title="實例清單" subtitle={`共 ${data?.length ?? 0} 個`} />
        {isLoading ? (
          <TableSkeleton rows={4} />
        ) : (
          <div className="overflow-x-auto">
            <table className="min-w-full text-sm">
              <thead className="text-left text-xs uppercase tracking-wider text-slate-500">
                <tr className="border-b border-white/5">
                  <th className="py-3 pr-4">實例</th>
                  <th className="py-3 pr-4">交易對</th>
                  <th className="py-3 pr-4">狀態</th>
                  <th className="py-3 pr-4">初始資本</th>
                  <th className="py-3 pr-4">建立</th>
                  <th className="py-3 pr-4 text-right">操作</th>
                </tr>
              </thead>
              <tbody>
                {(data ?? []).map((i) => (
                  <tr key={i.ID} className="border-b border-white/5 last:border-0">
                    <td className="py-3 pr-4 text-slate-200">{i.Name}</td>
                    <td className="py-3 pr-4 font-mono text-slate-400">{i.Symbol}</td>
                    <td className="py-3 pr-4"><StatusBadge status={i.Status} /></td>
                    <td className="py-3 pr-4 font-mono text-slate-300">{fmtUSDT(i.InitialCapitalUSDT)}</td>
                    <td className="py-3 pr-4 text-xs text-slate-500">{fmtRelTime(i.CreatedAt)}</td>
                    <td className="py-3 pr-4">
                      <div className="flex items-center justify-end gap-1.5">
                        <Button size="sm" variant="secondary" onClick={() => toggle(i.ID, i.Status)}>
                          {i.Status === 'RUNNING' ? '暫停' : '啟動'}
                        </Button>
                        <Link to={`/?instance=${i.ID}`}>
                          <Button size="sm" variant="ghost"><ArrowUpRight className="h-3.5 w-3.5" /> 儀表板</Button>
                        </Link>
                        <Link to={`/evolution?instance=${i.ID}`}>
                          <Button size="sm" variant="ghost"><Dna className="h-3.5 w-3.5" /> 優化</Button>
                        </Link>
                        {confirmDel === i.ID ? (
                          <Button size="sm" variant="danger" onClick={() => remove(i.ID)}>
                            確認刪除
                          </Button>
                        ) : (
                          <Button size="sm" variant="ghost" onClick={() => setConfirmDel(i.ID)}>
                            <Trash2 className="h-3.5 w-3.5 text-qs-danger" />
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
                {(data ?? []).length === 0 && (
                  <tr>
                    <td colSpan={6} className="py-10 text-center text-sm text-slate-500">
                      還沒有實例,點擊右上「建立新實例」開始
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}
