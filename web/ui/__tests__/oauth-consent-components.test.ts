import { afterEach, beforeEach, expect, it, vi } from 'vite-plus/test'
import { flushPromises, mount } from '@vue/test-utils'
import OAuthConsentPage from '../src/OAuthConsentPage.vue'
import { createCopy } from '../src/i18n.ts'
import { pages, type PageData } from '../src/data.ts'

const data: PageData = {
  page: 'oauth-consent',
  title: 'oauth.title',
  lang: 'en',
  publicUrl: 'https://cc.example',
  providerName: 'SSO',
  nonce: '',
  csrf: 'consent-csrf',
  loggedIn: true,
  isAdmin: false,
  oauthEnabled: true,
  me: { id: 1, name: 'Owner', email: 'owner@example.com', role: 'user' },
  project: null,
  env: '',
  canWrite: true,
  languageLabel: 'English',
  languages: [{ tag: 'en', label: 'English' }],
  messages: {},
  flash: null,
  consent: {
    request: 'request-id',
    clientName: 'External client',
    redirectUri: 'https://client.example/callback',
    resource: 'https://cc.example/mcp',
  },
}

const report = vi.fn()
function consent(lang = 'en') {
  return mount(OAuthConsentPage, {
    props: {
      data: { ...data, lang },
      page: { msg: createCopy(lang).msg, csrf: data.csrf, describe: String, report },
    },
  })
}

beforeEach(() => {
  report.mockReset()
  vi.stubGlobal('fetch', vi.fn())
  vi.stubGlobal('location', { assign: vi.fn() })
})
afterEach(() => vi.unstubAllGlobals())

it.each(['en', 'zh-CN'])(
  'requires explicit consent and shows the client and access in %s',
  (lang) => {
    const wrapper = consent(lang)
    expect(pages).toContain('oauth-consent')
    expect(wrapper.text()).toContain('External client')
    expect(wrapper.text()).toContain('owner@example.com')
    expect(wrapper.text()).toContain('https://client.example/callback')
    expect(wrapper.text().toLowerCase()).toContain('api key')
    expect(fetch).not.toHaveBeenCalled()
    wrapper.unmount()
  },
)

it.each([true, false])(
  'submits the %s decision once with CSRF and locks both actions',
  async (approve) => {
    let finish!: (response: Response) => void
    vi.mocked(fetch).mockImplementation(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve
        }),
    )
    const wrapper = consent()
    const buttons = wrapper.findAll('button')
    await buttons[approve ? 0 : 1]!.trigger('click')
    await buttons[0]!.trigger('click')
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith(
      '/oauth/consent',
      expect.objectContaining({
        method: 'POST',
        headers: expect.objectContaining({ 'X-CSRF-Token': 'consent-csrf' }),
        body: JSON.stringify({ request: 'request-id', approve }),
      }),
    )
    for (const button of buttons) expect(button.attributes('disabled')).toBeDefined()
    const redirect = `https://client.example/callback?${approve ? 'code=one' : 'error=access_denied'}`
    finish(new Response(JSON.stringify({ redirect })))
    await flushPromises()
    expect(location.assign).toHaveBeenCalledWith(redirect)
    wrapper.unmount()
  },
)

it('reports failures and allows retry without navigating', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ error: 'invalid_request' }), { status: 400 }),
  )
  const wrapper = consent()
  await wrapper.get('button').trigger('click')
  await flushPromises()
  expect(report).toHaveBeenCalledTimes(1)
  expect(location.assign).not.toHaveBeenCalled()
  expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
  wrapper.unmount()
})

it('rejects a redirect outside the displayed callback', async () => {
  vi.mocked(fetch).mockResolvedValue(
    new Response(JSON.stringify({ redirect: 'https://other.example/callback?code=one' })),
  )
  const wrapper = consent()
  await wrapper.get('button').trigger('click')
  await flushPromises()
  expect(report).toHaveBeenCalledTimes(1)
  expect(location.assign).not.toHaveBeenCalled()
  wrapper.unmount()
})
