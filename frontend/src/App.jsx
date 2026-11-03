import { useCallback, useEffect, useState } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { api } from './api'
import Sidebar from './components/Sidebar'
import Board from './pages/Board'
import Dashboard from './pages/Dashboard'
import Login from './pages/Login'

function Shell({ user, onLogout }) {
  const [boards, setBoards] = useState(null) // null = loading
  const reload = useCallback(() => api('/api/boards').then(setBoards).catch(() => setBoards((b) => b ?? [])), [])
  useEffect(() => { reload() }, [reload])

  return (
    <>
      <Sidebar user={user} boards={boards ?? []} onLogout={onLogout} />
      <main className="main">
        <Routes>
          <Route path="/" element={<Dashboard user={user} boards={boards} reload={reload} />} />
          <Route path="/boards/:id" element={<Board reloadBoards={reload} />} />
        </Routes>
      </main>
    </>
  )
}

export default function App() {
  const [user, setUser] = useState(undefined) // undefined = loading, null = logged out

  useEffect(() => {
    api('/api/me').then((r) => setUser(r.username)).catch(() => setUser(null))
  }, [])

  if (user === undefined) return <p className="muted" style={{ padding: 24 }}>Loading…</p>
  if (!user) return <Login onLoggedIn={setUser} />

  async function logout() {
    await api('/api/logout', { method: 'POST' })
    setUser(null)
  }

  return (
    <BrowserRouter>
      <Shell user={user} onLogout={logout} />
    </BrowserRouter>
  )
}
