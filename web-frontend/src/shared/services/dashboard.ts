import { api } from './apiClient';
import type { Instance, Portfolio } from './instances';

export interface DashboardSummary {
  instance: Instance;
  portfolio?: Portfolio;
  recent_trade_count: number;
}

export interface SystemStatus {
  app_role: string;
  api_connected: boolean;
  online_total: number;
  server_time: number;
}

export interface AgentStatus {
  online: boolean;
  user_id: number;
  api_configured: boolean;
}

export const dashboardService = {
  overview: () => api.get<DashboardSummary[]>('/api/v1/dashboard'),
  systemStatus: () => api.get<SystemStatus>('/api/v1/system/status'),
  agentStatus: () => api.get<AgentStatus>('/api/v1/agents/status'),
};
