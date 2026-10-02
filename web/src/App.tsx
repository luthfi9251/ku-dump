import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthProvider, RequireAuth } from './auth'
import Layout from './Layout'
import Login from './pages/Login'
import Databases from './pages/Databases'
import Dumps from './pages/Dumps'
import Jobs from './pages/Jobs'
import Settings from './pages/Settings'
import Workflows from './pages/Workflows'

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
              <Route path="/dumps" element={<Dumps />} />
              <Route path="/jobs" element={<Jobs />} />
              <Route path="/workflows" element={<Workflows />} />
              <Route path="/settings" element={<Settings />} />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </QueryClientProvider>
  )
}

