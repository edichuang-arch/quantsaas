import { Routes, Route, Navigate } from 'react-router-dom';
import { AuthGate } from './AuthGate';
import { AppShell } from './AppShell';
import { LoginPage } from '@/features/auth/LoginPage';
import { RegisterPage } from '@/features/auth/RegisterPage';
import { DashboardPage } from '@/features/dashboard/DashboardPage';
import { TemplatesPage } from '@/features/strategies/TemplatesPage';
import { InstanceListPage } from '@/features/strategies/InstanceListPage';
import { InstanceCreatePage } from '@/features/strategies/InstanceCreatePage';
import { EvolutionPage } from '@/features/evolution/EvolutionPage';
import { BacktestingPage } from '@/features/backtesting/BacktestingPage';
import { AgentsPage } from '@/features/agents/AgentsPage';
import { SettingsPage } from '@/features/settings/SettingsPage';

export function AppRouter() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />

      <Route element={<AuthGate />}>
        <Route
          path="/*"
          element={
            <AppShell>
              <ShellRoutes />
            </AppShell>
          }
        />
      </Route>
    </Routes>
  );
}

function ShellRoutes() {
  return (
    <Routes>
      <Route index element={<DashboardPage />} />
      <Route path="templates" element={<TemplatesPage />} />
      <Route path="instances" element={<InstanceListPage />} />
      <Route path="instances/new" element={<InstanceCreatePage />} />
      <Route path="evolution" element={<EvolutionPage />} />
      <Route path="backtesting" element={<BacktestingPage />} />
      <Route path="agents" element={<AgentsPage />} />
      <Route path="settings" element={<SettingsPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
