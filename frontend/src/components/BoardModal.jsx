import { useEffect, useRef, useState } from 'react'
import { api, baseName } from '../api'
import { FolderIcon } from './Logo'

// Create (no `initial`) or edit a board: name, short description and folders
// picked through the native Finder dialog (the Go server opens it).
export default function BoardModal({ initial, onSaved, onClose }) {
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [key, setKey] = useState(initial?.key ?? '')
  const [folders, setFolders] = useState(initial?.folders ?? [])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [picking, setPicking] = useState(false)
  const nameRef = useRef(null)

  useEffect(() => {
    nameRef.current?.focus()
    const onKey = (e) => e.key === 'Escape' && !picking && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, picking])

  async function addFolder() {
    setPicking(true)
    setError('')
    try {
      const { path } = await api('/api/pick-folder', { method: 'POST', body: {} })
      if (path) setFolders((fs) => (fs.includes(path) ? fs : [...fs, path]))
    } catch (err) {
      setError(err.message)
    } finally {
      setPicking(false)
    }
  }

  async function submit(e) {
    e.preventDefault()
    setBusy(true)
    setError('')
    const body = { name, description, folders, ...(initial && key ? { key } : {}) }
    try {
      const saved = initial
        ? await api(`/api/boards/${initial.id}`, { method: 'PUT', body })
        : await api('/api/boards', { method: 'POST', body })
      onSaved(saved)
    } catch (err) {
      setError(err.message)
      setBusy(false)
    }
  }

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !picking && onClose()}>
      <form className="modal" role="dialog" aria-modal="true" aria-label={initial ? 'Edit Board' : 'New Board'} onSubmit={submit}>
        <div className="modal-head">
          <h2>{initial ? 'Edit Board' : 'New Board'}</h2>
          <p>One board per product. Add the folders its agents may read and edit.</p>
        </div>

        <label className="field">
          <span>Product Name</span>
          <input ref={nameRef} placeholder="e.g. Harbor App" maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
        </label>

        <label className="field">
          <span>Description <em>optional</em></span>
          <textarea rows={3} maxLength={500} placeholder="What is this product, in a sentence or two?"
            value={description} onChange={(e) => setDescription(e.target.value)} />
        </label>

        {initial && (
          <label className="field">
            <span>Board Key <em>2-5 letters, prefix of ticket IDs. Existing IDs keep theirs.</em></span>
            <input className="key-input" maxLength={5} value={key}
              onChange={(e) => setKey(e.target.value.toUpperCase().replace(/[^A-Z]/g, ''))} />
          </label>
        )}

        <div className="field">
          <span>Folders <em>optional</em></span>
          {folders.length > 0 && (
            <ul className="folder-list">
              {folders.map((f) => (
                <li key={f} title={f}>
                  <FolderIcon />
                  <span className="folder-name">{baseName(f)}</span>
                  <span className="folder-path">{f}</span>
                  <button type="button" className="icon-btn" aria-label={`Remove ${baseName(f)}`}
                    onClick={() => setFolders((fs) => fs.filter((x) => x !== f))}>×</button>
                </li>
              ))}
            </ul>
          )}
          <button type="button" className="add-folder" onClick={addFolder} disabled={picking}>
            {picking ? 'Waiting for Finder…' : folders.length ? '+ Add Another Folder' : '+ Choose a Folder…'}
          </button>
        </div>

        {error && <p className="error">{error}</p>}
        <div className="modal-actions">
          <button type="button" onClick={onClose} disabled={picking}>Cancel</button>
          <button className="primary" type="submit" disabled={busy || picking || !name.trim()}>
            {initial ? 'Save Changes' : 'Create Board'}
          </button>
        </div>
      </form>
    </div>
  )
}
