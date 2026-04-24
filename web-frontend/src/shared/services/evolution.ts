import { api } from './apiClient';

export interface EvolutionTask {
  ID: number;
  StrategyID: string;
  Symbol: string;
  Status: 'pending' | 'running' | 'done' | 'failed' | 'aborted';
  PopSize: number;
  MaxGenerations: number;
  CurrentGen: number;
  BestScore: number;
  ErrorMessage?: string;
  StartedAt: string;
  FinishedAt?: string;
  CreatedAt: string;
}

export interface GeneRecord {
  ID: number;
  StrategyID: string;
  Symbol: string;
  Role: 'challenger' | 'champion' | 'retired';
  TaskID?: number;
  ScoreTotal: number;
  MaxDrawdown: number;
  WindowScores: Record<string, number>;
  ParamPack: unknown;
  ActivatedAt?: string;
  RetiredAt?: string;
  CreatedAt: string;
}

export interface CreateTaskPayload {
  strategy_id: string;
  symbol: string;
  pop_size?: number;
  max_generations?: number;
  spawn_mode?: 'inherit' | 'random_once' | 'manual';
  test_mode?: boolean;
  initial_usdt: number;
  monthly_inject: number;
  lot_step: number;
  lot_min: number;
  warmup_days: number;
}

export const evolutionService = {
  createTask: (p: CreateTaskPayload) =>
    api.post<EvolutionTask>('/api/v1/evolution/tasks', p),
  getStatus: (strategyId = 'sigmoid-btc', symbol = 'BTCUSDT') =>
    api.get<{ current?: EvolutionTask; challengers: GeneRecord[] }>(
      `/api/v1/evolution/tasks?strategy_id=${strategyId}&symbol=${symbol}`
    ),
  promote: (challengerId: number) =>
    api.post<{ status: string }>('/api/v1/evolution/tasks/0/promote', {
      challenger_id: challengerId,
    }),
  champion: (strategyId = 'sigmoid-btc', symbol = 'BTCUSDT') =>
    api.get<GeneRecord | null>(
      `/api/v1/genome/champion?strategy_id=${strategyId}&symbol=${symbol}`
    ),
};
