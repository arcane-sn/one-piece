import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import { toast } from './toast'

// Background Generate jobs live on the server. This provider polls them so a toast can announce a
// finished one anywhere in the app, and so kanban cards can show a spinner while one is running.
const JobsContext = createContext({ busyCards: new Set(), running: () => false })
export const useJobs = () => useContext(JobsContext)

const REFRESH = 'op-jobs-refresh'
export const refreshJobs = () => window.dispatchEvent(new Event(REFRESH))

function finishedMessage(j) {
  if (j.kind === 'review_chat') {
    return j.status === 'error' ? { text: `${j.label} gagal: ${j.error}`, kind: 'error' } : { text: `${j.label} selesai`, kind: 'ok' }
  }
  if (j.status === 'error') return { text: `Generate ${j.label} gagal: ${j.error}`, kind: 'error' }
  try {
    const r = JSON.parse(j.result || '{}')
    if (r.split) return { text: `Generate ${j.label}: tiket terlalu besar. ${r.reason || 'Pecah jadi tiket yang lebih kecil.'}`, kind: 'error' }
  } catch { /* plain result */ }
  return { text: `Generate ${j.label} selesai`, kind: 'ok' }
}

export function JobsProvider({ children }) {
  const [active, setActive] = useState([])
  const shown = useRef(new Set())

  const poll = useCallback(async () => {
    try {
      const js = await api('/api/jobs/active')
      for (const j of js) {
        if (j.status === 'running' || shown.current.has(j.id)) continue
        shown.current.add(j.id)
        const m = finishedMessage(j)
        toast(m.text, m.kind)
        api(`/api/jobs/${j.id}/seen`, { method: 'POST', body: {} }).catch(() => {})
      }
      setActive(js.filter((j) => j.status === 'running'))
      return js.some((j) => j.status === 'running')
    } catch {
      return false
    }
  }, [])

  useEffect(() => {
    let stop = false
    let timer
    const loop = async () => {
      const busy = await poll()
      if (!stop) timer = setTimeout(loop, busy ? 2500 : 10000)
    }
    const kick = () => { clearTimeout(timer); loop() }
    loop()
    window.addEventListener(REFRESH, kick)
    return () => { stop = true; clearTimeout(timer); window.removeEventListener(REFRESH, kick) }
  }, [poll])

  const value = useMemo(() => ({
    busyCards: new Set(active.map((j) => j.cardId).filter(Boolean)),
    // is a job of this kind running for this target? (scope: card | item | board)
    running: (kind, scope, target) => active.some((j) => j.kind === kind && j.scope === scope && j.targetId === Number(target)),
  }), [active])
  return <JobsContext.Provider value={value}>{children}</JobsContext.Provider>
}

// The newest job of one kind for one target, kept fresh while it runs. A modal that is closed and
// reopened gets the same state back from the server: running, finished with a result, or failed.
export function useJob(scope, target, kind) {
  const [job, setJob] = useState(null)
  const load = useCallback(async () => {
    if (!target) { setJob(null); return null }
    try {
      const js = await api(`/api/jobs?scope=${scope}&target=${target}`)
      const j = js.find((x) => x.kind === kind) ?? null
      setJob(j)
      return j
    } catch {
      return null
    }
  }, [scope, target, kind])
  useEffect(() => { load() }, [load])
  const running = job?.status === 'running'
  useEffect(() => {
    if (!running) return
    const t = setInterval(load, 1500)
    return () => clearInterval(t)
  }, [running, load])

  const start = useCallback(async (path, body) => {
    const j = await api(path, { method: 'POST', body })
    setJob(j)
    refreshJobs()
    return j
  }, [])
  const consume = useCallback(async () => {
    if (!job) return
    setJob((j) => j && { ...j, consumed: true })
    api(`/api/jobs/${job.id}/consume`, { method: 'POST', body: {} }).catch(() => {})
  }, [job])
  return { job, start, consume, reload: load }
}

// The result of a finished, not yet used job, or null.
export function jobResult(job) {
  if (!job || job.status !== 'done' || job.consumed) return null
  try { return JSON.parse(job.result) } catch { return null }
}
export const jobError = (job) => (job?.status === 'error' && !job.consumed ? job.error : '')
