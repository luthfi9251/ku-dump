import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { api } from './lib/api'

interface Me {
  id: string
  username: string
}

interface AuthState {
  loading: boolean
  needsSetup: boolean
  me: Me | null
  login: (username: string, password: string) => Promise<void>
  setup: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const Ctx = createContext<AuthState | null>(null)

export function useAuth(): AuthState {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAuth outside provider')
  return v
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [me, setMe] = useState<Me | null>(null)

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const user = await api.get<Me>('/api/me')
        if (!cancelled) setMe(user)
      } catch {
        try {
          const st = await api.get<{ needsSetup: boolean }>('/api/auth/status')
          if (!cancelled) setNeedsSetup(st.needsSetup)
        } catch {
          /* server unreachable */
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const login = useCallback(async (username: string, password: string) => {
    const user = await api.post<Me>('/api/auth/login', { username, password })
    setMe(user)
  }, [])

  const setup = useCallback(async (username: string, password: string) => {
    await api.post('/api/auth/setup', { username, password })
    const user = await api.post<Me>('/api/auth/login', { username, password })
    setMe(user)
  }, [])

  const logout = useCallback(async () => {
    await api.post('/api/auth/logout')
    setMe(null)
  }, [])

  return (
    <Ctx.Provider value={{ loading, needsSetup, me, login, setup, logout }}>{children}</Ctx.Provider>
  )
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { loading, me } = useAuth()
  if (loading) return <div className="p-8 text-slate-500">Loading…</div>
  if (!me) return <Navigate to="/login" replace />
  return <>{children}</>
}
