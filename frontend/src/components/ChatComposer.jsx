import MentionTextarea from './MentionTextarea'
import Tip from './Tip'

// Pill-shaped chat input. Enter sends (Shift+Enter = new line); there is no Send button,
// the ↵ icon just shows when there is something to send. While Claude works it turns into Stop.
export default function ChatComposer({ mentions = true, boardId, value, onChange, onSend, onStop, running, disabled, maxLength, placeholder = 'Message Claude…' }) {
  const can = !!value.trim() && !disabled
  return (
    <div className="composer-wrap">
    <div className="composer">
      <MentionTextarea
        boardId={boardId}
        rows={1}
        autoGrow
        maxLength={maxLength}
        placeholder={placeholder}
        value={value}
        disabled={running || disabled}
        onChange={onChange}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault()
            onSend()
          }
        }}
      />
      {running ? (
        <button type="button" className="composer-stop" onClick={onStop}><span className="spinner" aria-hidden="true" /> Stop</button>
      ) : (
        <span className={'composer-go' + (can ? ' on' : '')} aria-hidden="true">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M20 5v6a3 3 0 0 1-3 3H5M9 10l-4 4 4 4" /></svg>
        </span>
      )}
    </div>
    <div className="composer-hint">Enter sends · Shift+Enter new line{mentions ? ' · @ mentions a file or folder' : ''} · paste a screenshot to attach it</div>
    </div>
  )
}

// Header icon that starts a fresh Claude conversation (replaces the old text link under the input).
export function NewChatButton({ label, disabled, onClick }) {
  return (
    <Tip label={label} side="top">
      <button type="button" className="icon-btn" aria-label={label} disabled={disabled} onClick={onClick}>
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" /></svg>
      </button>
    </Tip>
  )
}

// "Continue" for a turn that stopped, failed, or ran out of steps: one click sends "continue".
export function ContinueChip({ messages, running, onContinue }) {
  const last = messages?.[messages.length - 1]
  if (running || last?.role !== 'assistant') return null
  if (!(last.status === 'stopped' || last.status === 'failed' || /Reply "continue"/.test(last.text))) return null
  return <button type="button" className="continue-chip" onClick={onContinue}>Continue ↵</button>
}
