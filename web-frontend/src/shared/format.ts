// 数字格式化工具。

export function fmtUSDT(x: number | undefined | null): string {
  if (x === undefined || x === null || Number.isNaN(x)) return '—';
  return x.toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

export function fmtAsset(x: number | undefined | null): string {
  if (x === undefined || x === null || Number.isNaN(x)) return '—';
  return x.toLocaleString('en-US', {
    minimumFractionDigits: 6,
    maximumFractionDigits: 6,
  });
}

export function fmtPercent(x: number | undefined | null): string {
  if (x === undefined || x === null || Number.isNaN(x)) return '—';
  return `${(x * 100).toFixed(2)}%`;
}

export function fmtRelTime(iso?: string): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  const diff = Date.now() - t;
  const min = Math.floor(diff / 60_000);
  if (min < 1) return '剛剛';
  if (min < 60) return `${min} 分鐘前`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr} 小時前`;
  const day = Math.floor(hr / 24);
  if (day < 30) return `${day} 天前`;
  return new Date(iso).toLocaleDateString('zh-TW');
}
