import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import FlowModal from './FlowModal'

// Swimlane flow list. `parent` is "/api/cards/{id}" (Business Flow) or "/api/items/{id}"
// (Technical Flow); null until the parent exists. `onChanged` runs after any change.
export default function FlowsSection({ parent, label = 'Business Flow', emptyHint, onChanged, onModal }) {
  const [flows, setFlows] = useState([])
  const [editing, setEditing] = useState(null) // null | 'new' | flow
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    if (!parent) return
    try {
      setFlows(await api(`${parent}/flows`))
    } catch (err) {
      setError(err.message)
    }
  }, [parent])
  useEffect(() => { load() }, [load])

  const open = (v) => { setEditing(v); onModal(!!v) }
  const done = () => { open(null); load(); onChanged() }

  return (
    <div className="field">
      <div className="field-row">
        <span>{label}</span>
        <button type="button" className="ai-btn" disabled={!parent} title={parent ? 'Add A Swimlane Flow' : 'Save first'} onClick={() => open('new')}>
          + Add Flow
        </button>
      </div>
      {error && <p className="error">{error}</p>}
      {flows.length === 0 ? (
        <p className="hint">{parent ? (emptyHint || 'No flow yet.') : 'Save first, then add a flow.'}</p>
      ) : (
        <ul className="flow-list">
          {flows.map((f) => (
            <li key={f.id}>
              <button type="button" onClick={() => open(f)}>{f.title}</button>
            </li>
          ))}
        </ul>
      )}
      {editing && (
        <FlowModal createPath={`${parent}/flows`} flow={editing === 'new' ? null : editing} onSaved={done} onDeleted={done} onClose={() => open(null)} />
      )}
    </div>
  )
}
