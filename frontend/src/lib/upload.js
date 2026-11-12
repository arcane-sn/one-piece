import { api } from '../api'

export const readDataURL = (blob) => new Promise((resolve, reject) => {
  const r = new FileReader()
  r.onload = () => resolve(r.result)
  r.onerror = () => reject(r.error)
  r.readAsDataURL(blob)
})

// Stores an image on the server and returns its /api/uploads/... url.
export async function uploadImage(blob) {
  const { url } = await api('/api/uploads', { method: 'POST', body: { data: await readDataURL(blob) } })
  return url
}
