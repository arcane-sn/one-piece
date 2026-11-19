import { useEffect, useRef, useState } from 'react'

// One confirm dialog for the whole app, above every other modal. Esc/backdrop = cancel.
export default function ConfirmHost() {
  const [req, setReq] = useState(null)
  const okRef = useRef(null)
  const cancelRef = useRef(null)

  useEffect(() => {
    const onReq = (e) => setReq((old) => { old?.resolve(false); return e.detail })
    window.addEventListener('op-confirm', onReq)
    return () => window.removeEventListener('op-confirm', onReq)
  }, [])

  const answer = (v) => { req.resolve(v); setReq(null) }

  useEffect(() => {
    if (!req) return
    ;(req.danger ? cancelRef : okRef).current?.focus()
    // capture phase + stopPropagation: Esc must close only this dialog, not the modal under it
    const onKey = (e) => {
      if (e.key !== 'Escape') return
      e.stopPropagation()
      e.preventDefault()
      req.resolve(false)
      setReq(null)
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [req])

  if (!req) return null
  return (
    <div className="modal-backdrop confirm-backdrop" onMouseDown={(e) => e.target === e.currentTarget && answer(false)}>
      <div className="modal confirm-modal" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title" aria-describedby="confirm-msg">
        <div>
          <h2 id="confirm-title">{req.title}</h2>
          {req.message && <p id="confirm-msg" className="confirm-msg">{req.message}</p>}
        </div>
        <div className="modal-actions">
          <button type="button" ref={cancelRef} onClick={() => answer(false)}>{req.cancelLabel ?? 'Cancel'}</button>
          <button type="button" ref={okRef} className={req.danger ? 'confirm-ok danger' : 'primary'} onClick={() => answer(true)}>{req.confirmLabel ?? 'OK'}</button>
        </div>
      </div>
    </div>
  )
}
