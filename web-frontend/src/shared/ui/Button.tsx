import type { ButtonHTMLAttributes } from 'react';

type Variant = 'primary' | 'secondary' | 'danger' | 'ghost';
type Size = 'sm' | 'md' | 'lg';

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  loading?: boolean;
}

const VARIANT: Record<Variant, string> = {
  primary:
    'bg-qs-accent/90 text-slate-900 border-qs-accent hover:bg-qs-accent disabled:opacity-50',
  secondary:
    'bg-white/5 text-slate-200 border-white/10 hover:bg-white/10 disabled:opacity-50',
  danger:
    'bg-qs-danger/90 text-slate-950 border-qs-danger hover:bg-qs-danger disabled:opacity-50',
  ghost:
    'bg-transparent text-slate-400 border-transparent hover:text-slate-200 hover:bg-white/5',
};

const SIZE: Record<Size, string> = {
  sm: 'text-xs px-2.5 py-1',
  md: 'text-sm px-4 py-2',
  lg: 'text-sm px-5 py-2.5',
};

export function Button({
  variant = 'primary',
  size = 'md',
  loading,
  disabled,
  children,
  className = '',
  ...rest
}: Props) {
  return (
    <button
      disabled={disabled || loading}
      className={`inline-flex items-center justify-center gap-1.5 rounded-md border font-medium tracking-wide transition-colors duration-150 ${VARIANT[variant]} ${SIZE[size]} ${className}`}
      {...rest}
    >
      {loading && (
        <svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none">
          <circle cx="12" cy="12" r="10" stroke="currentColor" strokeOpacity="0.3" strokeWidth="3" />
          <path d="M22 12a10 10 0 0 0-10-10" stroke="currentColor" strokeWidth="3" />
        </svg>
      )}
      {children}
    </button>
  );
}
