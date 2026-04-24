import type { InputHTMLAttributes } from 'react';

interface Props extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  hint?: string;
  error?: string;
}

export function Input({ label, hint, error, className = '', ...rest }: Props) {
  return (
    <label className="block">
      {label && (
        <div className="mb-1 text-xs font-medium text-slate-400 tracking-wide">{label}</div>
      )}
      <input
        {...rest}
        className={`qs-input w-full rounded-md border border-slate-700 bg-slate-900/80 px-3 py-2 text-sm text-slate-200 placeholder:text-slate-600 transition-colors ${className}`}
      />
      {hint && !error && <div className="mt-1 text-xs text-slate-500">{hint}</div>}
      {error && <div className="mt-1 text-xs text-qs-danger">{error}</div>}
    </label>
  );
}
