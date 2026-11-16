import { useCallback, useEffect, useState } from 'react'
import { api } from '../api'
import { toast } from '../lib/toast'

// The ticket's decision log: dated notes Claude saves from Discuss (and notes the owner adds).
// They travel with later Run and QA sessions for this ticket.
export default function DecisionLog({ cardId, reloadKey = 0 }) {
  const [items, setItems] = useState([])
  const [text, setText] = useState('')
  const url = `/api/cards/${cardId}/decisions`

  const load = useCallback(async () => {
    try { setItems(await api(url)) } catch (err) { toast(err.message) }
  }, [url])
  useEffect(() => { load() }, [load, reloadKey])

  async function add() {
    const t = text.trim()
    if (!t) return
    try {
      await api(url, { method: 'POST', body: { text: t } })
      setText('')
      load()
    } catch (err) { toast(err.message) }
  }

  async function remove(id) {
    try { await api(`${url}/${id}`, { method: 'DELETE' }); load() } catch (err) { toast(err.message) }
  }

  return (
    <div className="field">
      <div className="field-row"><span>Decision Log</span></div>
      {items.length === 0 ? (
        <p className="hint">Nothing yet. What you and Claude agree on in Discuss is saved here and sent to later Run and QA sessions.</p>
      ) : (
        <ul className="decision-list">
          {items.map((d) => (
            <li key={d.id}>
              <span className="decision-date" title={d.createdAt}>{d.createdAt.slice(0, 10)}</span>
              <span className="decision-text">{d.text} <em>({d.source === 'claude' ? 'Claude' : 'you'})</em></span>
              <button type="button" className="icon-btn" aria-label="Delete note" onClick={() => remove(d.id)}>×</button>
            </li>
          ))}
        </ul>
      )}
      <div className="decision-add">
        <input value={text} maxLength={2000} placeholder="Add a note…"
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); add() } }} />
        <button type="button" onClick={add} disabled={!text.trim()}>Add</button>
      </div>
    </div>
  )
}
