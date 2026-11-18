import { beforeEach, expect, it, vi } from 'vite-plus/test'
import { flushPromises, mount } from '@vue/test-utils'
import WorkspacePage from '../src/WorkspacePage.vue'
import { createPage } from '../src/page.ts'
import type { PageData } from '../src/data.ts'

vi.mock('../src/toast.ts', () => ({ notify: vi.fn() }))
const data: PageData = {
  page: 'project',
  project: { slug: 'billing', name: 'Billing' },
  env: 'staging',
  publicUrl: '',
  providerName: '',
  nonce: '',
  oauthEnabled: false,
  flash: null,
  title: 'Billing',
  loggedIn: true,
  isAdmin: false,
  canWrite: true,
  me: null,
  languages: [],
  languageLabel: '',
  csrf: 'csrf',
  lang: 'en',
  messages: {},
}
const fetchMock = vi.fn()
beforeEach(() => {
  let environments = [
    { slug: 'production', name: 'Production', is_primary: true },
    { slug: 'staging', name: 'Staging', is_primary: false },
  ]
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset().mockImplementation(async (url: string, init: RequestInit) => {
    let result: unknown = { environments, items: [] }
    if (init.method === 'PATCH') {
      result = JSON.parse(init.body as string) as (typeof environments)[number]
      environments = [environments[0], result as (typeof environments)[number]]
    }
    if (url.endsWith('/primary')) {
      environments = environments.map((env) => ({
        ...env,
        is_primary: url.endsWith(`/${env.slug}/primary`),
      }))
    }
    if (init.method === 'DELETE') environments = environments.filter((e) => !url.endsWith(e.slug))
    return { ok: true, status: init.method === 'DELETE' ? 204 : 200, json: async () => result }
  })
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})
async function render(canWrite = true, env = data.env) {
  const wrapper = mount(WorkspacePage, {
    props: { data: { ...data, canWrite, env }, page: createPage(data), title: 'Billing' },
  })
  await flushPromises()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Environments')!
    .trigger('click')
  await flushPromises()
  return wrapper
}
async function settle() {
  await vi.dynamicImportSettled()
  await flushPromises()
}
it('opens the environment menu directly and loads the selected environment without a dialog', async () => {
  const wrapper = await render(false)
  await wrapper
    .findAll('button')
    .find((button) => button.text() === 'Items')!
    .trigger('click')
  await flushPromises()
  fetchMock.mockClear()
  const trigger = wrapper.get('button[aria-label="Environment: staging"]')
  await trigger.trigger('click')
  expect(trigger.attributes('aria-expanded')).toBe('true')
  expect(wrapper.find('dialog').exists()).toBe(false)
  const panel = wrapper.get('.dropdown-panel')
  expect(panel.text()).toContain('Production (production)')
  expect(panel.text()).toContain('— Primary')
  expect(panel.get('[aria-pressed="true"]').text()).toBe('Staging (staging)')
  expect(fetchMock).not.toHaveBeenCalled()
  await panel.get('[aria-pressed="false"]').trigger('click')
  await settle()
  expect(wrapper.find('dialog').exists()).toBe(false)
  expect(wrapper.find('.dropdown-panel').exists()).toBe(false)
  expect(wrapper.text()).toContain('Environment: production')
  expect(fetchMock).toHaveBeenCalledWith(
    '/api/v1/projects/billing/environments/production/items',
    expect.objectContaining({ method: 'GET' }),
  )
  wrapper.unmount()
})
it('closes the environment menu with Escape without switching', async () => {
  const wrapper = await render()
  fetchMock.mockClear()
  const trigger = wrapper.get('button[aria-label="Environment: staging"]')
  await trigger.trigger('click')
  await trigger.trigger('keydown', { key: 'Escape' })
  expect(trigger.attributes('aria-expanded')).toBe('false')
  expect(wrapper.find('dialog').exists()).toBe(false)
  expect(fetchMock).not.toHaveBeenCalled()
  wrapper.unmount()
})
it.each([
  {
    page: 'users' as const,
    rows: [{ id: 9, email: 'user@example.com', role: 'user' }],
    label: 'Admin',
    url: '/api/v1/users/9',
    method: 'PATCH',
    body: { role: 'admin' },
  },
  {
    page: 'settings' as const,
    rows: [{ key: 'allow_registration', value: false }],
    label: 'Allowed',
    url: '/api/v1/settings',
    method: 'PUT',
    body: { allow_registration: true },
  },
])(
  'selects $page choices directly and preserves save confirmation and cancellation',
  async ({ page, rows, label, url, method, body }) => {
    const pageData = { ...data, page, isAdmin: true }
    fetchMock.mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ [page]: rows }) })
    const wrapper = mount(WorkspacePage, {
      props: { data: pageData, page: createPage(pageData), title: page },
    })
    await flushPromises()
    fetchMock.mockClear()
    const trigger = wrapper.get('button[aria-label="Edit"]')
    await trigger.trigger('click')
    expect(wrapper.find('dialog').exists()).toBe(false)
    const select = async () => {
      await wrapper
        .get('.dropdown-panel')
        .findAll('button')
        .find((button) => button.text() === label)!
        .trigger('click')
      await settle()
    }
    await select()
    expect(wrapper.get('dialog').text()).toContain(label)
    expect(wrapper.find('dialog .dropdown').exists()).toBe(false)
    expect(fetchMock).not.toHaveBeenCalled()
    await wrapper.get('dialog button[type="button"]').trigger('click')
    await settle()
    expect(fetchMock).not.toHaveBeenCalled()
    await trigger.trigger('click')
    await select()
    await wrapper.get('dialog form').trigger('submit')
    await settle()
    expect(fetchMock).toHaveBeenCalledWith(
      url,
      expect.objectContaining({
        method,
        body: JSON.stringify(body),
        headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }),
      }),
    )
    wrapper.unmount()
  },
)
it('confirms environment renaming, preserves cancellation and follows the selected slug', async () => {
  const wrapper = await render()
  const row = wrapper.findAll('tbody tr')[1]
  expect(row.find('[data-danger-action]').exists()).toBe(false)
  await row
    .findAll('button')
    .find((b) => b.text() === 'More')!
    .trigger('click')
  await row.get('[data-danger-action="renameEnvironment"]').trigger('click')
  await settle()
  expect(wrapper.get('dialog').text()).toContain('breaks existing business connections')
  expect(wrapper.get('dialog button[type="submit"]').classes()).toContain('btn-danger')
  await wrapper.get('dialog input').setValue('preview')
  await wrapper.get('dialog button[type="button"]').trigger('click')
  expect(fetchMock.mock.calls.some(([, init]) => init.method === 'PATCH')).toBe(false)
  await row
    .findAll('button')
    .find((b) => b.text() === 'More')!
    .trigger('click')
  await row.get('[data-danger-action="renameEnvironment"]').trigger('click')
  await settle()
  expect((wrapper.get('dialog input').element as HTMLInputElement).value).toBe('staging')
  await wrapper.get('dialog input').setValue('preview')
  await wrapper.get('dialog form').trigger('submit')
  await settle()
  expect(fetchMock).toHaveBeenCalledWith(
    '/api/v1/projects/billing/environments/staging',
    expect.objectContaining({
      method: 'PATCH',
      headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }),
      body: '{"slug":"preview","name":"Staging"}',
    }),
  )
  expect(wrapper.text()).toContain('Environment: preview')
  wrapper.unmount()
})
it('confirms deletion and switches away from the deleted environment', async () => {
  const wrapper = await render()
  const row = wrapper.findAll('tbody tr')[1]
  await row
    .findAll('button')
    .find((b) => b.text() === 'More')!
    .trigger('click')
  await row.get('[data-danger-action="deleteEnvironment"]').trigger('click')
  await settle()
  expect(wrapper.get('dialog').text()).toContain('all its configuration and proposals')
  expect(fetchMock.mock.calls.some(([, init]) => init.method === 'DELETE')).toBe(false)
  await wrapper.get('dialog form').trigger('submit')
  await settle()
  expect(wrapper.findAll('tbody tr')).toHaveLength(1)
  expect(wrapper.text()).toContain('Environment: production')
  wrapper.unmount()
})
it('hides environment mutations without write access', async () => {
  const wrapper = await render(false)
  expect(wrapper.find('tbody button').exists()).toBe(false)
  expect(wrapper.text()).not.toContain('New environment')
  wrapper.unmount()
})

it('sets the project primary environment without changing the current selection', async () => {
  const wrapper = await render(true, 'production')
  expect(wrapper.findAll('tbody tr')[0].get('.badge').text()).toBe('Primary')
  expect(wrapper.findAll('tbody tr')[1].find('.badge').exists()).toBe(false)
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Set primary')!
    .trigger('click')
  await settle()
  expect(wrapper.get('dialog').text()).toContain('will open by default')
  expect(fetchMock.mock.calls.some(([url]) => url.endsWith('/primary'))).toBe(false)
  await wrapper.get('dialog form').trigger('submit')
  await settle()
  expect(fetchMock).toHaveBeenCalledWith(
    '/api/v1/projects/billing/environments/staging/primary',
    expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }),
    }),
  )
  expect(wrapper.findAll('tbody tr')[1].get('.badge').text()).toBe('Primary')
  expect(wrapper.findAll('tbody tr')[0].find('.badge').exists()).toBe(false)
  expect(wrapper.text()).toContain('Environment: production')
  wrapper.unmount()
})
