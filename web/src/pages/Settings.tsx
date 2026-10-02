import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, Terminal } from 'lucide-react'
import { api } from '../lib/api'
import type { ToolsDTO } from '../lib/types'
import { Badge, Spinner } from '../ui'

export default function Settings() {
  const tools = useQuery({ queryKey: ['tools'], queryFn: () => api.get<ToolsDTO>('/api/tools') })

  return (
    <div className="max-w-3xl space-y-8">
      {/* Page Title */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-white">System Settings</h1>
        <p className="mt-1 text-xs text-slate-400">
          Inspect host CLI tools. Storage destinations moved to their own{' '}
          <a href="/destinations" className="text-indigo-400 hover:underline">Destinations page</a>.
        </p>
      </div>

      {/* CLI Tools Section */}
      <section className="rounded-2xl border border-slate-800 bg-slate-900/80 p-6 shadow-2xl backdrop-blur-md space-y-4">
        <div className="flex items-center gap-3 border-b border-slate-800 pb-4">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-violet-500/10 text-violet-400 border border-violet-500/20">
            <Terminal size={20} />
          </div>
          <div>
            <h2 className="text-base font-bold text-slate-100">Host CLI Tool Diagnostics</h2>
            <p className="text-xs text-slate-400">
              `ku-dump` delegates execution to native engine CLI tools installed on the host system.
            </p>
          </div>
        </div>

        <div className="space-y-3">
          <ToolRow name="PostgreSQL Tools" missing={tools.data?.postgres} pkgs="postgresql-client (pg_dump, pg_restore)" />
          <ToolRow name="MongoDB Tools" missing={tools.data?.mongodb} pkgs="mongodb-database-tools (mongodump, mongorestore)" />
        </div>
      </section>
    </div>
  )
}

function ToolRow({ name, missing, pkgs }: { name: string; missing?: string[]; pkgs: string }) {
  const ok = (missing?.length ?? 0) === 0
  const isLoading = missing === undefined

  return (
    <div className="flex items-center justify-between rounded-xl border border-slate-800 bg-slate-950/60 p-4">
      <div>
        <div className="font-semibold text-slate-100 text-sm">{name}</div>
        <div className="text-xs text-slate-400 mt-0.5">{pkgs}</div>
      </div>
      <div>
        {isLoading ? (
          <Spinner />
        ) : ok ? (
          <Badge tone="green">
            <CheckCircle2 size={12} className="inline mr-1" /> Installed & Ready
          </Badge>
        ) : (
          <Badge tone="red">
            <AlertTriangle size={12} className="inline mr-1" /> Missing: {missing?.join(', ')}
          </Badge>
        )}
      </div>
    </div>
  )
}
