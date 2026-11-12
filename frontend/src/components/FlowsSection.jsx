import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import FlowModal from './FlowModal'
import GenerateFlowModal from './GenerateFlowModal'

// Swimlane flow list. `parent` is "/api/cards/{id}" (Business Flow) or "/api/items/{id}"
// (Technical Flow); null until the parent exists. `onChanged` runs after any change.
export default function FlowsSection({ parent, label = 'Business Flow', emptyHint, onChanged, onModal }) {
  const [flows, setFlows] = useState([])
  const [editing, setEditing] = useState(null) // null | 'new' | 'generate' | flow
  const target = parent?.match(/^\/api\/(cards|items)\/(\d+)$/) // which Business/Technical Flow this list belongs to
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
        <span className="items-actions">
          <button type="button" className="ai-btn" disabled={!parent} title={parent ? 'Describe it and let Claude draw the flow' : 'Save first'} onClick={() => open('generate')}>
            <span aria-hidden="true">✦</span> Generate
          </button>
          <button type="button" className="ai-btn" disabled={!parent} title={parent ? 'Add A Swimlane Flow' : 'Save first'} onClick={() => open('new')}>
            + Add Flow
          </button>
        </span>
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
      {editing === 'generate' && target && (
        <GenerateFlowModal kind={target[1] === 'items' ? 'technical' : 'business'} id={Number(target[2])} createPath={`${parent}/flows`}
          onSaved={done} onClose={() => open(null)} />
      )}
      {editing && editing !== 'generate' && (
        <FlowModal createPath={`${parent}/flows`} flow={editing === 'new' ? null : editing} onSaved={done} onDeleted={done} onClose={() => open(null)} />
      )}
    </div>
  )
}
