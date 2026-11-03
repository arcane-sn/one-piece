import { useState } from 'react'
import { api } from '../api'
import Logo from '../components/Logo'

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
      setError(err.status === 401 ? "Username or password doesn't match. Check both and try again." : err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-page">
      <form className="login" onSubmit={submit}>
        <Logo size={56} wordmark={false} />
        <h1>Ready When You Are</h1>
        <p className="sub">Sign in to see what your agents have finished.</p>
        <input placeholder="Username" autoFocus autoComplete="username" value={username} aria-invalid={!!error}
          onChange={(e) => setUsername(e.target.value)} />
        <input placeholder="Password" type="password" autoComplete="current-password" value={password} aria-invalid={!!error}
          onChange={(e) => setPassword(e.target.value)} />
        {error && <p className="error">{error}</p>}
        <button className="primary" type="submit" disabled={busy || !username || !password}>
          {busy && <span className="spinner" />}
          {busy ? 'Signing In…' : 'Sign In'}
        </button>
      </form>
    </div>
  )
}
