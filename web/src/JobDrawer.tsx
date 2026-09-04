import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Terminal, X, StopCircle } from 'lucide-react'
import { api } from './lib/api'
import type { JobDTO } from './lib/types'
import { Badge, Button, Spinner } from './ui'

export function statusTone(status: JobDTO['status']): 'green' | 'red' | 'amber' | 'blue' | 'slate' {
  switch (status) {
    case 'success':
      return 'green'
    case 'failed':
      return 'red'
    case 'running':
      return 'blue'
    case 'pending':
      return 'amber'
    default:
      return 'slate'
  }
}

export default function JobDrawer({ jobId, onClose }: { jobId: string; onClose: () => void }) {
  const qc = useQueryClient()
  const [copied, setCopied] = useState(false)
  const logContainerRef = useRef<HTMLDivElement>(null)

  const job = useQuery({
    queryKey: ['job', jobId],
    queryFn: () => api.get<JobDTO>(`/api/jobs/${jobId}`),
    refetchInterval: (q) =>
      q.state.data ? (q.state.data.status === 'pending' || q.state.data.status === 'running' ? 1500 : false) : 1500,
  })

  const cancel = useMutation({
    mutationFn: () => api.post(`/api/jobs/${jobId}/cancel`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['job', jobId] }),
  })

  const j = job.data

  // Auto-scroll logs to bottom on new output
  useEffect(() => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight
    }
  }, [j?.logTail])

  function handleCopyLogs() {
    if (!j?.logTail) return
    void navigator.clipboard.writeText(j.logTail)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-slate-950/80 backdrop-blur-sm animate-fade-in" onClick={onClose}>
      <div
        className="animate-slide-in-right flex h-full w-full max-w-2xl flex-col border-l border-slate-800 bg-slate-900 shadow-2xl shadow-black/90"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Drawer Header */}
        <div className="flex items-center justify-between border-b border-slate-800 p-5 bg-slate-950/60">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-indigo-500/10 text-indigo-400 border border-indigo-500/20">
              <Terminal size={20} />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="font-bold text-slate-100 text-base">{j ? `${j.type.toUpperCase()} — ${j.databaseName}` : 'Job Logs'}</h2>
                {j && (
                  <Badge tone={statusTone(j.status)} pulse={j.status === 'running' || j.status === 'pending'}>
                    {j.status}
                  </Badge>
                )}
              </div>
              {j && <div className="text-xs font-mono text-slate-400 mt-0.5">{j.dumpLabel || `Job ID: ${j.id}`}</div>}
            </div>
          </div>

          <div className="flex items-center gap-2">
            {j && (j.status === 'pending' || j.status === 'running') && (
              <Button
                variant="danger"
                size="sm"
                onClick={() => cancel.mutate()}
                disabled={cancel.isPending}
                title="Abort Job Execution"
              >
                <StopCircle size={14} /> Cancel
              </Button>
            )}
            <button
              onClick={onClose}
              className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-slate-200 transition-colors"
            >
              <X size={20} />
            </button>
          </div>
        </div>

        {/* Terminal Header Action Bar */}
        <div className="flex items-center justify-between border-b border-slate-800/80 bg-slate-950 px-4 py-2 text-xs text-slate-400">
          <div className="flex items-center gap-2">
            <span className="h-2.5 w-2.5 rounded-full bg-rose-500/80 inline-block" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-500/80 inline-block" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-500/80 inline-block" />
            <span className="ml-2 font-mono text-slate-500">stdout/stderr tail</span>
          </div>
          <button
            onClick={handleCopyLogs}
            disabled={!j?.logTail}
            className="flex items-center gap-1.5 rounded bg-slate-800 px-2.5 py-1 text-slate-300 transition-colors hover:bg-slate-700 disabled:opacity-40"
          >
            {copied ? <Check size={13} className="text-emerald-400" /> : <Copy size={13} />}
            <span>{copied ? 'Copied!' : 'Copy Logs'}</span>
          </button>
        </div>

        {/* Console Log Area */}
        <div ref={logContainerRef} className="flex-1 overflow-auto bg-slate-950 p-5 font-mono text-xs leading-relaxed text-slate-200 selection:bg-indigo-500/40">
          {job.isLoading ? (
            <div className="flex items-center justify-center py-20 text-slate-400 gap-2">
              <Spinner /> Loading job output...
            </div>
          ) : (
            <>
              {j?.error && (
                <div className="mb-4 rounded-lg border border-rose-800/60 bg-rose-950/50 p-3 text-rose-300">
                  <div className="font-bold uppercase tracking-wider text-[11px] text-rose-400 mb-1">Error Traceback</div>
                  <pre className="whitespace-pre-wrap">{j.error}</pre>
                </div>
              )}
              <pre className="whitespace-pre-wrap text-slate-300 tracking-tight font-mono">
                {j?.logTail ? (
                  j.logTail.split('\n').map((line, idx) => (
                    <div key={idx} className="hover:bg-slate-900/60 px-1 py-0.5 rounded flex">
                      <span className="w-8 shrink-0 text-slate-600 select-none text-right pr-3">{idx + 1}</span>
                      <span className="flex-1">{line}</span>
                    </div>
                  ))
                ) : (
                  <span className="italic text-slate-600">Waiting for process output…</span>
                )}
              </pre>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

