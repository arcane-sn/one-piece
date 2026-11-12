import { useEffect, useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import { build, parse } from '../lib/swimlanes'

const HINT = {
  business: 'e.g. A customer pays, support sees it, and a wrong payment is refunded.',
  technical: 'e.g. The form calls the API, the API checks the password and sets the session cookie.',
}

// How many lines of the text the swimlane drawing could not read (null = it cannot be drawn at all).
function problems(source) {
  try {
    const P = parse(source)
    build(P)
    return P.bad ?? []
  } catch {
    return null
  }
}

// Prompt -> Claude drafts a swimlane flow -> preview with the real drawing -> save.
// If the drawing cannot read some lines, Claude gets one automatic chance to fix them.
export default function GenerateFlowModal({ kind, id, createPath, onSaved, onClose }) {
  const [prompt, setPrompt] = useState('')
  const [title, setTitle] = useState('')
  const [source, setSource] = useState('')
  const [busy, setBusy] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [fontsTick, setFontsTick] = useState(0)

  useEffect(() => { document.fonts?.ready.then(() => setFontsTick(1)) }, [])
  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && !busy && !saving && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  const bad = useMemo(() => (source ? problems(source) : []), [source])
  const svg = useMemo(() => {
    if (!source || bad === null) return ''
    try { return build(parse(source)).svg } catch { return '' }
  }, [source, bad, fontsTick])

  async function generate(e) {
    e?.preventDefault()
    e?.stopPropagation()
    setBusy(true)
    setError('')
    try {
      let r = await api('/api/flows/generate', { method: 'POST', body: { kind, id, prompt } })
      let p = problems(r.source)
      if (p === null || p.length > 0) { // one automatic retry, telling Claude which lines failed
        try {
          const r2 = await api('/api/flows/generate', { method: 'POST', body: { kind, id, prompt, previous: r.source, problems: p ?? [] } })
          const p2 = problems(r2.source)
          if (p2 !== null && (p === null || p2.length <= p.length)) r = r2
        } catch { /* keep the first draft */ }
      }
      setTitle(r.title || title)
      setSource(r.source)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  async function save() {
    setSaving(true)
    setError('')
    try {
      onSaved(await api(createPath, { method: 'POST', body: { title: title.trim(), source } }))
    } catch (err) {
      setError(err.message)
      setSaving(false)
    }
  }

  const ready = !!source && !busy
  return createPortal(
    <div className="modal-backdrop flow-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && !saving && onClose()}>
      <form className="modal gen-flow-modal" role="dialog" aria-modal="true" aria-label="Generate Flow" onSubmit={generate}>
        <div className="ticket-head">
          <h2>Generate {kind === 'technical' ? 'Technical' : 'Business'} Flow</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={onClose} disabled={busy || saving}>×</button>
        </div>
        <label className="field">
          <span>What Should The Flow Show?</span>
          <textarea autoFocus rows={3} maxLength={2000} placeholder={HINT[kind]} value={prompt} onChange={(e) => setPrompt(e.target.value)} />
        </label>
        {busy && <p className="muted prompt-status"><span className="spinner" /> Claude is drawing the flow…</p>}
        {error && <p className="error">{error}</p>}
        {ready && (
          <>
            <label className="field">
              <span>Flow Name</span>
              <input maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />
            </label>
            <div className="field">
              <span>Preview{bad?.length > 0 && <em className="flow-warn"> · {bad.length} line{bad.length > 1 ? 's' : ''} not understood</em>}</span>
              <div className="sl-out gen-preview">
                <div className="sl-scroll">
                  {svg
                    ? <div className="sl-svg" style={{ zoom: 0.85 }} dangerouslySetInnerHTML={{ __html: svg }} />
                    : <p className="sl-empty">This text cannot be drawn. Regenerate, or save and fix it in the editor.</p>}
                </div>
              </div>
              <p className="hint">You can edit the text after saving: open the flow from the list.</p>
            </div>
          </>
        )}
        <div className="modal-actions">
          <button type="button" onClick={onClose} disabled={busy || saving}>Cancel</button>
          <button type="submit" className={ready ? '' : 'primary'} disabled={busy || saving || !prompt.trim()}>{ready ? 'Regenerate' : 'Generate'}</button>
          {ready && (
            <button type="button" className="primary" disabled={saving || !title.trim() || !svg} onClick={save}>Save Flow</button>
          )}
        </div>
      </form>
    </div>,
    document.body,
  )
}
