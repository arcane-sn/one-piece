import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'

// What Claude is doing right now (chat turns, Agent runs), from GET /api/activity. Polled every 2s while
// something runs, every 6s otherwise, so cards, the Discuss button and the dashboard can show it.
const Ctx = createContext({ rows: [], finished: [], forCard: () => [], dismiss: () => {}, clearFinished: () => {} })
const KEEP = 'op-activity-finished'
export const rowKey = (r) => `${r.kind}:${r.boardId}:${r.cardId}`
export const useActivity = () => useContext(Ctx)
export const KIND = { discuss: 'Discuss', review: 'Review', brainstorm: 'Brainstorming', agent: 'Agent', chain: 'Run All' }

export function ActivityProvider({ children }) {
  const [rows, setRows] = useState([])
  // finished work stays on the dashboard, flagged as done, until the owner dismisses it
  const [finished, setFinished] = useState(() => {
    try { return JSON.parse(localStorage.getItem(KEEP) || '[]') } catch { return [] }
  })
  const prev = useRef(null)
  useEffect(() => { localStorage.setItem(KEEP, JSON.stringify(finished)) }, [finished])
  useEffect(() => {
    let stop = false
    let timer
    const loop = async () => {
      let busy = false
      try {
        const r = await api('/api/activity')
        busy = r.length > 0
        const now = new Set(r.map(rowKey))
        const gone = (prev.current ?? []).filter((x) => !now.has(rowKey(x)))
        prev.current = r
        setFinished((f) => {
          const kept = f.filter((x) => !now.has(rowKey(x))) // running again: no longer "done"
          if (gone.length === 0 && kept.length === f.length) return f
          const at = new Date().toISOString()
          return [...gone.map((x) => ({ ...x, finishedAt: at })), ...kept.filter((x) => !gone.some((g) => rowKey(g) === rowKey(x)))].slice(0, 20)
        })
        setRows((old) => (JSON.stringify(old) === JSON.stringify(r) ? old : r))
      } catch { /* keep the last state */ }
      if (!stop) timer = setTimeout(loop, busy ? 2000 : 6000)
    }
    loop()
    return () => { stop = true; clearTimeout(timer) }
  }, [])
  const dismiss = useCallback((k) => setFinished((f) => f.filter((x) => rowKey(x) !== k)), [])
  const clearFinished = useCallback(() => setFinished([]), [])
  const value = useMemo(() => ({ rows, finished, forCard: (id) => rows.filter((r) => r.cardId === id), dismiss, clearFinished }), [rows, finished, dismiss, clearFinished])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}
