import { useEffect, useState } from 'react'
import { BrowserRouter, Link, Route, Routes } from 'react-router-dom'
import { api } from './api'
import Dashboard from './pages/Dashboard'
import Login from './pages/Login'

export default function App() {
  const [user, setUser] = useState(undefined) // undefined = loading, null = logged out

  useEffect(() => {
    api('/api/me').then((r) => setUser(r.username)).catch(() => setUser(null))
  }, [])

  if (user === undefined) return <p className="muted">Memuat…</p>
  if (!user) return <Login onLoggedIn={setUser} />

  async function logout() {
    await api('/api/logout', { method: 'POST' })
    setUser(null)
  }

  return (
    <BrowserRouter>
      <header className="topbar">
        <Link to="/">One Piece</Link>
        <span>
          {user} <button onClick={logout}>Logout</button>
        </span>
      </header>
      <main className="container">
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/boards/:id" element={<p>Board (Task 8)</p>} />
        </Routes>
      </main>
    </BrowserRouter>
  )
}
