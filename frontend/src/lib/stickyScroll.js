import { useEffect, useRef } from 'react'

// Keeps a chat list pinned to the bottom only while the reader is at the bottom. Polling replaces the messages
// every second, so scrolling up to read must not be undone; sending a message pins it again.
export function useStickyScroll(listRef, messages, resetKey) {
  const stuck = useRef(true)
  const lastCount = useRef(0)
  useEffect(() => {
    const el = listRef.current
    if (!el) return
    const onScroll = () => { stuck.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80 }
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  })
  useEffect(() => { stuck.current = true }, [resetKey])
  useEffect(() => {
    const el = listRef.current
    const n = messages?.length ?? 0
    if (n > lastCount.current && messages[n - 1]?.role === 'user') stuck.current = true // the owner just sent
    lastCount.current = n
    if (el && stuck.current) el.scrollTop = el.scrollHeight
  }, [messages, listRef])
}
