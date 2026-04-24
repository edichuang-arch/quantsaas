import { NavLink } from 'react-router-dom';
import { Activity } from 'lucide-react';
import { navItems } from '@/shared/config/navigation';

export function Sidebar() {
  const main = navItems.filter((i) => i.placement === 'main');
  const footer = navItems.filter((i) => i.placement === 'footer');

  return (
    <aside className="hidden h-screen w-16 shrink-0 flex-col border-r-2 border-[#0a0f1c] bg-[#020617]/40 backdrop-blur-xl lg:flex lg:w-64">
      {/* 品牌区 */}
      <div className="flex h-16 items-center border-b border-white/5 px-4">
        <div className="flex items-center gap-2.5">
          <div className="relative flex h-8 w-8 items-center justify-center rounded-md bg-qs-warm/10 text-qs-warm shadow-glow">
            <Activity className="h-4 w-4" />
          </div>
          <div className="hidden text-sm font-semibold tracking-wide text-slate-200 lg:block">
            Quant<span className="text-qs-accent">SaaS</span>
          </div>
        </div>
      </div>

      {/* 导航 */}
      <nav className="flex-1 space-y-0.5 overflow-y-auto p-2 custom-scrollbar">
        {main.map((item) => (
          <NavLink key={item.to} to={item.to} end={item.end} className={linkClass}>
            <item.icon className="h-4 w-4" />
            <span className="hidden lg:block">{item.label}</span>
          </NavLink>
        ))}
      </nav>

      <div className="space-y-0.5 border-t border-white/5 p-2">
        {footer.map((item) => (
          <NavLink key={item.to} to={item.to} className={linkClass}>
            <item.icon className="h-4 w-4" />
            <span className="hidden lg:block">{item.label}</span>
          </NavLink>
        ))}
      </div>
    </aside>
  );
}

const linkClass = ({ isActive }: { isActive: boolean }) =>
  [
    'flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors duration-150',
    isActive
      ? 'border border-qs-accent/20 bg-qs-accent/[0.06] text-qs-accent'
      : 'border border-transparent text-slate-500 hover:bg-white/[0.04] hover:text-slate-200',
  ].join(' ');
