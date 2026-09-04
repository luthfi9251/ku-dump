import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from 'react'
import { X } from 'lucide-react'
import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}

export type ButtonVariant = 'primary' | 'outline' | 'danger' | 'ghost' | 'secondary' | 'success'
export type ButtonSize = 'sm' | 'md' | 'lg'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
}

export function Button({
  variant = 'primary',
  size = 'md',
  className,
  children,
  ...props
}: ButtonProps) {
  const variants: Record<ButtonVariant, string> = {
    primary: 'bg-indigo-600 text-white hover:bg-indigo-500 active:bg-indigo-700 shadow-lg shadow-indigo-600/20 border border-indigo-500/30',
    secondary: 'bg-slate-800 text-slate-100 hover:bg-slate-700 active:bg-slate-850 border border-slate-700',
    outline: 'border border-slate-700 bg-slate-900/60 text-slate-200 hover:bg-slate-800 hover:text-white hover:border-slate-600',
    ghost: 'text-slate-400 hover:bg-slate-800/60 hover:text-slate-200',
    danger: 'bg-rose-600/90 text-white hover:bg-rose-500 active:bg-rose-700 shadow-lg shadow-rose-600/20 border border-rose-500/30',
    success: 'bg-emerald-600 text-white hover:bg-emerald-500 active:bg-emerald-700 shadow-lg shadow-emerald-600/20 border border-emerald-500/30',
  }

  const sizes: Record<ButtonSize, string> = {
    sm: 'px-2.5 py-1 text-xs gap-1.5 font-medium rounded-md',
    md: 'px-3.5 py-1.5 text-sm gap-2 font-medium rounded-lg',
    lg: 'px-4 py-2 text-base gap-2.5 font-medium rounded-lg',
  }

  return (
    <button
      {...props}
      className={cn(
        'inline-flex items-center justify-center transition-all duration-150 disabled:cursor-not-allowed disabled:opacity-50 select-none focus:outline-none focus:ring-2 focus:ring-indigo-500/40',
        variants[variant],
        sizes[size],
        className,
      )}
    >
      {children}
    </button>
  )
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={cn(
        'w-full rounded-lg border border-slate-700/80 bg-slate-900/90 px-3 py-2 text-sm text-slate-100 placeholder-slate-500 transition-all duration-150 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/30 disabled:opacity-50',
        className,
      )}
    />
  )
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...props}
      className={cn(
        'w-full rounded-lg border border-slate-700/80 bg-slate-900 px-3 py-2 text-sm text-slate-100 transition-all duration-150 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/30 disabled:opacity-50',
        className,
      )}
    />
  )
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <div className="flex items-center justify-between">
        <span className="text-xs font-semibold uppercase tracking-wider text-slate-300">{label}</span>
        {hint && <span className="text-xs text-slate-500">{hint}</span>}
      </div>
      {children}
    </label>
  )
}

export function Modal({
  open,
  onClose,
  title,
  subtitle,
  children,
  wide,
}: {
  open: boolean
  onClose: () => void
  title: string
  subtitle?: string
  children: ReactNode
  wide?: boolean
}) {
  if (!open) return null
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/80 backdrop-blur-md p-4 transition-opacity"
      onClick={onClose}
    >
      <div
        className={cn(
          'animate-fade-in relative max-h-[90vh] w-full overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/95 p-6 text-slate-100 shadow-2xl shadow-black/80 flex flex-col',
          wide ? 'max-w-2xl' : 'max-w-lg',
        )}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-5 flex items-start justify-between border-b border-slate-800/80 pb-4">
          <div>
            <h2 className="text-lg font-bold text-slate-100">{title}</h2>
            {subtitle && <p className="mt-0.5 text-xs text-slate-400">{subtitle}</p>}
          </div>
          <button
            onClick={onClose}
            className="rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-slate-800 hover:text-slate-200"
          >
            <X size={18} />
          </button>
        </div>
        <div className="flex-1 overflow-y-auto pr-1">{children}</div>
      </div>
    </div>
  )
}

export type BadgeTone = 'green' | 'red' | 'amber' | 'blue' | 'slate' | 'violet' | 'indigo'

export function Badge({
  tone,
  children,
  pulse = false,
  className,
}: {
  tone: BadgeTone
  children: ReactNode
  pulse?: boolean
  className?: string
}) {
  const tones: Record<BadgeTone, { bg: string; text: string; border: string; dot: string }> = {
    green: { bg: 'bg-emerald-950/60', text: 'text-emerald-400', border: 'border-emerald-800/50', dot: 'bg-emerald-400' },
    red: { bg: 'bg-rose-950/60', text: 'text-rose-400', border: 'border-rose-800/50', dot: 'bg-rose-400' },
    amber: { bg: 'bg-amber-950/60', text: 'text-amber-400', border: 'border-amber-800/50', dot: 'bg-amber-400' },
    blue: { bg: 'bg-sky-950/60', text: 'text-sky-400', border: 'border-sky-800/50', dot: 'bg-sky-400' },
    indigo: { bg: 'bg-indigo-950/60', text: 'text-indigo-400', border: 'border-indigo-800/50', dot: 'bg-indigo-400' },
    slate: { bg: 'bg-slate-800/80', text: 'text-slate-300', border: 'border-slate-700/60', dot: 'bg-slate-400' },
    violet: { bg: 'bg-violet-950/60', text: 'text-violet-400', border: 'border-violet-800/50', dot: 'bg-violet-400' },
  }

  const t = tones[tone]
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold tracking-wide uppercase',
        t.bg,
        t.text,
        t.border,
        className,
      )}
    >
      {pulse && <span className={cn('h-1.5 w-1.5 rounded-full animate-pulse-dot', t.dot)} />}
      {children}
    </span>
  )
}

export function StatCard({
  title,
  value,
  description,
  icon: Icon,
  tone = 'indigo',
}: {
  title: string
  value: ReactNode
  description?: string
  icon?: React.ComponentType<{ size?: number; className?: string }>
  tone?: 'indigo' | 'emerald' | 'sky' | 'violet' | 'amber'
}) {
  const iconColors: Record<string, string> = {
    indigo: 'bg-indigo-500/10 text-indigo-400 border-indigo-500/20',
    emerald: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20',
    sky: 'bg-sky-500/10 text-sky-400 border-sky-500/20',
    violet: 'bg-violet-500/10 text-violet-400 border-violet-500/20',
    amber: 'bg-amber-500/10 text-amber-400 border-amber-500/20',
  }

  return (
    <div className="relative overflow-hidden rounded-xl border border-slate-800/80 bg-slate-900/80 p-4 backdrop-blur-md shadow-lg transition-all hover:border-slate-700">
      <div className="flex items-center justify-between">
        <span className="text-xs font-semibold uppercase tracking-wider text-slate-400">{title}</span>
        {Icon && (
          <div className={cn('rounded-lg border p-2', iconColors[tone])}>
            <Icon size={18} />
          </div>
        )}
      </div>
      <div className="mt-2 text-2xl font-bold tracking-tight text-slate-100">{value}</div>
      {description && <div className="mt-1 text-xs text-slate-500">{description}</div>}
    </div>
  )
}

export function Spinner({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        'inline-block h-4 w-4 animate-spin rounded-full border-2 border-indigo-400/30 border-t-indigo-400',
        className,
      )}
    />
  )
}

