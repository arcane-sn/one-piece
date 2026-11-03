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
    <form className="login" onSubmit={submit}>
      <h1>One Piece</h1>
      <input placeholder="Username" autoFocus autoComplete="username" value={username}
        onChange={(e) => setUsername(e.target.value)} />
      <input placeholder="Password" type="password" autoComplete="current-password" value={password}
        onChange={(e) => setPassword(e.target.value)} />
      {error && <p className="error">{error}</p>}
      <button type="submit" disabled={busy || !username || !password}>Masuk</button>
    </form>
  )
}
