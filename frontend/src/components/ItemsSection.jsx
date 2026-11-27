import Thinking from './Thinking'
import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api'
import ItemModal from './ItemModal'
import ItemRow, { EvidenceSummary } from './ItemRow'
import { toast } from '../lib/toast'
import { useJob } from '../lib/jobs'

const KINDS = [
  ['agent', 'Agent Tasks', 'Add Task'],
  ['qa', 'QA Test Cases', 'Add Test Case'],
]
// Agent Tasks and QA Test Cases: the plan and Definition of Done the owner approves. Claude drafts them when the
// ticket reaches Business; they are editable there and locked once the ticket is in Agent or Done.
// onChanged() tells the ticket the plan changed (an edit revokes the approval).
export default function ItemsSection({ card, onModal, onChanged }) {
  const [items, setItems] = useState([])
  const [editing, setEditing] = useState(null) // null | {kind} (new) | item
  const [error, setError] = useState('')
  const [split, setSplit] = useState('')
  // Generate runs as a background job on the server: closing the ticket does not stop it.
  const agentJob = useJob('card', card?.id, 'agent_tasks')
  const qaJob = useJob('card', card?.id, 'qa_cases')
  const jobs = { agent: agentJob, qa: qaJob }
  const busy = agentJob.job?.status === 'running' ? 'agent' : qaJob.job?.status === 'running' ? 'qa' : ''

  const load = useCallback(async () => {
    if (!card?.id) return
    try {
      setItems(await api(`/api/cards/${card.id}/items`))
    } catch (err) {
      setError(err.message)
    }
  }, [card?.id])
  useEffect(() => { load() }, [load])

  const anyDoing = items.some((i) => i.status === 'doing')
  // Run All answers before the server has marked the first task Doing, so show it as started right away
  const [starting, setStarting] = useState('') // kind | ''
  useEffect(() => {
    if (!starting) return
    if (anyDoing) { setStarting(''); return }
    const t = setInterval(load, 700)
    const stop = setTimeout(() => setStarting(''), 8000)
    return () => { clearInterval(t); clearTimeout(stop) }
  }, [starting, anyDoing, load])
  // follow runs started here or from the task modal
  useEffect(() => {
    if (!anyDoing) return
    const t = setInterval(load, 2000)
    return () => clearInterval(t)
  }, [anyDoing, load])

  async function runAll(action, kind) {
    setError('')
    try {
      if (action === 'run-all') setStarting(kind)
      await api(`/api/cards/${card.id}/${action}`, { method: 'POST', body: { kind } })
      if (action === 'stop-all') setStarting('')
      await load()
    } catch (err) {
      setStarting('')
      toast(err.message)
    }
  }

  const open = (v) => { setEditing(v); onModal(!!v) }
  const planOpen = card?.status === 'business' // the plan can only change in Business
  const running = card?.status === 'agent' && card?.approved // tasks and QA only run in Agent

  async function generate(kind) {
    setError('')
    setSplit('')
    try {
      await jobs[kind].start(`/api/cards/${card.id}/generate-items`, { kind })
    } catch (err) {
      toast(err.message)
    }
  }

  // When a job that was running finishes, show its items (or why the ticket must be split).
  const seenRunning = useRef({})
  for (const kind of ['agent', 'qa']) {
    const j = jobs[kind].job
    if (j?.status === 'running') seenRunning.current[kind] = j.id
  }
  useEffect(() => {
    for (const kind of ['agent', 'qa']) {
      const j = jobs[kind].job
      if (j && j.status !== 'running' && seenRunning.current[kind] === j.id) {
        seenRunning.current[kind] = 0
        load()
        onChanged?.()
        try {
          const r = JSON.parse(j.result || '{}')
          if (r.split) setSplit(r.reason || 'This ticket is too big. Split it into smaller tickets.')
        } catch { /* no result */ }
      }
    }
  }, [agentJob.job, qaJob.job]) // eslint-disable-line react-hooks/exhaustive-deps

  const sections = [['agent', 'Agent Tasks', 'Add Task'], ['qa', 'QA Test Cases', 'Add Test Case']]
  return (
    <>
      {error && <p className="error">{error}</p>}
      {split && <div className="review-comment work"><div className="review-comment-head">Too Big For One Ticket</div><p>{split}</p></div>}
      {sections.map(([kind, label, add]) => {
        const all = items.filter((i) => i.kind === kind)
        const list = all.filter((i) => !i.superseded)
        const struck = all.filter((i) => i.superseded)
        const going = list.some((i) => i.status === 'doing') || starting === kind
        return (
          <div className="field items-block" key={kind}>
            <div className="items-head">
              <span className="items-title">{label}</span>
              <span className="items-actions">
                {planOpen && (
                  <button type="button" className="ai-btn" disabled={busy === kind || anyDoing} onClick={() => generate(kind)}>
                    {busy === kind ? <><span className="spinner" aria-hidden="true" /> <Thinking inline /></> : <><span aria-hidden="true">✦</span> {list.some((i) => !i.fixFor) ? 'Regenerate' : 'Generate'}</>}
                  </button>
                )}
                {running && (going
                  ? <button type="button" className="ai-btn" onClick={() => runAll('stop-all', kind)}><span className="spinner" aria-hidden="true" /> In progress · ■ Stop</button>
                  : <button type="button" className="ai-btn" disabled={!!busy || anyDoing || !list.some((i) => i.status === 'todo')}
                      title={kind === 'qa' ? 'Runs after all Agent Tasks are Done' : undefined} onClick={() => runAll('run-all', kind)}>▶ Run All</button>)}
                {planOpen && <button type="button" className="ai-btn" onClick={() => open({ kind })}>+ {add}</button>}
              </span>
            </div>
            {busy === kind && <p className="muted prompt-status"><span className="spinner" /> Claude is {kind === 'qa' ? 'writing the test cases' : 'breaking the ticket down'}… You can close this and come back.</p>}
            {list.length === 0 && busy !== kind && (
              <p className="hint">{kind === 'qa' ? 'No test cases yet. They are the Definition of Done: Generate them, or add your own.' : 'No tasks yet.'}</p>
            )}
            {kind === 'qa' && list.length > 0 && <EvidenceSummary list={list} />}
            <ul className="flow-list">
              {list.map((i) => <ItemRow key={i.id} item={i} card={card} onOpen={open} />)}
            </ul>
            {struck.length > 0 && (
              <>
                <p className="hint superseded-label">Superseded · replaced by a newer Generate</p>
                <ul className="flow-list superseded">
                  {struck.map((i) => <ItemRow key={i.id} item={i} card={card} onOpen={open} struck />)}
                </ul>
              </>
            )}
          </div>
        )
      })}
      {editing && (
        <ItemModal
          cardId={card.id}
          item={editing.id ? editing : null}
          kind={editing.kind}
          locked={!planOpen}
          onSaved={() => { open(null); load(); onChanged?.() }}
          onDeleted={() => { open(null); load(); onChanged?.() }}
          onClose={() => { open(null); load(); onChanged?.() }}
        />
      )}
    </>
  )
}

