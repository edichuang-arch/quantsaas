import { Card, CardHeader } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { useAuthStore } from '@/stores/authStore';
import { LogOut } from 'lucide-react';

export function SettingsPage() {
  const { user, logout } = useAuthStore();

  return (
    <div className="mx-auto max-w-2xl space-y-4">
      <div>
        <h1 className="text-lg font-semibold tracking-wide text-slate-200">帳戶設定</h1>
        <p className="mt-1 text-sm text-slate-500">查看與管理你的 QuantSaaS 帳戶資訊</p>
      </div>

      <Card>
        <CardHeader title="個人資料" subtitle="此版本僅展示,未來會加入修改與訂閱升級" />
        <div className="space-y-3 text-sm">
          <Row label="電子郵件" value={user?.email ?? '—'} mono />
          <Row label="訂閱計畫" value={user?.plan ?? 'free'} />
          <Row label="實例配額" value={`${user?.max_instances ?? 1} 個`} />
        </div>
      </Card>

      <Card>
        <CardHeader title="登出" subtitle="清除本地 token,返回登入頁" />
        <Button variant="danger" onClick={logout}><LogOut className="h-3.5 w-3.5" /> 登出</Button>
      </Card>
    </div>
  );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between border-b border-white/5 py-2 last:border-0">
      <div className="text-slate-500">{label}</div>
      <div className={mono ? 'font-mono text-slate-200' : 'text-slate-200'}>{value}</div>
    </div>
  );
}
