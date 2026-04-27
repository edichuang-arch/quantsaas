import { LayoutDashboard, Package, ListChecks, Dna, Plug, Beaker, Settings, ScrollText } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

export interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  placement: 'main' | 'footer';
  end?: boolean;
}

export const navItems: NavItem[] = [
  { to: '/', label: '總覽', icon: LayoutDashboard, placement: 'main', end: true },
  { to: '/templates', label: '策略模板', icon: Package, placement: 'main' },
  { to: '/instances', label: '我的實例', icon: ListChecks, placement: 'main' },
  { to: '/trades', label: '成交紀錄', icon: ScrollText, placement: 'main' },
  { to: '/evolution', label: '參數優化', icon: Dna, placement: 'main' },
  { to: '/backtesting', label: '回測分析', icon: Beaker, placement: 'main' },
  { to: '/agents', label: '執行端', icon: Plug, placement: 'main' },
  { to: '/settings', label: '帳戶設定', icon: Settings, placement: 'footer' },
];
