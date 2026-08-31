import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { api } from '../lib/api'
import type { JobDTO } from '../lib/types'
import { Badge, Spinner } from '../ui'
import JobDrawer, { statusTone } from '../JobDrawer'

export default function Jobs() {
  const [params, setParams] = useSearchParams()
  const [drawerId, setDrawerId] = useState<string | null>(params.get('job'))

  const jobs = useQuery({
    queryKey: ['jobs'],
    queryFn: () => api.get<JobDTO[]>('/api/jobs?limit=200'),
    refetchInterval: 2000,
  })

  function openDrawer(id: string) {
    setDrawerId(id)
    if (params.get('job') !== id) {
      params.set('job', id)
      setParams(params, { replace: true })
    }
  }

  function closeDrawer() {
    setDrawerId(null)
    params.delete('job')
    setParams(params, { replace: true })
  }

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-bold">Jobs</h1>
      {jobs.isLoading && <Spinner />}
      <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 text-left text-slate-500">
            <tr>
              <th className="p-3">Type</th>
              <th className="p-3">Database</th>
              <th className="p-3">Dump</th>
              <th className="p-3">Status</th>
              <th className="p-3">Created</th>
              <th className="p-3">Finished</th>
            </tr>
          </thead>
          <tbody>
            {jobs.data?.map((j) => (
              <tr
                key={j.id}
                onClick={() => openDrawer(j.id)}
                className={`cursor-pointer border-b border-slate-100 last:border-0 hover:bg-slate-50 ${drawerId === j.id ? 'bg-slate-100' : ''}`}
              >
                <td className="p-3 font-medium">{j.type}</td>
                <td className="p-3">{j.databaseName}</td>
                <td className="p-3">{j.dumpLabel}</td>
                <td className="p-3">
                  <Badge tone={statusTone(j.status)}>{j.status}</Badge>
                </td>
                <td className="p-3 text-slate-500">{new Date(j.createdAt).toLocaleString()}</td>
                <td className="p-3 text-slate-500">{j.finishedAt ? new Date(j.finishedAt).toLocaleString() : '—'}</td>
              </tr>
            ))}
            {jobs.data?.length === 0 && (
              <tr>
                <td colSpan={6} className="p-6 text-center text-slate-500">
                  No jobs yet. Start a dump or restore.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {drawerId && <JobDrawer jobId={drawerId} onClose={closeDrawer} />}
    </div>
  )
}
