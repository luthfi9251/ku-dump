import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthProvider, RequireAuth } from './auth'
import Layout from './Layout'
import Login from './pages/Login'
import Databases from './pages/Databases'

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route
              element={
                <RequireAuth>
                  <Layout />
                </RequireAuth>
              }
            >
              <Route index element={<Databases />} />
              <Route path="/dumps" element={<Placeholder title="Dumps" />} />
              <Route path="/jobs" element={<Placeholder title="Jobs" />} />
              <Route path="/settings" element={<Placeholder title="Settings" />} />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </QueryClientProvider>
  )
}

function Placeholder({ title }: { title: string }) {
  return <div className="text-slate-500">{title} page — implemented in the next tasks.</div>
}
