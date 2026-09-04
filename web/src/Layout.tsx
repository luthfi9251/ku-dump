import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { Archive, Database, HardDrive, ListChecks, LogOut, Settings, ShieldCheck } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useAuth } from './auth'
import { api } from './lib/api'
import type { JobDTO } from './lib/types'

const links = [
  { to: '/', label: 'Databases', icon: Database },
  { to: '/dumps', label: 'Dumps', icon: Archive },
  { to: '/jobs', label: 'Jobs', icon: ListChecks },
  { to: '/settings', label: 'Settings', icon: Settings },
]

export default function Layout() {
  const { me, logout } = useAuth()
  const navigate = useNavigate()

  const jobs = useQuery({
    queryKey: ['jobs-summary'],
    queryFn: () => api.get<JobDTO[]>('/api/jobs?limit=50'),
    refetchInterval: 10000,
  })

  const runningCount = jobs.data?.filter((j) => j.status === 'pending' || j.status === 'running').length ?? 0

  return (
    <div className="flex min-h-screen bg-slate-950 text-slate-100 selection:bg-indigo-500/30 selection:text-indigo-200">
      {/* Sidebar Navigation */}
      <aside className="fixed inset-y-0 left-0 z-30 flex w-64 flex-col border-r border-slate-800/80 bg-slate-900/90 backdrop-blur-xl">
        {/* Brand Header */}
        <div className="flex items-center gap-3 border-b border-slate-800/80 px-6 py-5">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-tr from-indigo-600 to-violet-500 shadow-md shadow-indigo-600/30">
            <HardDrive size={20} className="text-white" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-bold tracking-tight text-white text-base">ku-dump</span>
              <span className="rounded bg-indigo-500/10 px-1.5 py-0.5 text-[10px] font-semibold text-indigo-400 border border-indigo-500/20">
                v1.0
              </span>
            </div>
            <p className="text-[11px] text-slate-400">Database Backup Manager</p>
          </div>
        </div>

        {/* Main Navigation Links */}
        <nav className="flex-1 space-y-1.5 px-3 py-6">
          {links.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              className={({ isActive }) => cnNav(isActive)}
            >
              <Icon size={18} className="transition-transform group-hover:scale-110" />
              <span className="flex-1 font-medium">{label}</span>
              {label === 'Jobs' && runningCount > 0 && (
                <span className="flex items-center gap-1 rounded-full bg-amber-500/20 px-2 py-0.5 text-[11px] font-semibold text-amber-400 border border-amber-500/30 animate-pulse">
                  <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
                  {runningCount} active
                </span>
              )}
            </NavLink>
          ))}
        </nav>

        {/* User Footer */}
        <div className="border-t border-slate-800/80 p-4">
          <div className="flex items-center justify-between rounded-xl bg-slate-950/60 p-3 border border-slate-800/60">
            <div className="flex items-center gap-2.5 overflow-hidden">
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-indigo-950 border border-indigo-800/50 text-indigo-400">
                <ShieldCheck size={16} />
              </div>
              <div className="truncate">
                <div className="truncate text-xs font-semibold text-slate-200">{me?.username || 'admin'}</div>
                <div className="text-[10px] text-slate-500 uppercase tracking-wider">Administrator</div>
              </div>
            </div>
            <button
              title="Logout"
              className="rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-slate-800 hover:text-rose-400"
              onClick={async () => {
                await logout()
                navigate('/login')
              }}
            >
              <LogOut size={16} />
            </button>
          </div>
        </div>
      </aside>

      {/* Main Content Area */}
      <div className="pl-64 flex-1 flex flex-col min-w-0">
        <main className="flex-1 p-8 max-w-7xl w-full mx-auto">
          <Outlet />
        </main>
      </div>
    </div>
  )
}

function cnNav(active: boolean): string {
  return `group flex items-center gap-3 rounded-xl px-3.5 py-2.5 text-sm transition-all duration-150 ${
    active
      ? 'bg-gradient-to-r from-indigo-600/20 to-violet-600/10 text-indigo-300 font-semibold border border-indigo-500/30 shadow-sm'
      : 'text-slate-400 hover:bg-slate-800/60 hover:text-slate-200'
  }`
}

