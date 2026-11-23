import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'

// Our own confirm dialog. Its Esc/Enter listener is registered once at app start (capture), so it runs before
// the listeners of modals opened later and can swallow the key while the dialog is up.
export default function Confirmer() {
  const [ask, setAsk] = useState(null)
  useEffect(() => {
    let cur = null
    const settle = (ok) => { const a = cur; cur = null; setAsk(null); a?.resolve(ok) }
    const onAsk = (e) => { cur?.resolve(false); cur = e.detail; setAsk(e.detail) }
    const onKey = (e) => {
      if (!cur) return
      if (e.key === 'Escape') { e.preventDefault(); e.stopImmediatePropagation(); settle(false) }
      else if (e.key === 'Enter') { e.preventDefault(); e.stopImmediatePropagation(); settle(true) }
    }
    window.addEventListener('op-confirm', onAsk)
    window.addEventListener('keydown', onKey, true)
    window.__opConfirmSettle = settle
    return () => { window.removeEventListener('op-confirm', onAsk); window.removeEventListener('keydown', onKey, true) }
  }, [])
  if (!ask) return null
  const done = (ok) => window.__opConfirmSettle(ok)
  return createPortal(
    <div className="modal-backdrop confirm-backdrop" onMouseDown={(e) => { e.stopPropagation(); if (e.target === e.currentTarget) done(false) }}>
      <div className="modal confirm" role="alertdialog" aria-modal="true" aria-label={ask.message}>
        <p className="confirm-msg">{ask.message}</p>
        <div className="confirm-actions">
          <button type="button" onClick={() => done(false)}>{ask.cancelLabel ?? 'Cancel'} <kbd>Esc</kbd></button>
          <button type="button" className={ask.danger ? 'danger' : 'primary'} autoFocus onClick={() => done(true)}>{ask.okLabel ?? 'OK'} <kbd>↵</kbd></button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
