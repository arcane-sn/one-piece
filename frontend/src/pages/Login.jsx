import { useState } from 'react'
import { api } from '../api'

export default function Login({ onLoggedIn }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api('/api/login', { method: 'POST', body: { username, password } })
      const me = await api('/api/me')
      onLoggedIn(me.username)
    } catch (err) {
      setError(err.status === 401 ? 'Username atau password salah' : err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-page">
      <form className="login glass" onSubmit={submit}>
        <div className="login-icon" aria-hidden="true">
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4" />
            <path d="M10 17l5-5-5-5M15 12H3" />
          </svg>
        </div>
        <h1>Masuk ke One Piece</h1>
        <p className="muted">Satu tempat untuk semua pekerjaan Agent.</p>
        <input placeholder="Username" autoFocus autoComplete="username" value={username}
          onChange={(e) => setUsername(e.target.value)} />
        <input placeholder="Password" type="password" autoComplete="current-password" value={password}
          onChange={(e) => setPassword(e.target.value)} />
        {error && <p className="error">{error}</p>}
        <button className="primary" type="submit" disabled={busy || !username || !password}>Masuk</button>
      </form>
    </div>
  )
}
