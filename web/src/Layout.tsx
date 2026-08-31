import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { Archive, Database, ListChecks, LogOut, Settings } from 'lucide-react'
import { useAuth } from './auth'

const links = [
  { to: '/', label: 'Databases', icon: Database },
  { to: '/dumps', label: 'Dumps', icon: Archive },
  { to: '/jobs', label: 'Jobs', icon: ListChecks },
  { to: '/settings', label: 'Settings', icon: Settings },
]

export default function Layout() {
  const { me, logout } = useAuth()
  const navigate = useNavigate()
  return (
    <div className="flex min-h-screen bg-slate-50 text-slate-900">
      <aside className="flex w-56 flex-col border-r border-slate-200 bg-white">
        <div className="px-4 py-4 text-lg font-bold">ku-dump</div>
        <nav className="flex-1 space-y-1 px-2">
          {links.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              className={({ isActive }) =>
                cnNav(isActive)
              }
            >
              <Icon size={16} />
              {label}
            </NavLink>
          ))}
        </nav>
        <div className="border-t border-slate-200 p-4 text-sm">
          <div className="mb-2 truncate text-slate-500">{me?.username}</div>
          <button
            className="flex items-center gap-1.5 text-slate-500 hover:text-slate-800"
            onClick={async () => {
              await logout()
              navigate('/login')
            }}
          >
            <LogOut size={16} />
            Logout
          </button>
        </div>
      </aside>
      <main className="flex-1 p-6">
        <Outlet />
      </main>
    </div>
  )
}

function cnNav(active: boolean): string {
  return `flex items-center gap-2 rounded px-3 py-2 text-sm ${
    active ? 'bg-slate-900 text-white' : 'text-slate-600 hover:bg-slate-100'
  }`
}
