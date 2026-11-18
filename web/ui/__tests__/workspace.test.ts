import type { PageData } from '../src/data.ts'
import { describe, it, expect, vi, beforeEach } from 'vite-plus/test'
import { createWorkspace } from '../src/workspace.ts'
import { createPage } from '../src/page.ts'
import { api } from '../src/core.ts'
vi.mock('../src/toast.ts', () => ({ notify: vi.fn() }))
const data: PageData = {
  publicUrl: '',
  providerName: '',
  nonce: '',
  oauthEnabled: false,
  flash: null,
  title: '',
  loggedIn: false,
  isAdmin: false,
  canWrite: false,
  me: null,
  languages: [],
  languageLabel: '',
  page: 'project',
  project: { slug: 'billing', name: 'Billing' },
  env: 'production',
  csrf: 'csrf',
  lang: 'en',
  messages: {},
}
const fetchMock = vi.fn()
beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => ({ items: [], keys: [], proposals: [], environments: [] }),
  })
})
describe('workspace mutations', () => {
  it('encodes the original item key and sends edited values with CSRF', async () => {
    const w = createWorkspace(data, createPage(data))
    w.edit({ key: 'old.key', value: 'one', description: '' })
    w.form.values.key = 'new.key'
    w.form.values.value = 'two'
    await w.form.submit()
    expect(fetchMock.mock.calls[0][0]).toBe(
      '/api/v1/projects/billing/environments/production/items/old.key',
    )
    expect(fetchMock.mock.calls[0][1]).toMatchObject({
      method: 'PUT',
      headers: { 'X-CSRF-Token': 'csrf' },
      body: JSON.stringify({ key: 'new.key', value: 'two', description: '', secret: false }),
    })
    expect(w.form.open).toBe(false)
  })
  it('keeps a newly issued token visible until the dialog closes', async () => {
    const w = createWorkspace(data, createPage(data))
    w.state.tab = 'keys'
    w.edit()
    fetchMock.mockResolvedValueOnce({
      ok: true,
      status: 201,
      json: async () => ({ token: 'cc_once' }),
    })
    await w.form.submit()
    expect(w.form.token).toBe('cc_once')
    expect(w.form.open).toBe(true)
  })
  it('keeps failed edits open with a translated error', async () => {
    const w = createWorkspace(data, createPage(data))
    w.edit()
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 409,
      json: async () => ({ error: 'duplicate' }),
    })
    await w.form.submit()
    expect(w.form.open).toBe(true)
    expect(w.form.error).toBe(
      'This record changed or conflicts with an existing record. Refresh and try again.',
    )
  })
  it('requires confirmation before deleting or reviewing a proposal', async () => {
    const w = createWorkspace(data, createPage(data))
    w.state.tab = 'proposals'
    w.review({ id: 7, key: 'a' }, false)
    expect(fetchMock).not.toHaveBeenCalled()
    await w.form.submit()
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/projects/billing/proposals/7/review')
    expect(fetchMock.mock.calls[0][1].body).toBe('{"approve":false}')
  })
  it('changes environment before requesting its items', async () => {
    const w = createWorkspace(data, createPage(data))
    w.state.environments = [{ slug: 'staging', name: 'Staging', is_primary: false }]
    await w.environment('staging')
    expect(w.form.open).toBe(false)
    expect(fetchMock.mock.calls[1][0]).toContain('/environments/staging/items')
  })
  it('ignores current, unknown and busy environment selections', async () => {
    const w = createWorkspace(data, createPage(data))
    w.state.environments = [{ slug: 'staging', name: 'Staging', is_primary: false }]
    await w.environment('production')
    await w.environment('unknown')
    w.state.busy = true
    await w.environment('staging')
    expect(fetchMock).not.toHaveBeenCalled()
    expect(w.state.env).toBe('production')
  })
  it('reports failed environment reads and permits retrying', async () => {
    const page = createPage(data)
    const report = vi.spyOn(page, 'report')
    const w = createWorkspace(data, page)
    w.state.environments = [{ slug: 'staging', name: 'Staging', is_primary: false }]
    w.state.rows = [{ key: 'old' }]
    const error = new TypeError('Failed to fetch')
    fetchMock.mockRejectedValueOnce(error)
    await w.environment('staging')
    expect(report).toHaveBeenCalledWith(error)
    expect(w.state.rows).toEqual([])
    expect(w.state.busy).toBe(false)
    expect(w.form.open).toBe(false)
    await w.refresh()
    expect(fetchMock.mock.calls.at(-1)![0]).toContain('/environments/staging/items')
  })
  it('updates user roles through the admin API', async () => {
    const w = createWorkspace({ ...data, page: 'users' }, createPage(data))
    w.choose({ id: 9, role: 'user', email: 'user@example.com' }, 'admin')
    expect(fetchMock).not.toHaveBeenCalled()
    expect(w.form.fields).toEqual([])
    await w.form.submit()
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/users/9')
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'PATCH', body: '{"role":"admin"}' })
  })
  it('accepts empty successful deletion responses', async () => {
    fetchMock.mockResolvedValueOnce({ ok: true, status: 204 })
    expect(await api('/item', { method: 'DELETE' })).toBeNull()
  })
})

it('creates, updates and deletes projects through their own routes', async () => {
  const w = createWorkspace({ ...data, page: 'projects' }, createPage(data))
  w.edit()
  Object.assign(w.form.values, { slug: 'new', name: 'New', description: '' })
  await w.form.submit()
  expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/projects')
  expect(fetchMock.mock.calls[0][1].method).toBe('POST')
  fetchMock.mockClear()
  w.edit({ slug: 'new', name: 'New', description: '' })
  w.form.values.name = 'Renamed'
  await w.form.submit()
  expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/projects/new')
  expect(fetchMock.mock.calls[0][1].method).toBe('PATCH')
  fetchMock.mockClear()
  w.remove({ slug: 'new', name: 'Renamed' })
  expect(fetchMock).not.toHaveBeenCalled()
  await w.form.submit()
  expect(fetchMock.mock.calls[0][1].method).toBe('DELETE')
})

it('reports network failures without discarding form values', async () => {
  const w = createWorkspace(data, createPage(data))
  w.edit()
  w.form.values.key = 'keep'
  fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'))
  await w.form.submit()
  expect(w.form.values.key).toBe('keep')
  expect(w.form.error).toBe('Network error, try again.')
  expect(w.form.busy).toBe(false)
})

it('creates and rotates keys using the unified routes', async () => {
  const w = createWorkspace({ ...data, page: 'keys', project: null }, createPage(data))
  w.edit()
  expect(w.form.fields).toHaveLength(1)
  expect(w.form.values.role).toBeUndefined()
  w.form.scopes = [{ id: 1, project: 'billing', permissions: ['config:read'] }]
  w.form.values.name = 'Automation'
  fetchMock.mockResolvedValueOnce({
    ok: true,
    status: 201,
    json: async () => ({ token: 'cc_global' }),
  })
  await w.form.submit()
  expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/keys')
  expect(w.form.token).toBe('cc_global')
  fetchMock.mockClear()
  w.actionsFor({ id: 42, name: 'Automation' })[0].request()
  expect(fetchMock).not.toHaveBeenCalled()
  await w.form.submit()
  expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/keys/42/rotate')
  fetchMock.mockClear()
  w.actionsFor({ id: 42, name: 'Automation' })[1].request()
  expect(fetchMock).not.toHaveBeenCalled()
  await w.form.submit()
  expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/keys/42')
  expect(fetchMock.mock.calls[0][1].method).toBe('DELETE')
})

it('edits independent project scopes without mutating the displayed key before save', async () => {
  const w = createWorkspace({ ...data, page: 'keys' }, createPage(data))
  const row = {
    id: 7,
    name: 'Reader',
    scopes: [{ project: 'billing', permissions: ['config:read'] }],
  }
  w.edit(row)
  w.form.scopes![0].permissions.push('config:write')
  w.form.scopes!.push({ id: 2, project: 'shipping', permissions: ['config:read'] })
  expect(row.scopes).toEqual([{ project: 'billing', permissions: ['config:read'] }])
  await w.form.submit()
  expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/keys/7')
  expect(fetchMock.mock.calls[0][1]).toMatchObject({
    method: 'PATCH',
    body: JSON.stringify({
      name: 'Reader',
      scopes: [
        { project: 'billing', permissions: ['config:read', 'config:write'] },
        { project: 'shipping', permissions: ['config:read'] },
      ],
    }),
  })
})

it('validates incomplete scopes before sending a request and permits explicit no-access keys', async () => {
  const w = createWorkspace({ ...data, page: 'keys' }, createPage(data))
  w.edit()
  w.form.scopes = [{ id: 1, project: 'billing', permissions: [] }]
  await w.form.submit()
  expect(fetchMock).not.toHaveBeenCalled()
  expect(w.form.error).toBe('Choose a project and at least one permission for each scope.')
  w.form.excludeScope(1)
  await w.form.submit()
  expect(fetchMock.mock.calls[0][1].body).toBe(JSON.stringify({ name: '', scopes: [] }))
})

it('falls back to the primary environment when the selected environment no longer exists', async () => {
  const w = createWorkspace({ ...data, env: 'deleted' }, createPage(data))
  fetchMock.mockResolvedValueOnce({
    ok: true,
    status: 200,
    json: async () => ({
      environments: [
        { slug: 'alpha', name: 'Alpha', is_primary: false },
        { slug: 'production', name: 'Production', is_primary: true },
      ],
    }),
  })
  await w.refresh()
  expect(w.state.env).toBe('production')
  expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/projects/billing/environments/production/items')
})

it('retains a valid selected environment even when another environment is primary', async () => {
  const w = createWorkspace({ ...data, env: 'staging' }, createPage(data))
  fetchMock.mockResolvedValueOnce({
    ok: true,
    status: 200,
    json: async () => ({
      environments: [
        { slug: 'production', name: 'Production', is_primary: true },
        { slug: 'staging', name: 'Staging', is_primary: false },
      ],
    }),
  })
  await w.refresh()
  expect(w.state.env).toBe('staging')
  expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/projects/billing/environments/staging/items')
})
