// 策略模板 UI 展示配置（前端静态数据，不从后端拉）。

export interface StrategyMeta {
  id: string;
  name: string;
  description: string;
  exchange: string;
  symbol: string;
  accent: string; // tailwind 色
  supportsEvolution: boolean;
}

export const strategyCatalog: StrategyMeta[] = [
  {
    id: 'sigmoid-btc',
    name: 'Sigmoid 動態均衡 · BTC',
    description:
      '以 Sigmoid 函數計算目標倉位權重,宏觀 DCA 建立底倉,微觀動態調整活躍倉位。適合長週期累積且耐受震盪。',
    exchange: 'binance',
    symbol: 'BTCUSDT',
    accent: '#2dd4bf',
    supportsEvolution: true,
  },
];

export function findStrategy(id: string): StrategyMeta | undefined {
  return strategyCatalog.find((s) => s.id === id);
}
