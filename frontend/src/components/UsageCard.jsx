import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import Tip from './Tip'

// "claude-sonnet-5-5" -> "Sonnet 5.5"
export function modelName(id) {
  const m = /^claude-([a-z]+)-(\d+)-(\d+)/.exec(id || '')
  return m ? `${m[1][0].toUpperCase()}${m[1].slice(1)} ${m[2]}.${m[3]}` : id || '—'
}
const fmtN = (n) => (n >= 1e6 ? (n / 1e6).toFixed(1) + 'M' : n >= 1e3 ? (n / 1e3).toFixed(1) + 'k' : String(n))
const sum = (rows, k) => rows.reduce((a, r) => a + r[k], 0)

function until(ts, now) {
  const s = ts - now
  if (s <= 0) return 'reset'
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60)
  return 'in ' + (d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${Math.max(m, 1)}m`)
}
function ago(ts, now) {
  const m = Math.floor((now - ts) / 60)
  return m < 1 ? 'just now' : m < 60 ? `${m}m ago` : `${Math.floor(m / 60)}h ago`
}
// A window whose reset time has passed has no known utilization until Claude runs again.
const pct = (l, now) => (l && l.resetsAt > now ? Math.round(l.utilization * 100) : null)
const tone = (p) => (p === null ? '' : p >= 90 ? 'hot' : p >= 70 ? 'warm' : '')

function Bar({ label, l, now }) {
  const p = pct(l, now)
  return (
    <div className="usage-row">
      <span className="usage-lbl">{label}</span>
      <span className={'usage-bar ' + tone(p)}><span style={{ width: (p ?? 0) + '%' }} /></span>
      <span className="usage-val">{p === null ? '—' : p + '%'}</span>
      <span className="usage-reset">{l ? until(l.resetsAt, now) : ''}</span>
    </div>
  )
}

function Ring({ p }) {
  const r = 11, c = 2 * Math.PI * r
  return (
    <svg width="30" height="30" viewBox="0 0 30 30" aria-hidden="true" className={'usage-ringsvg ' + tone(p)}>
      <circle cx="15" cy="15" r={r} fill="none" strokeWidth="3.5" className="bg" />
      <circle cx="15" cy="15" r={r} fill="none" strokeWidth="3.5" strokeLinecap="round" className="fg"
        strokeDasharray={c} strokeDashoffset={c * (1 - (p ?? 0) / 100)} transform="rotate(-90 15 15)" />
    </svg>
  )
}

function Table({ title, rows }) {
  return (
    <div className="usage-sec">
      <b>{title}</b>
      {rows.length === 0 ? <span className="muted">No runs yet</span> : (
        <>
          <div className="usage-line"><span>Input / output</span><span>{fmtN(sum(rows, 'input'))} / {fmtN(sum(rows, 'output'))}</span></div>
          <div className="usage-line"><span>Cache read / write</span><span>{fmtN(sum(rows, 'cacheRead'))} / {fmtN(sum(rows, 'cacheWrite'))}</span></div>
          <div className="usage-line"><span>Turns · cost</span><span>{sum(rows, 'turns')} · ${sum(rows, 'costUsd').toFixed(2)}</span></div>
          {rows.length > 1 && rows.map((r) => (
            <div className="usage-line sub" key={r.model}><span>{modelName(r.model)}</span><span>${r.costUsd.toFixed(2)}</span></div>
          ))}
        </>
      )}
    </div>
  )
}

// Always-visible Claude usage: model, 5-hour and weekly limits with their reset times. Click for details.
export default function UsageCard({ rail }) {
  const [v, setV] = useState(null)
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [tick, setTick] = useState(() => Math.floor(Date.now() / 1000))
  const btn = useRef(null)
  const [pos, setPos] = useState(null)

  const probed = useRef(false)
  // First look with no limits on record (fresh install): make one tiny call so the card has real numbers.
  const load = useCallback(() => api('/api/usage').then((d) => {
    setV(d)
    if (!d.limits?.five_hour && !d.limits?.seven_day && !probed.current) {
      probed.current = true
      api('/api/usage/refresh', { method: 'POST', body: {} }).then(setV).catch(() => {})
    }
  }).catch(() => {}), [])
  useEffect(() => {
    load()
    const t = setInterval(() => { load(); setTick(Math.floor(Date.now() / 1000)) }, 30000)
    const onFocus = () => { load(); setTick(Math.floor(Date.now() / 1000)) }
    window.addEventListener('focus', onFocus)
    return () => { clearInterval(t); window.removeEventListener('focus', onFocus) }
  }, [load])

  useEffect(() => {
    if (!open) return
    const onKey = (e) => e.key === 'Escape' && setOpen(false)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open])

  async function refresh() {
    setBusy(true)
    try { setV(await api('/api/usage/refresh', { method: 'POST', body: {} })) } catch { /* keep the old numbers */ }
    setTick(Math.floor(Date.now() / 1000))
    setBusy(false)
  }
  function toggle() {
    if (!open && btn.current) {
      const r = btn.current.getBoundingClientRect()
      setPos({ left: r.right + 12, bottom: window.innerHeight - r.bottom })
    }
    setOpen((o) => !o)
  }

  const now = Math.max(v?.now ?? 0, tick)
  const f = v?.limits?.five_hour, w = v?.limits?.seven_day
  const pf = pct(f, now), pw = pct(w, now)
  const worst = Math.max(pf ?? 0, pw ?? 0)
  const empty = v && !f && !w
  const summary = `5h ${pf === null ? '—' : pf + '%'} · Week ${pw === null ? '—' : pw + '%'} · ${modelName(v?.model)}`

  return (
    <>
      <Tip label={summary} off={!rail || open}>
        <button type="button" ref={btn} className={'usage' + (open ? ' open' : '')} aria-label="Claude usage" onClick={toggle}>
          <span className="usage-ring"><Ring p={pf === null && pw === null ? null : worst} /></span>
          <span className="usage-full">
            <span className="usage-model">{modelName(v?.model)}</span>
            {empty || !v ? <span className="usage-empty">No data yet</span> : (
              <>
                <Bar label="5h" l={f} now={now} />
                <Bar label="Week" l={w} now={now} />
              </>
            )}
          </span>
        </button>
      </Tip>
      {open && v && pos && createPortal(
        <>
          <div className="usage-scrim" onMouseDown={() => setOpen(false)} />
          <div className="usage-pop" role="dialog" aria-label="Claude usage" style={{ left: pos.left, bottom: pos.bottom }}>
            <div className="usage-pophead">
              <b>Claude usage</b>
              <span className="usage-chip">{modelName(v.model)}</span>
            </div>
            <Bar label="5h" l={f} now={now} />
            <Bar label="Week" l={w} now={now} />
            <p className="usage-note">
              {f || w ? `Limits updated ${ago(Math.max(f?.updatedAt ?? 0, w?.updatedAt ?? 0), now)}. ` : ''}
              They only change when Claude runs; Refresh makes one tiny call.
            </p>
            <Table title="This 5-hour window" rows={v.session} />
            <Table title="Today" rows={v.today} />
            <Table title="Last 7 days" rows={v.week} />
            <button type="button" className="usage-refresh" disabled={busy} onClick={refresh}>
              {busy && <span className="spinner" aria-hidden="true" />} Refresh limits
            </button>
          </div>
        </>,
        document.body,
      )}
    </>
  )
}
