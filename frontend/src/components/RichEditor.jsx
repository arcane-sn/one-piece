import { EditorContent, useEditor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { Markdown } from '@tiptap/markdown'
import { TaskItem, TaskList } from '@tiptap/extension-list'
import { Placeholder } from '@tiptap/extensions'

// WYSIWYG editor (Tiptap). The value is Markdown, so it stays readable for
// `claude -p` and for plain-text previews.
export default function RichEditor({ value, onChange, placeholder }) {
  const editor = useEditor({
    extensions: [
      StarterKit.configure({ link: { openOnClick: false } }),
      Markdown,
      TaskList,
      TaskItem.configure({ nested: true }),
      Placeholder.configure({ placeholder }),
    ],
    content: value,
    contentType: 'markdown',
    shouldRerenderOnTransaction: true, // keeps toolbar "active" states in sync
    onUpdate: ({ editor: ed }) => onChange(ed.getMarkdown().trimEnd()),
  })
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
    <div className="rte">
      <div className="rte-toolbar" role="toolbar" aria-label="Formatting">
        <select
          aria-label="Text Style"
          value={heading}
          onChange={(e) => {
            const v = e.target.value
            const c = editor.chain().focus()
            if (v === 'p') c.setParagraph().run()
            else c.setHeading({ level: Number(v) }).run()
          }}
        >
          <option value="p">Paragraph</option>
          <option value="1">Heading 1</option>
          <option value="2">Heading 2</option>
          <option value="3">Heading 3</option>
        </select>
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
      <EditorContent editor={editor} className="rte-content" />
    </div>
  )
}
