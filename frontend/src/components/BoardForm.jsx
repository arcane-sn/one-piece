import { useState } from 'react'
import { api } from '../api'

// Create (no `initial`) or edit a board: name + one absolute folder path per line.
export default function BoardForm({ initial, onSaved, onCancel }) {
  const [name, setName] = useState(initial?.name ?? '')
  const [folders, setFolders] = useState((initial?.folders ?? []).join('\n'))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e) {
    e.preventDefault()
    setBusy(true)
    setError('')
    const body = { name, folders: folders.split('\n').map((f) => f.trim()).filter(Boolean) }
    try {
      const saved = initial
        ? await api(`/api/boards/${initial.id}`, { method: 'PUT', body })
        : await api('/api/boards', { method: 'POST', body })
      onSaved(saved)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="form panel" onSubmit={submit}>
      <h3>{initial ? 'Edit board' : 'New board'}</h3>
      <input placeholder="Product name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
      <textarea rows={4} placeholder={'Folders (one absolute path per line)\n/Users/me/app/frontend\n/Users/me/app/backend'}
        value={folders} onChange={(e) => setFolders(e.target.value)} />
      {error && <p className="error">{error}</p>}
      <div className="row">
        <button className="primary" type="submit" disabled={busy || !name.trim()}>Save</button>
        <button type="button" onClick={onCancel}>Cancel</button>
      </div>
    </form>
  )
}
