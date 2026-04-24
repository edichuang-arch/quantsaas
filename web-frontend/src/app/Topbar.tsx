import { useQuery } from '@tanstack/react-query';
import { LogOut, KeyRound, Radio, Activity } from 'lucide-react';
import { useAuthStore } from '@/stores/authStore';
import { dashboardService } from '@/shared/services/dashboard';

export function Topbar() {
  const { user, logout } = useAuthStore();

  const { data: sys } = useQuery({
    queryKey: ['system_status'],
    queryFn: dashboardService.systemStatus,
    refetchInterval: 30_000,
  });

  return (
    <header className="flex h-16 items-center justify-between border-b border-white/5 bg-[#020617]/60 px-4 backdrop-blur-xl lg:px-6">
      <div className="flex items-center gap-5">
        <StatusDot
          icon={KeyRound}
          ok={sys?.api_connected ?? false}
          okLabel="密鑰已配置"
          offLabel="密鑰未設定"
        />
        <StatusDot
          icon={Radio}
          ok={sys?.api_connected ?? false}
          okLabel="執行端已連線"
          offLabel="執行端離線"
        />
        <EngineDot role={sys?.app_role} />
      </div>

      <div className="flex items-center gap-3 text-sm">
        <div className="hidden text-right sm:block">
          <div className="text-xs text-slate-500">登入帳戶</div>
          <div className="font-mono text-xs text-slate-300">{user?.email ?? '...'}</div>
        </div>
        <button
          onClick={logout}
          className="inline-flex h-9 w-9 items-center justify-center rounded-md border border-white/10 text-slate-400 transition hover:bg-white/5 hover:text-slate-200"
          aria-label="logout"
        >
          <LogOut className="h-4 w-4" />
        </button>
      </div>
    </header>
  );
}

function StatusDot({
  icon: Icon,
  ok,
  okLabel,
  offLabel,
}: {
  icon: typeof KeyRound;
  ok: boolean;
  okLabel: string;
  offLabel: string;
}) {
  return (
    <div
      className={`hidden items-center gap-1.5 text-xs md:inline-flex ${ok ? 'text-qs-safe' : 'text-slate-500'}`}
      title={ok ? okLabel : offLabel}
    >
      <Icon className="h-3.5 w-3.5" />
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${ok ? 'bg-qs-safe' : 'bg-slate-600'}`} />
      {ok ? okLabel : offLabel}
    </div>
  );
}

function EngineDot({ role }: { role?: string }) {
  const color =
    role === 'saas' ? 'text-qs-safe bg-qs-safe' :
    role === 'lab' ? 'text-qs-warn bg-qs-warn' :
    'text-qs-accent bg-qs-accent';
  return (
    <div className="inline-flex items-center gap-1.5 text-xs text-slate-300">
      <Activity className="h-3.5 w-3.5 text-qs-warm" />
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${color.split(' ')[1]}`} />
      {role ? role.toUpperCase() : 'DEV'}
    </div>
  );
}
