import { useEffect, useRef, useState } from 'react'
import { EditorContent, useEditor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import Image from '@tiptap/extension-image'
import { Markdown } from '@tiptap/markdown'
import { TaskItem, TaskList } from '@tiptap/extension-list'
import { Placeholder } from '@tiptap/extensions'
import { api } from '../api'
import { uploadImage } from '../lib/upload'

// Fetches the image an <img src> points to (data:, blob: or a web url the browser may read).
async function fetchImage(src) {
  let res
  try { res = await fetch(src, { credentials: 'include' }) } catch { res = await fetch(src) }
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  const blob = await res.blob()
  if (!blob.type.startsWith('image/')) throw new Error('not an image')
  return blob
}

const imageFiles = (dt) => [...(dt?.files ?? [])].filter((f) => f.type.startsWith('image/'))

const STYLES = [
  ['p', 'Paragraph', 'rte-opt-p'],
  ['1', 'Heading 1', 'rte-opt-h1'],
  ['2', 'Heading 2', 'rte-opt-h2'],
  ['3', 'Heading 3', 'rte-opt-h3'],
]

// Custom dropdown (the native <select> clashes with the rest of the UI).
function StyleMenu({ value, onPick }) {
  const [open, setOpen] = useState(false)
  const ref = useRef(null)
  useEffect(() => {
    if (!open) return
    const away = (e) => !ref.current?.contains(e.target) && setOpen(false)
    const esc = (e) => { if (e.key === 'Escape') { e.stopPropagation(); setOpen(false) } }
    document.addEventListener('mousedown', away)
    window.addEventListener('keydown', esc, true)
    return () => { document.removeEventListener('mousedown', away); window.removeEventListener('keydown', esc, true) }
  }, [open])
  const label = STYLES.find((s) => s[0] === value)?.[1] ?? 'Paragraph'
  return (
    <div className="rte-menu" ref={ref}>
      <button type="button" className="rte-style" aria-haspopup="menu" aria-expanded={open} aria-label="Text Style"
        onMouseDown={(e) => { e.preventDefault(); setOpen((o) => !o) }}>
        {label}
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 3.5 5 6.5 8 3.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
      </button>
      {open && (
        <ul className="rte-pop" role="menu">
          {STYLES.map(([v, name, cls]) => (
            <li key={v} role="none">
              <button type="button" role="menuitemradio" aria-checked={v === value} className={cls + (v === value ? ' on' : '')}
                onMouseDown={(e) => { e.preventDefault(); setOpen(false); onPick(v) }}>
                {name}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// Inserts pasted/dropped content whose pictures are copied to the server first. A picture that
// cannot be read (Jira links need its login) is dropped and reported through setNotice.
async function pasteWithImages(ed, html, files, setNotice) {
  setNotice('Copying images…')
  let failed = 0
  let out = ''
  if (html) {
    const doc = new DOMParser().parseFromString(html, 'text/html')
    for (const img of [...doc.querySelectorAll('img')]) {
      try {
        img.setAttribute('src', await uploadImage(await fetchImage(img.getAttribute('src') ?? '')))
        img.removeAttribute('srcset')
      } catch {
        failed++
        img.remove()
      }
    }
    out = doc.body.innerHTML
  }
  if (out && failed === 0) {
    ed.chain().focus().insertContent(out, { contentType: 'html' }).run()
  } else {
    if (out) ed.chain().focus().insertContent(out, { contentType: 'html' }).run()
    // The picture itself usually travels with the clipboard as a file when its link is unreadable.
    for (const f of files) {
      try {
        ed.chain().focus().setImage({ src: await uploadImage(f) }).run()
        failed = Math.max(0, failed - 1)
      } catch (err) {
        setNotice(err.message)
        return
      }
    }
  }
  setNotice(failed > 0
    ? `${failed} image${failed > 1 ? 's' : ''} could not be copied (the source blocks other sites). Paste the screenshot itself, or drag the image file in.`
    : '')
}

// WYSIWYG editor (Tiptap). The value is Markdown, so it stays readable for
// `claude -p` and for plain-text previews.
export default function RichEditor({ value, onChange, placeholder, readOnly = false }) {
  const ref = useRef(null)
  const [notice, setNotice] = useState('')
  const editor = useEditor({
    extensions: [
      StarterKit.configure({ link: { openOnClick: false } }),
      Image.configure({ allowBase64: false }),
      Markdown,
      TaskList,
      TaskItem.configure({ nested: true }),
      Placeholder.configure({ placeholder }),
    ],
    content: value,
    contentType: 'markdown',
    editable: !readOnly,
    editorProps: {
      // Rich paste with pictures (e.g. a Jira description): copy each picture to the server first,
      // since the source's own image links stop working outside Jira.
      handlePaste: (_view, event) => {
        const cd = event.clipboardData
        const ed = ref.current
        const html = cd?.getData('text/html') ?? ''
        const files = imageFiles(cd)
        if (ed && (/<img[\s>]/i.test(html) || files.length)) {
          event.preventDefault()
          pasteWithImages(ed, html, files, setNotice)
          return true
        }
        // Jira's editor copies an attachment as a bare media node with no <img> and no way to read it.
        if (ed && !files.length && /data-node-type="media|data-testid="media-|mediasingle/i.test(html)) {
          setNotice('This paste has an image that Jira does not hand over (the text was pasted). Right-click the image in Jira, Copy Image, and paste it here, or drag the image file in.')
        }
        // Plain-text paste is treated as Markdown ("- [ ] x" becomes a task list, "## T" a heading).
        const text = cd?.getData('text/plain')
        if (!text || cd.getData('text/html') || !ed || ed.isActive('codeBlock')) return false
        event.preventDefault()
        ed.commands.insertContent(text, { contentType: 'markdown' })
        return true
      },
      handleDrop: (_view, event, _slice, moved) => {
        const files = imageFiles(event.dataTransfer)
        const ed = ref.current
        if (moved || !files.length || !ed) return false
        event.preventDefault()
        pasteWithImages(ed, '', files, setNotice)
        return true
      },
    },
    shouldRerenderOnTransaction: true, // keeps toolbar "active" states in sync
    onUpdate: ({ editor: ed }) => onChange(ed.getMarkdown().trimEnd()),
  })
  ref.current = editor
  // Pick up value changes made outside the editor (e.g. AI-generated text).
  useEffect(() => {
    if (editor && value !== editor.getMarkdown().trimEnd()) editor.commands.setContent(value, { contentType: 'markdown', emitUpdate: false })
  }, [value, editor])
  if (!editor) return null

  const run = (fn) => (e) => {
    e.preventDefault() // keep focus/selection in the editor
    fn(editor.chain().focus()).run()
  }
  const Btn = ({ label, active, disabled, onRun, children }) => (
    <button type="button" className={'rte-btn' + (active ? ' on' : '')} title={label} aria-label={label}
      aria-pressed={!!active} disabled={disabled} onMouseDown={run(onRun)}>
      {children}
    </button>
  )

  function setLink() {
    const prev = editor.getAttributes('link').href ?? ''
    const url = window.prompt('Link URL (leave empty to remove)', prev)
    if (url === null) return
    if (url.trim() === '') editor.chain().focus().extendMarkRange('link').unsetLink().run()
    else editor.chain().focus().extendMarkRange('link').setLink({ href: url.trim() }).run()
  }

  const heading = editor.isActive('heading') ? String(editor.getAttributes('heading').level) : 'p'

  return (
    <div className={'rte' + (readOnly ? ' ro' : '')}>
      <div className="rte-toolbar" role="toolbar" aria-label="Formatting">
        <StyleMenu
          value={heading}
          onPick={(v) => {
            const c = editor.chain().focus()
            if (v === 'p') c.setParagraph().run()
            else c.setHeading({ level: Number(v) }).run()
          }}
        />
        <span className="rte-sep" />
        <Btn label="Bold" active={editor.isActive('bold')} onRun={(c) => c.toggleBold()}><b>B</b></Btn>
        <Btn label="Italic" active={editor.isActive('italic')} onRun={(c) => c.toggleItalic()}><i>I</i></Btn>
        <Btn label="Strikethrough" active={editor.isActive('strike')} onRun={(c) => c.toggleStrike()}><s>S</s></Btn>
        <Btn label="Inline Code" active={editor.isActive('code')} onRun={(c) => c.toggleCode()}>{'</>'}</Btn>
        <span className="rte-sep" />
        <Btn label="Bullet List" active={editor.isActive('bulletList')} onRun={(c) => c.toggleBulletList()}>•</Btn>
        <Btn label="Numbered List" active={editor.isActive('orderedList')} onRun={(c) => c.toggleOrderedList()}>1.</Btn>
        <Btn label="Task List" active={editor.isActive('taskList')} onRun={(c) => c.toggleTaskList()}>☑</Btn>
        <span className="rte-sep" />
        <Btn label="Quote" active={editor.isActive('blockquote')} onRun={(c) => c.toggleBlockquote()}>❝</Btn>
        <Btn label="Code Block" active={editor.isActive('codeBlock')} onRun={(c) => c.toggleCodeBlock()}>{'{ }'}</Btn>
        <Btn label="Divider" onRun={(c) => c.setHorizontalRule()}>—</Btn>
        <button type="button" className={'rte-btn' + (editor.isActive('link') ? ' on' : '')} title="Link" aria-label="Link"
          onMouseDown={(e) => { e.preventDefault(); setLink() }}>🔗</button>
        <span className="rte-sep" />
        <Btn label="Undo" disabled={!editor.can().undo()} onRun={(c) => c.undo()}>↶</Btn>
        <Btn label="Redo" disabled={!editor.can().redo()} onRun={(c) => c.redo()}>↷</Btn>
      </div>
      {notice && <p className="rte-notice" role="status">{notice}</p>}
      <EditorContent editor={editor} className="rte-content" />
    </div>
  )
}
