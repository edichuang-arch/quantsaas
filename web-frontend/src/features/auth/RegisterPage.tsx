import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Activity } from 'lucide-react';
import { AuthScaffold } from '@/app/AppShell';
import { Card } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { authService } from '@/shared/services/auth';
import { useAuthStore } from '@/stores/authStore';

export function RegisterPage() {
  const nav = useNavigate();
  const { login } = useAuthStore();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    if (password !== confirm) {
      setErr('兩次密碼不一致');
      return;
    }
    if (password.length < 6) {
      setErr('密碼至少 6 個字元');
      return;
    }
    setLoading(true);
    try {
      const resp = await authService.register(email, password);
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
          <div className="text-lg font-semibold tracking-wide text-slate-200">建立你的帳戶</div>
          <div className="mt-1 text-xs text-slate-500">
            免費註冊,開始你的量化實驗
          </div>
        </div>

        <form className="space-y-3" onSubmit={onSubmit}>
          <Input
            label="電子郵件"
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            disabled={loading}
          />
          <Input
            label="密碼"
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            hint="至少 6 個字元"
            disabled={loading}
          />
          <Input
            label="確認密碼"
            type="password"
            required
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            disabled={loading}
          />

          {err && <div className="text-xs text-qs-danger">{err}</div>}

          <Button type="submit" loading={loading} className="w-full uppercase tracking-wider">
            註冊
          </Button>
        </form>

        <div className="mt-5 text-center text-xs text-slate-500">
          已有帳號?{' '}
          <Link to="/login" className="text-qs-accent hover:underline">
            前往登入
          </Link>
        </div>
      </Card>
    </AuthScaffold>
  );
}
