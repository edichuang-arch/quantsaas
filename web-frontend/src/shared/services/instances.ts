import { api } from './apiClient';

export interface Instance {
  ID: number;
  UserID: number;
  TemplateID: number;
  Name: string;
  Symbol: string;
  Status: 'RUNNING' | 'STOPPED' | 'ERROR';
  InitialCapitalUSDT: number;
  MonthlyInjectUSDT: number;
  LastProcessedBarTime: number;
  StartedAt?: string;
  StoppedAt?: string;
  CreatedAt: string;
}

export interface Portfolio {
  ID: number;
  InstanceID: number;
  USDTBalance: number;
  USDTFrozen: number;
  DeadStackAsset: number;
  FloatStackAsset: number;
  ColdSealedAsset: number;
  LastPriceUSDT: number;
  TotalEquity: number;
  UpdatedAt: string;
}

export interface TradeRecord {
  ID: number;
  InstanceID: number;
  ClientOrderID: string;
  Action: 'BUY' | 'SELL';
  Engine: 'MACRO' | 'MICRO';
  Symbol: string;
  LotType: 'DEAD_STACK' | 'FLOATING' | 'COLD_SEALED';
  FilledQty: number;
  FilledPrice: number;
  FilledUSDT: number;
  Fee: number;
  FeeAsset: string;
  CreatedAt: string;
}

export interface CreateInstancePayload {
  template_id: number;
  name: string;
  symbol: string;
  initial_capital_usdt: number;
  monthly_inject_usdt: number;
}

export const instancesService = {
  list: () => api.get<Instance[]>('/api/v1/instances'),
  create: (p: CreateInstancePayload) => api.post<Instance>('/api/v1/instances', p),
  start: (id: number) => api.post<{ status: string }>(`/api/v1/instances/${id}/start`),
  stop: (id: number) => api.post<{ status: string }>(`/api/v1/instances/${id}/stop`),
  remove: (id: number) => api.delete<{ status: string }>(`/api/v1/instances/${id}`),
  portfolio: (id: number) => api.get<Portfolio>(`/api/v1/instances/${id}/portfolio`),
  trades: (id: number) => api.get<TradeRecord[]>(`/api/v1/instances/${id}/trades`),
};
