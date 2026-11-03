import { useEffect, useState } from 'react'
import { BrowserRouter, Link, Route, Routes } from 'react-router-dom'
import { api } from './api'
import Logo from './components/Logo'
import Board from './pages/Board'
import Dashboard from './pages/Dashboard'
import Login from './pages/Login'

export default function App() {
  const [user, setUser] = useState(undefined) // undefined = loading, null = logged out

  useEffect(() => {
    api('/api/me').then((r) => setUser(r.username)).catch(() => setUser(null))
  }, [])

  if (user === undefined) return <p className="muted">Loading…</p>
  if (!user) return <Login onLoggedIn={setUser} />

  async function logout() {
    await api('/api/logout', { method: 'POST' })
    setUser(null)
  }

  return (
    <BrowserRouter>
      <header className="topbar">
        <Link to="/" className="brand"><Logo size={28} /></Link>
        <span>
          {user} <button onClick={logout}>Log out</button>
        </span>
      </header>
      <main className="container">
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/boards/:id" element={<Board />} />
        </Routes>
      </main>
    </BrowserRouter>
  )
}
