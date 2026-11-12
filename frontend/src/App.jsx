import { useCallback, useEffect, useState } from 'react'
import { BrowserRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { api } from './api'
import BoardModal from './components/BoardModal'
import Sidebar from './components/Sidebar'
import Toaster from './components/Toaster'
import Board from './pages/Board'
import Dashboard from './pages/Dashboard'
import Login from './pages/Login'

function Shell({ user, onLogout }) {
  const [boards, setBoards] = useState(null) // null = loading
  const [modal, setModal] = useState(null) // null | 'new' | Board (edit)
  const navigate = useNavigate()
  const reload = useCallback(() => api('/api/boards').then(setBoards).catch(() => setBoards((b) => b ?? [])), [])
  useEffect(() => { reload() }, [reload])
  const closeModal = useCallback(() => setModal(null), [])

  function saved(board) {
    const created = modal === 'new'
    setModal(null)
    reload()
    if (created) navigate(`/boards/${board.id}`)
  }

  return (
    <>
      <Sidebar user={user} boards={boards ?? []} onLogout={onLogout} onNewBoard={() => setModal('new')} />
      <main className="main">
        <Routes>
          <Route path="/" element={<Dashboard user={user} boards={boards} reload={reload} onNew={() => setModal('new')} onEdit={setModal} />} />
          <Route path="/boards/:id" element={<Board reloadBoards={reload} />} />
        </Routes>
      </main>
      <Toaster />
      {modal && (
        <BoardModal key={modal === 'new' ? 'new' : modal.id} initial={modal === 'new' ? undefined : modal} onSaved={saved} onClose={closeModal} />
      )}
    </>
  )
}

export default function App() {
  const [user, setUser] = useState(undefined) // undefined = loading, null = logged out

  useEffect(() => {
    api('/api/me').then((r) => setUser(r.username)).catch(() => setUser(null))
  }, [])

  if (user === undefined) return <p className="muted" style={{ padding: 24 }}>Loading…</p>
  if (!user) return <Login onLoggedIn={setUser} />

  async function logout() {
    await api('/api/logout', { method: 'POST' })
    setUser(null)
  }

  return (
    <BrowserRouter>
      <Shell user={user} onLogout={logout} />
    </BrowserRouter>
  )
}
