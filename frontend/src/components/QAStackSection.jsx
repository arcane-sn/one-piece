import { useEffect, useState } from 'react'
import { api, baseName } from '../api'
import { toast } from '../lib/toast'

const blank = { name: '', folder: '', command: '', port: '', readyUrl: '', readyTimeoutS: 60 }
const SESSION = { none: 'No saved login', valid: 'Login saved', expired: 'Login expired' }

// Board setting: the local stack browser QA starts, the hosts it may reach, and the manual login.
// Nothing here is reachable by Claude. Buttons are type="button" because this lives inside the board form.
export default function QAStackSection({ board }) {
  const base = `/api/boards/${board.id}/qa`
  const [view, setView] = useState(null)
  const [cfg, setCfg] = useState(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function load() {
    try {
      const v = await api(base)
      setView(v)
      setCfg((c) => c ?? {
        ...v.config,
        allowedHosts: (v.config.allowedHosts ?? []).join('\n'),
        services: (v.config.services ?? []).map((s) => ({ ...s })),
      })
    } catch (err) { setError(err.message) }
  }
  useEffect(() => { load() }, [])
  // while the login window or a run holds the lock, keep the state fresh
  useEffect(() => {
    if (!view?.busy) return
    const t = setInterval(load, 2000)
    return () => clearInterval(t)
  }, [view?.busy])

  if (!cfg) return error ? <p className="error">{error}</p> : null
  const set = (patch) => setCfg((c) => ({ ...c, ...patch }))
  const setSvc = (i, patch) => set({ services: cfg.services.map((s, j) => (j === i ? { ...s, ...patch } : s)) })

  async function save() {
    setBusy(true); setError('')
    try {
      const body = {
        ...cfg,
        allowedHosts: cfg.allowedHosts.split('\n').map((h) => h.trim()).filter(Boolean),
        services: cfg.services.map((s) => ({ ...s, port: Number(s.port) || 0, readyTimeoutS: Number(s.readyTimeoutS) || 60 })),
      }
      const v = await api(base, { method: 'PUT', body })
      setView(v)
      setCfg({ ...v.config, allowedHosts: v.config.allowedHosts.join('\n'), services: v.config.services.map((s) => ({ ...s })) })
      toast('QA stack saved', 'ok')
    } catch (err) { setError(err.message) } finally { setBusy(false) }
  }
  async function act(path, method = 'POST', msg) {
    setError('')
    try {
      await api(base + path, { method, body: method === 'POST' ? {} : undefined })
      if (msg) toast(msg, 'ok')
      load()
    } catch (err) { setError(err.message) }
  }

  const configured = view.config.baseUrl && view.config.services?.length > 0
  return (
    <details className="field qa-stack" open={configured || undefined}>
      <summary><span>Browser QA stack <em>optional. Without it QA Test Cases only read code.</em></span></summary>
      {!view.toolchain && <p className="hint">Playwright is not installed. Run <code>server qa-setup</code> once in the backend folder.</p>}
      <label className="field"><span>Base URL</span>
        <input placeholder="http://localhost:3000" value={cfg.baseUrl} onChange={(e) => set({ baseUrl: e.target.value })} /></label>
      <div className="field-pair">
        <label className="field"><span>Login URL <em>defaults to Base URL</em></span>
          <input value={cfg.loginUrl} onChange={(e) => set({ loginUrl: e.target.value })} /></label>
        <label className="field"><span>Session check URL <em>defaults to Base URL</em></span>
          <input value={cfg.sessionCheckUrl} onChange={(e) => set({ sessionCheckUrl: e.target.value })} /></label>
      </div>
      <label className="field"><span>Allowed hosts <em>one per line, e.g. sso.example.test or *.example.test. localhost is always allowed. Everything else is blocked.</em></span>
        <textarea rows={3} value={cfg.allowedHosts} onChange={(e) => set({ allowedHosts: e.target.value })} /></label>
      <label className="check"><input type="checkbox" checked={cfg.headed} onChange={(e) => set({ headed: e.target.checked })} /> Show the browser while QA runs</label>

      <div className="field"><span>Services <em>started for each run and stopped after it. A port that is already busy blocks the run.</em></span>
        {cfg.services.map((s, i) => (
          <div className="qa-svc" key={i}>
            <input placeholder="name" value={s.name} onChange={(e) => setSvc(i, { name: e.target.value })} />
            <select value={s.folder} onChange={(e) => setSvc(i, { folder: e.target.value })}>
              <option value="">Folder…</option>
              {board.folders.map((f) => <option key={f} value={f}>{baseName(f)}</option>)}
            </select>
            <input className="wide" placeholder="command, e.g. npm run dev" value={s.command} onChange={(e) => setSvc(i, { command: e.target.value })} />
            <input placeholder="port" inputMode="numeric" value={s.port} onChange={(e) => setSvc(i, { port: e.target.value })} />
            <input className="wide" placeholder="ready URL" value={s.readyUrl} onChange={(e) => setSvc(i, { readyUrl: e.target.value })} />
            <button type="button" className="icon-btn" aria-label="Remove service" onClick={() => set({ services: cfg.services.filter((_, j) => j !== i) })}>×</button>
          </div>
        ))}
        <button type="button" className="add-folder" onClick={() => set({ services: [...cfg.services, { ...blank }] })}>+ Add Service</button>
      </div>

      {error && <p className="error">{error}</p>}
      <div className="qa-actions">
        <button type="button" className="primary" disabled={busy || !!view.busy} onClick={save}>Save QA Stack</button>
        <span className={`item-status ${view.session.state === 'valid' ? '' : 'stale'}`}>{SESSION[view.session.state]}</span>
        <button type="button" disabled={!configured || !view.toolchain || !!view.busy} onClick={() => act('/login', 'POST', 'A browser window opened. Log in, then close it.')}>
          {view.busy === 'login' ? 'Waiting for you to close the browser…' : 'Log in…'}</button>
        <button type="button" disabled={view.session.state === 'none' || !!view.busy} onClick={() => act('/session/check', 'POST', 'Checked')}>Check</button>
        <button type="button" disabled={view.session.state === 'none' || !!view.busy} onClick={() => act('/session', 'DELETE', 'Login forgotten')}>Forget</button>
      </div>
      <p className="hint">You log in yourself in a real browser window (bot checks and all). Claude never sees the saved login, and it is only used on the hosts above.</p>
    </details>
  )
}
