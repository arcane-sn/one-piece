import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import FlowModal from './FlowModal'

// "Business Flow" list under the description. `onChanged` runs after any change
// (the server bumped the ticket's version and started a new review).
export default function FlowsSection({ card, onChanged, onModal }) {
  const [flows, setFlows] = useState([])
  const [editing, setEditing] = useState(null) // null | 'new' | flow
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    if (!card?.id) return
    try {
      setFlows(await api(`/api/cards/${card.id}/flows`))
    } catch (err) {
      setError(err.message)
    }
  }, [card?.id])
  useEffect(() => { load() }, [load])

  const open = (v) => { setEditing(v); onModal(!!v) }
  const done = () => { open(null); load(); onChanged() }

  return (
    <div className="field">
      <div className="field-row">
        <span>Business Flow</span>
        <button type="button" className="ai-btn" disabled={!card} title={card ? 'Add A Swimlane Flow' : 'Create the ticket first'} onClick={() => open('new')}>
          + Add Flow
        </button>
      </div>
      {error && <p className="error">{error}</p>}
      {flows.length === 0 ? (
        <p className="hint">{card ? 'No flow yet. Claude asks for at least one before approval.' : 'Create the ticket first, then add its flow.'}</p>
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
        <FlowModal cardId={card.id} flow={editing === 'new' ? null : editing} onSaved={done} onDeleted={done} onClose={() => open(null)} />
      )}
    </div>
  )
}
