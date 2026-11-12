import { useEffect, useState } from 'react'

const SHOW_MS = 7000

// Stack of toasts at the bottom of the screen, above every modal.
export default function Toaster() {
  const [items, setItems] = useState([])
  useEffect(() => {
    let n = 0
    const onToast = (e) => {
      const id = ++n
      setItems((xs) => [...xs.slice(-3), { id, ...e.detail }])
      setTimeout(() => setItems((xs) => xs.filter((x) => x.id !== id)), SHOW_MS)
    }
    window.addEventListener('op-toast', onToast)
    return () => window.removeEventListener('op-toast', onToast)
  }, [])
  if (!items.length) return null
  return (
    <div className="toaster" aria-live="assertive">
      {items.map((t) => (
        <button key={t.id} type="button" role="alert" className={'toast ' + t.kind} title="Dismiss"
          onClick={() => setItems((xs) => xs.filter((x) => x.id !== t.id))}>
          <span className="toast-msg">{t.message}</span>
          <span aria-hidden="true" className="toast-x">×</span>
        </button>
      ))}
    </div>
  )
}
