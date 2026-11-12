import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { api } from '../api'

// The "@word" being typed right before the caret, or null.
function activeMention(text, caret) {
  const m = /(^|\s)@([^\s@]*)$/.exec(text.slice(0, caret))
  return m ? { start: caret - m[2].length - 1, query: m[2] } : null
}

// A textarea where typing "@" lists files and folders of the board's folders.
// Picking one inserts "@<folder>/<path>"; the server lets Claude read what was mentioned.
export default function MentionTextarea({ boardId, value, onChange, onKeyDown: outerKeyDown, autoGrow, ...rest }) {
  const ta = useRef(null)
  useLayoutEffect(() => { // autoGrow: the box follows its text, up to the CSS max-height
    const el = ta.current
    if (!autoGrow || !el) return
    el.style.height = 'auto'
    el.style.height = el.scrollHeight + 2 + 'px'
  }, [autoGrow, value])
  const [menu, setMenu] = useState(null) // { start, query, items, active }
  const seq = useRef(0)

  useEffect(() => {
    if (!menu || !boardId) return
    const mine = ++seq.current
    const t = setTimeout(async () => {
      try {
        const items = await api(`/api/boards/${boardId}/files?q=${encodeURIComponent(menu.query)}`)
        if (seq.current === mine) setMenu((m) => (m && m.query === menu.query ? { ...m, items, active: 0 } : m))
      } catch { /* no list: the textarea still works */ }
    }, 120)
    return () => clearTimeout(t)
  }, [menu?.query, menu?.start, boardId]) // eslint-disable-line react-hooks/exhaustive-deps

  function sync(el) {
    const m = boardId ? activeMention(el.value, el.selectionStart) : null
    setMenu((old) => (m ? { ...m, items: old && old.start === m.start ? old.items : [], active: old && old.start === m.start ? old.active : 0 } : null))
  }

  function pick(item) {
    const el = ta.current
    const text = `@${item.path}${item.isDir ? '/' : ' '}`
    const next = value.slice(0, menu.start) + text + value.slice(el.selectionStart)
    onChange(next)
    setMenu(null)
    requestAnimationFrame(() => {
      el.focus()
      const pos = menu.start + text.length
      el.setSelectionRange(pos, pos)
      if (item.isDir) sync(el) // keep drilling into the folder
    })
  }

  // The menu takes the keys it needs first; anything it leaves alone goes on to the caller's handler.
  function onKeyDown(e) {
    menuKey(e)
    if (!e.defaultPrevented) outerKeyDown?.(e)
  }

  function menuKey(e) {
    if (!menu?.items.length) {
      if (menu && e.key === 'Escape') { e.nativeEvent.stopPropagation(); setMenu(null) }
      return
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const n = menu.items.length
      setMenu({ ...menu, active: (menu.active + (e.key === 'ArrowDown' ? 1 : n - 1)) % n })
    } else if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault()
      pick(menu.items[menu.active])
    } else if (e.key === 'Escape') {
      e.nativeEvent.stopPropagation() // do not close the modal behind the menu
      setMenu(null)
    }
  }

  return (
    <div className="mention-wrap">
      <textarea ref={ta} value={value} {...rest}
        onChange={(e) => { onChange(e.target.value); sync(e.target) }}
        onKeyDown={onKeyDown}
        onKeyUp={(e) => { if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') sync(e.target) }}
        onClick={(e) => sync(e.target)}
        onBlur={() => setTimeout(() => setMenu(null), 120)} />
      {menu?.items.length > 0 && (
        <ul className="mention-list" role="listbox" aria-label="Files and folders">
          {menu.items.map((it, i) => (
            <li key={it.path} role="option" aria-selected={i === menu.active} className={i === menu.active ? 'on' : ''}
              onMouseDown={(e) => { e.preventDefault(); pick(it) }} onMouseEnter={() => setMenu((m) => ({ ...m, active: i }))}>
              {it.path}{it.isDir ? '/' : ''}{it.isRepo && <span className="repo-tag">repo</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
