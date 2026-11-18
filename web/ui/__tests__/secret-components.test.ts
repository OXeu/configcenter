import { beforeEach, describe, expect, it, vi } from 'vite-plus/test'
import { flushPromises, mount } from '@vue/test-utils'
import WorkspacePage from '../src/WorkspacePage.vue'
import { createPage } from '../src/page.ts'
import { createWorkspace } from '../src/workspace.ts'
import { createSecretViews } from '../src/secrets.ts'
import type { PageData } from '../src/data.ts'

vi.mock('../src/toast.ts', () => ({ notify: vi.fn() }))

const plaintext = 'sk-confidential-middle-x123'
const row = { key: 'api/token', value: 'sk-****x123', secret: true, masked: true, description: '' }
const data: PageData = {
  page: 'project',
  project: { slug: 'billing', name: 'Billing' },
  env: 'production',
  publicUrl: '',
  providerName: '',
  nonce: '',
  oauthEnabled: false,
  flash: null,
  title: 'Billing',
  loggedIn: true,
  isAdmin: true,
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
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset().mockImplementation(async (url: string) => ({
    ok: true,
    status: 200,
    json: async () =>
      url.endsWith('/reveal')
        ? { value: plaintext }
        : {
            environments: [{ slug: 'production', name: 'Production' }],
            items: [{ ...row }, { key: 'public', value: 'visible', secret: false }],
            proposals: [{ ...row, id: 7, environment: 'production', action: 'set' }],
          },
  }))
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

async function render() {
  const wrapper = mount(WorkspacePage, {
    props: { data, page: createPage(data), title: 'Billing' },
  })
  await flushPromises()
  return wrapper
}

async function settleDialog() {
  await vi.dynamicImportSettled()
  await flushPromises()
}

describe('secret configuration', () => {
  it('shows only masks until explicitly revealing, then forgets plaintext on hide and refresh', async () => {
    const wrapper = await render()
    expect(wrapper.text()).toContain(row.value)
    expect(wrapper.text()).toContain('visible')
    expect(wrapper.html()).not.toContain(plaintext)
    expect(fetchMock.mock.calls.some(([url]) => url.endsWith('/reveal'))).toBe(false)
    const button = wrapper.get('button[aria-label="Show value for api/token"]')
    await button.trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/v1/projects/billing/environments/production/items/api%2Ftoken/reveal',
      expect.objectContaining({
        method: 'POST',
        headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }),
      }),
    )
    expect(wrapper.text()).toContain(plaintext)
    await wrapper.get('button[aria-label="Hide value for api/token"]').trigger('click')
    expect(wrapper.text()).not.toContain(plaintext)
    await button.trigger('click')
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((control) => control.text() === 'Refresh')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain(plaintext)
    wrapper.unmount()
  })

  it('preserves an unread secret when editing metadata without sending the mask', async () => {
    const wrapper = await render()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Edit')!
      .trigger('click')
    await settleDialog()
    const dialog = wrapper.get('dialog')
    expect(dialog.get('[role="checkbox"]').attributes('aria-checked')).toBe('true')
    expect(dialog.find('textarea').exists()).toBe(false)
    expect(dialog.text()).toContain(row.value)
    await dialog.findAll('input').at(-1)!.setValue('New description')
    await dialog.get('form').trigger('submit')
    await flushPromises()
    const write = fetchMock.mock.calls.find(([, init]) => init.method === 'PUT')!
    expect(JSON.parse(write[1].body)).toEqual({
      key: row.key,
      description: 'New description',
      secret: true,
    })
    expect(write[1].body).not.toContain(row.value)
    expect(write[1].body).not.toContain(plaintext)
    wrapper.unmount()
  })

  it('reveals only through authorization in the dialog and sends an explicitly edited replacement', async () => {
    const wrapper = await render()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Edit')!
      .trigger('click')
    await settleDialog()
    const dialog = wrapper.get('dialog')
    await dialog
      .findAll('button')
      .find((button) => button.text() === 'Show')!
      .trigger('click')
    await flushPromises()
    expect(dialog.get<HTMLTextAreaElement>('textarea').element.value).toBe(plaintext)
    await dialog.get('textarea').setValue('replacement')
    await dialog.get('form').trigger('submit')
    await flushPromises()
    const write = fetchMock.mock.calls.find(([, init]) => init.method === 'PUT')!
    expect(JSON.parse(write[1].body)).toMatchObject({ value: 'replacement', secret: true })
    wrapper.unmount()
  })

  it('allows replacing a secret without first retrieving its plaintext', async () => {
    const wrapper = await render()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Edit')!
      .trigger('click')
    await settleDialog()
    const dialog = wrapper.get('dialog')
    await dialog
      .findAll('button')
      .find((button) => button.text() === 'Replace value')!
      .trigger('click')
    expect(dialog.get<HTMLTextAreaElement>('textarea').element.value).toBe('')
    await dialog.get('textarea').setValue('')
    await dialog.get('form').trigger('submit')
    await flushPromises()
    expect(fetchMock.mock.calls.some(([url]) => url.endsWith('/reveal'))).toBe(false)
    const write = fetchMock.mock.calls.find(([, init]) => init.method === 'PUT')!
    expect(JSON.parse(write[1].body)).toMatchObject({ value: '', secret: true })
    wrapper.unmount()
  })

  it('masks proposal values and uses the proposal-specific privileged path', async () => {
    const wrapper = await render()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Proposals')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain(plaintext)
    await wrapper.get('button[aria-label="Show value for api/token"]').trigger('click')
    await flushPromises()
    expect(fetchMock.mock.calls.at(-1)![0]).toBe('/api/v1/projects/billing/proposals/7/reveal')
    expect(wrapper.text()).toContain(plaintext)
    wrapper.unmount()
  })

  it('supports the secret option on new fields and clears drafts after closing', async () => {
    const workspace = createWorkspace(data, createPage(data))
    workspace.edit()
    expect(workspace.form.secret).toBe(false)
    workspace.form.secret = true
    workspace.form.values.key = 'new'
    workspace.form.values.value = plaintext
    await workspace.form.submit()
    await flushPromises()
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toMatchObject({
      value: plaintext,
      secret: true,
    })
    expect(workspace.form.values).toEqual({})
  })

  it('discards pending reveals after navigation and reports denied reads without displaying plaintext', async () => {
    let finish!: (value: string) => void
    const read = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          finish = resolve
        }),
    )
    const report = vi.fn()
    const secrets = createSecretViews(read, report)
    const pending = secrets.toggle(row)
    await secrets.toggle(row)
    expect(read).toHaveBeenCalledTimes(1)
    secrets.reset()
    finish(plaintext)
    await pending
    expect(secrets.values.size).toBe(0)
    read.mockRejectedValueOnce(new Error('denied'))
    await secrets.toggle(row)
    expect(report).toHaveBeenCalledTimes(1)
    expect(secrets.values.size).toBe(0)
  })

  it('does not replace a masked registration setting without an explicit selection', async () => {
    const workspace = createWorkspace({ ...data, page: 'settings' }, createPage(data))
    const setting = { key: 'allow_registration', secret: true, value: '****' }
    expect(workspace.editChoices(setting).every((choice) => !choice.selected)).toBe(true)
    workspace.choose(setting, '')
    expect(workspace.form.open).toBe(false)
    expect(fetchMock).not.toHaveBeenCalled()
    workspace.choose(setting, 'true')
    expect(workspace.form.fields).toEqual([])
    expect(fetchMock).not.toHaveBeenCalled()
    await workspace.form.submit()
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ allow_registration: true })
  })

  it('discards a secret returned after its edit dialog has closed', async () => {
    const workspace = createWorkspace(data, createPage(data))
    workspace.edit(row)
    let finish!: (value: string) => void
    workspace.form.readSecret = () =>
      new Promise<string>((resolve) => {
        finish = resolve
      })
    const pending = workspace.form.toggleValue()
    workspace.form.open = false
    await flushPromises()
    finish(plaintext)
    await pending
    expect(workspace.form.values).toEqual({})
    expect(workspace.form.valueRevealed).toBe(false)
  })
})
