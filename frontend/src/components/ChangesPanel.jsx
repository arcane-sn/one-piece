import { useEffect, useState } from 'react'
import { api, baseName } from '../api'
import { toast } from '../lib/toast'

// What an Agent Task committed: per repo the commit, its files with +/- and, on click, the file's diff.
export default function ChangesPanel({ itemId, refreshKey }) {
  const [changes, setChanges] = useState(null)
  const [open, setOpen] = useState(null) // `${changeId}:${path}`
  const [diff, setDiff] = useState({}) // key -> { text, truncated } | 'loading'
  useEffect(() => {
    api(`/api/items/${itemId}/changes`).then((r) => setChanges(r ?? [])).catch(() => setChanges([]))
  }, [itemId, refreshKey])
  if (!changes || changes.length === 0) return null

  async function toggle(c, path) {
    const key = `${c.id}:${path}`
    setOpen((o) => (o === key ? null : key))
    if (diff[key]) return
    setDiff((d) => ({ ...d, [key]: 'loading' }))
    try {
      const r = await api(`/api/item-changes/${c.id}/diff?file=${encodeURIComponent(path)}`)
      setDiff((d) => ({ ...d, [key]: { text: r.diff, truncated: r.truncated } }))
    } catch (e) {
      setDiff((d) => { const n = { ...d }; delete n[key]; return n })
      toast(e.message)
    }
  }

  return (
    <div className="field changes">
      <span>Changes</span>
      {changes.map((c) => (
        <div className="change-repo" key={c.id}>
          <div className="change-head">
            <strong title={c.repo}>{baseName(c.repo)}</strong>
            <button type="button" className="link-btn" title="Copy the commit SHA" onClick={() => navigator.clipboard?.writeText(c.sha).then(() => toast('SHA copied', 'ok'))}>
              <code>{c.sha.slice(0, 8)}</code>
            </button>
            <span className="change-stat"><b className="add">+{c.additions}</b> <b className="del">−{c.deletions}</b> · {c.files.length} file{c.files.length === 1 ? '' : 's'}</span>
          </div>
          <ul className="change-files">
            {c.files.map((f) => {
              const key = `${c.id}:${f.path}`
              const d = diff[key]
              return (
                <li key={f.path}>
                  <button type="button" className="change-file" aria-expanded={open === key} onClick={() => toggle(c, f.path)}>
                    <span className="change-path" title={f.path}>{f.path}</span>
                    <span className="change-stat">{f.additions < 0 ? 'binary' : <><b className="add">+{f.additions}</b> <b className="del">−{f.deletions}</b></>}</span>
                  </button>
                  {open === key && (
                    <pre className="diff">
                      {d === 'loading' || !d ? 'Loading…' : d.text.split('\n').map((l, i) => (
                        <span key={i} className={l.startsWith('+') && !l.startsWith('+++') ? 'dl-add' : l.startsWith('-') && !l.startsWith('---') ? 'dl-del' : l.startsWith('@@') ? 'dl-hunk' : ''}>{l}{'\n'}</span>
                      ))}
                      {d?.truncated && <span className="dl-hunk">… diff cut at 200 KB; open the commit in git for the rest</span>}
                    </pre>
                  )}
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </div>
  )
}
