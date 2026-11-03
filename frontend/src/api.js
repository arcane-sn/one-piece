// Last path segment, e.g. "/Users/me/shop/web" -> "web".
export const baseName = (p) => p.split('/').filter(Boolean).pop() || p

// Thin fetch wrapper for the One Piece API.
// The backend rejects non-GET requests without a JSON content type, so we always send it.
export async function api(path, { method = 'GET', body } = {}) {
  const headers = {}
  if (method !== 'GET') headers['Content-Type'] = 'application/json'
  const res = await fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
  })
  if (res.status === 204) return null
  let data = null
  try {
    data = await res.json()
  } catch {
    // non-JSON body; fall through to the status check
  }
  if (!res.ok) {
    const err = new Error(data?.error || `HTTP ${res.status}`)
    err.status = res.status
    throw err
  }
  return data
}
