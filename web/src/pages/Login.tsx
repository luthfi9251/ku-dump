import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { CheckCircle2, HardDrive, KeyRound, ShieldAlert, User } from 'lucide-react'
import { useAuth } from '../auth'
import { Button, Field, Input, Spinner } from '../ui'

export default function Login() {
  const { me, needsSetup, login, setup } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [successMsg, setSuccessMsg] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (me) {
      navigate('/', { replace: true })
    }
  }, [me, navigate])

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setSuccessMsg('')
    setBusy(true)
    try {
      if (needsSetup) {
        await setup(username, password)
        setSuccessMsg('Account created successfully! Redirecting to dashboard...')
      } else {
        await login(username, password)
        setSuccessMsg('Login successful! Redirecting...')
      }
      setTimeout(() => {
        navigate('/', { replace: true })
      }, 500)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Authentication failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-950 p-4 relative overflow-hidden selection:bg-indigo-500/30 selection:text-indigo-200">
      {/* Background Decorative Glow Blobs */}
      <div className="absolute -top-40 -left-40 h-96 w-96 rounded-full bg-indigo-600/15 blur-3xl" />
      <div className="absolute -bottom-40 -right-40 h-96 w-96 rounded-full bg-violet-600/15 blur-3xl" />

      <div className="relative w-full max-w-md">
        <form
          onSubmit={onSubmit}
          className="space-y-5 rounded-2xl border border-slate-800 bg-slate-900/90 p-8 shadow-2xl backdrop-blur-xl"
        >
          {/* Header Brand */}
          <div className="flex flex-col items-center text-center space-y-2 mb-2">
            <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-gradient-to-tr from-indigo-600 to-violet-500 shadow-lg shadow-indigo-600/30">
              <HardDrive size={24} className="text-white" />
            </div>
            <h1 className="text-2xl font-bold tracking-tight text-white">ku-dump</h1>
            <p className="text-xs text-slate-400">Database Backup & Restore Management Console</p>
          </div>

          {needsSetup && (
            <div className="rounded-xl border border-sky-800/60 bg-sky-950/40 p-3 text-xs text-sky-300 flex items-start gap-2">
              <ShieldAlert size={16} className="shrink-0 mt-0.5" />
              <div>
                <span className="font-semibold block">First Run Setup</span>
                Create your initial administrator account (username ≥ 3 chars, password ≥ 8 chars).
              </div>
            </div>
          )}

          <div className="space-y-4">
            <Field label="Username">
              <div className="relative">
                <User size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
                <Input
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="admin"
                  autoFocus
                  className="pl-9"
                  required
                />
              </div>
            </Field>

            <Field label="Password">
              <div className="relative">
                <KeyRound size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
                <Input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••"
                  className="pl-9"
                  required
                />
              </div>
            </Field>
          </div>

          {successMsg && (
            <div className="rounded-lg border border-emerald-800/60 bg-emerald-950/40 p-2.5 text-xs text-emerald-300 flex items-center gap-2">
              <CheckCircle2 size={16} className="shrink-0" />
              {successMsg}
            </div>
          )}

          {error && (
            <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-2.5 text-xs text-rose-300">
              {error}
            </div>
          )}

          <Button type="submit" disabled={busy} className="w-full justify-center py-2.5 shadow-lg shadow-indigo-600/25">
            {busy ? <Spinner /> : needsSetup ? 'Create Admin Account & Log In' : 'Log In'}
          </Button>
        </form>
      </div>
    </div>
  )
}


