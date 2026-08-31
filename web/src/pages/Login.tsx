import { useState } from 'react'
import type { FormEvent } from 'react'
import { useAuth } from '../auth'
import { Button, Field, Input } from '../ui'

export default function Login() {
  const { needsSetup, login, setup } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      if (needsSetup) await setup(username, password)
      else await login(username, password)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50">
      <form onSubmit={onSubmit} className="w-80 space-y-4 rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
        <h1 className="text-xl font-bold">ku-dump</h1>
        {needsSetup && (
          <p className="rounded bg-blue-50 p-2 text-sm text-blue-800">
            First run: create the admin user (username ≥ 3, password ≥ 8 chars).
          </p>
        )}
        <Field label="Username">
          <Input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
        </Field>
        <Field label="Password">
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        {error && <p className="text-sm text-red-600">{error}</p>}
        <Button type="submit" disabled={busy} className="w-full justify-center">
          {busy ? '…' : needsSetup ? 'Create admin & login' : 'Login'}
        </Button>
      </form>
    </div>
  )
}
