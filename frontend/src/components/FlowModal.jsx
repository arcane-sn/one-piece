import { useEffect, useMemo, useState } from 'react'
import { createPortal } from 'react-dom'
import { api } from '../api'
import { build, hl, parse } from '../lib/swimlanes'

const SAMPLE = `title: Sign In
autonumber
User -> App: Opens the login page
User -> App: Submits username and password
App -> Server: Verify credentials
Server --> App: Session cookie
App --> User: Shows the dashboard
`

// Business Flow editor: swimlane text on the left, live diagram on the right.
export default function FlowModal({ cardId, flow, onSaved, onDeleted, onClose }) {
  const [title, setTitle] = useState(flow?.title ?? '')
  const [source, setSource] = useState(flow?.source ?? SAMPLE)
  const [busy, setBusy] = useState(false)
  const [zoom, setZoom] = useState(1)
  const [fontsTick, setFontsTick] = useState(0)
  const [scroll, setScroll] = useState({ top: 0, left: 0 })
  const [error, setError] = useState('')

  useEffect(() => { document.fonts?.ready.then(() => setFontsTick(1)) }, []) // text is measured with the swimlane fonts

  const diagram = useMemo(() => {
    try {
      const P = parse(source)
      return { out: build(P), bad: P.bad?.length ?? 0, notes: P.noteRows }
    } catch {
      return { out: null, bad: 0, broken: true }
    }
  }, [source, fontsTick])
  const highlighted = useMemo(
    () => source.split('\n').map((l, i) => hl(l, diagram.notes?.has(i))).join('\n') + '\n',
    [source, diagram.notes],
  )

  const dirty = title !== (flow?.title ?? '') || source !== (flow?.source ?? SAMPLE)
  const close = () => {
    if (dirty && !window.confirm('Discard unsaved changes?')) return
    onClose()
  }

  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && close()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  async function save(e) {
    e.preventDefault()
    e.stopPropagation() // React bubbles through the portal into the ticket form
    setBusy(true)
    setError('')
    const body = { title, source }
    try {
      const f = flow
        ? await api(`/api/flows/${flow.id}`, { method: 'PUT', body })
        : await api(`/api/cards/${cardId}/flows`, { method: 'POST', body })
      onSaved(f)
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  async function remove() {
    if (!window.confirm(`Delete "${flow.title}"?`)) return
    setBusy(true)
    try {
      await api(`/api/flows/${flow.id}`, { method: 'DELETE' })
      onDeleted(flow)
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop flow-backdrop" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <form className="modal flow-modal" role="dialog" aria-modal="true" aria-label={flow ? 'Edit Flow' : 'Add Flow'} onSubmit={save}>
        <div className="ticket-head">
          <h2>{flow ? 'Edit Flow' : 'Add Flow'}</h2>
          <button type="button" className="icon-btn" aria-label="Close" onClick={close}>×</button>
        </div>
        <label className="field">
          <span>Flow Name</span>
          <input autoFocus placeholder="e.g. Owner signs in" maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />
        </label>
        <div className="flow-split">
          <div className="field flow-src">
            <span>Swimlanes</span>
            <div className="sl-ed">
              <pre className="sl-hl" aria-hidden="true" style={{ transform: `translate(${-scroll.left}px, ${-scroll.top}px)` }} dangerouslySetInnerHTML={{ __html: highlighted }} />
              <textarea className="sl-src" spellCheck={false} wrap="off" aria-label="Swimlane source" value={source}
                onChange={(e) => setSource(e.target.value)}
                onScroll={(e) => setScroll({ top: e.target.scrollTop, left: e.target.scrollLeft })} />
            </div>
            <p className="hint">One step per line: <code>User -&gt; App: message</code>. Arrows: -&gt; --&gt; -&gt;&gt; &lt;- =&gt; -x. Also <code>note:</code>, <code>if:</code>/<code>else:</code>/<code>end</code>, <code>autonumber</code>.</p>
          </div>
          <div className="field flow-view">
            <span>Preview{diagram.bad > 0 && <em className="flow-warn"> · {diagram.bad} line{diagram.bad > 1 ? 's' : ''} not understood</em>}</span>
            <div className="sl-out">
              <div className="sl-zoom" role="group" aria-label="Zoom">
                <button type="button" aria-label="Zoom out" onClick={() => setZoom((z) => Math.max(0.3, +(z - 0.1).toFixed(2)))}>−</button>
                <output>{Math.round(zoom * 100)}%</output>
                <button type="button" aria-label="Zoom in" onClick={() => setZoom((z) => Math.min(2, +(z + 0.1).toFixed(2)))}>+</button>
              </div>
              <div className="sl-scroll">
                {diagram.out
                  ? <div className="sl-svg" style={{ zoom }} dangerouslySetInnerHTML={{ __html: diagram.out.svg }} />
                  : <p className="sl-empty">{diagram.broken ? 'This text cannot be drawn yet.' : <>No diagram yet. Type <code>Alice -&gt; Bob: hello</code> in the editor.</>}</p>}
              </div>
            </div>
          </div>
        </div>
        {error && <p className="error">{error}</p>}
        <div className="modal-actions ticket-actions">
          {flow && <button type="button" className="danger" onClick={remove} disabled={busy}>Delete</button>}
          <span className="grow" />
          <button type="button" onClick={close}>Cancel</button>
          <button className="primary" type="submit" disabled={busy || !title.trim() || (flow && !dirty)}>
            {flow ? 'Save Flow' : 'Add Flow'}
          </button>
        </div>
      </form>
    </div>,
    document.body,
  )
}
