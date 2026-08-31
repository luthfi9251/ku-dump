import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { X } from 'lucide-react'
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
  const job = useQuery({
    queryKey: ['job', jobId],
    queryFn: () => api.get<JobDTO>(`/api/jobs/${jobId}`),
    refetchInterval: (q) =>
      q.state.data ? (q.state.data.status === 'pending' || q.state.data.status === 'running' ? 2000 : false) : 2000,
  })
  const cancel = useMutation({
    mutationFn: () => api.post(`/api/jobs/${jobId}/cancel`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['job', jobId] }),
  })
  const j = job.data
  return (
    <div className="fixed inset-0 z-30 flex justify-end bg-black/30" onClick={onClose}>
      <div
        className="flex h-full w-full max-w-xl flex-col bg-white shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-slate-200 p-4">
          <div>
            <h2 className="font-semibold">{j ? `${j.type} — ${j.databaseName}` : 'Job'}</h2>
            {j && <div className="text-xs text-slate-500">{j.dumpLabel}</div>}
          </div>
          <div className="flex items-center gap-2">
            {j && <Badge tone={statusTone(j.status)}>{j.status}</Badge>}
            {j && (j.status === 'pending' || j.status === 'running') && (
              <Button variant="danger" onClick={() => cancel.mutate()} disabled={cancel.isPending}>
                Cancel
              </Button>
            )}
            <button onClick={onClose} className="rounded p-1 text-slate-400 hover:bg-slate-100">
              <X size={18} />
            </button>
          </div>
        </div>
        <div className="flex-1 overflow-auto bg-slate-950 p-4">
          {job.isLoading ? (
            <Spinner />
          ) : (
            <>
              {j?.error && (
                <pre className="mb-3 whitespace-pre-wrap rounded bg-red-900/50 p-2 text-xs text-red-200">{j.error}</pre>
              )}
              <pre className="whitespace-pre-wrap font-mono text-xs text-slate-200">{j?.logTail || 'waiting for output…'}</pre>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
