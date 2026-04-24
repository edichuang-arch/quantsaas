import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Activity } from 'lucide-react';
import { AuthScaffold } from '@/app/AppShell';
import { Card } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { authService } from '@/shared/services/auth';
import { useAuthStore } from '@/stores/authStore';

export function LoginPage() {
  const nav = useNavigate();
  const { login } = useAuthStore();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    setLoading(true);
    try {
      const resp = await authService.login(email, password);
      login(resp.token, { user_id: resp.user_id, email: resp.email, plan: resp.plan });
      nav('/', { replace: true });
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthScaffold>
      <Card className="w-full max-w-[400px] !bg-slate-900/60 backdrop-blur-xl">
        <div className="mb-6 text-center">
          <div className="mx-auto mb-3 inline-flex h-12 w-12 items-center justify-center rounded-lg bg-qs-warm/10 text-qs-warm shadow-glow">
            <Activity className="h-5 w-5" />
          </div>
          <div className="text-lg font-semibold tracking-wide text-slate-200">
            Quant<span className="text-qs-accent">SaaS</span>
          </div>
          <div className="mt-1 text-xs text-slate-500 tracking-wide">
            登入你的智能水庫控制台
          </div>
        </div>

        <form className="space-y-3" onSubmit={onSubmit}>
          <Input
            label="電子郵件"
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            disabled={loading}
          />
          <Input
            label="密碼"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={loading}
          />

          {err && <div className="text-xs text-qs-danger">{err}</div>}

          <Button type="submit" loading={loading} className="w-full uppercase tracking-wider">
            登入
          </Button>
        </form>

        <div className="mt-5 text-center text-xs text-slate-500">
          還沒有帳號?{' '}
          <Link to="/register" className="text-qs-accent hover:underline">
            立即註冊
          </Link>
        </div>
      </Card>
    </AuthScaffold>
  );
}
