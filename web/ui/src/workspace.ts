import { dangerAction, dangerOperations } from './danger.ts'
import { reactive } from 'vue'
import { api } from './core.ts'
import { createForm } from './forms.ts'
import { notify } from './toast.ts'
import { createSecretViews, type SecretRow } from './secrets.ts'

export type Environment = { slug: string; name: string; is_primary: boolean }

/** @param {string} value */
const enc = (value: string) => encodeURIComponent(value)
/** @param {string} key @param {string} label @param {boolean} [required] */
const field = (key: string, label: string, required: boolean = true) => ({ key, label, required })

/** @param {import('./data.ts').PageData} data @param {import('./page.ts').PageContext} page */
export function createWorkspace(
  data: import('./data.ts').PageData,
  page: import('./page.ts').PageContext,
) {
  const form = createForm(page)
  const state = reactive({
    rows: /** @type {any[]} */ ([]) as any[],
    environments: [] as Environment[],
    tab: data.page === 'keys' ? 'keys' : 'items',
    view: 'all',
    env: data.env,
    busy: false,
    scopeProjects: [] as import('./forms.ts').Choice[],
    scopePermissions: [] as import('./forms.ts').Choice[],
    globalPermissions: [] as string[],
  })
  const base = '/api/v1/projects/' + encodeURIComponent(data.project?.slug ?? '')
  function revealURL(row: SecretRow) {
    if (data.page === 'settings')
      return '/api/v1/projects/configcenter/environments/production/items/allow_registration/reveal'
    if (state.tab === 'proposals') return `${base}/proposals/${row.id}/reveal`
    return `${base}/environments/${enc(state.env)}/items/${enc(row.key)}/reveal`
  }
  async function readSecret(url: string): Promise<string> {
    const result = (await api(url, { method: 'POST', csrf: page.csrf })) as { value: string }
    return result.value
  }
  const secrets = createSecretViews((row) => readSecret(revealURL(row)), page.report)
  const path = () =>
    state.tab === 'keys'
      ? '/api/v1/keys'
      : data.page === 'settings'
        ? '/api/v1/settings'
        : data.page === 'projects'
          ? '/api/v1/projects'
          : data.page === 'users'
            ? '/api/v1/users'
            : state.tab === 'items'
              ? `${base}/environments/${enc(state.env)}/items`
              : `${base}/${state.tab}`
  /** @returns {Promise<void>} */
  async function load(): Promise<void> {
    secrets.reset()
    state.busy = true
    try {
      if (data.page === 'project') {
        const result = (await api(`${base}/environments`)) as { environments: Environment[] }
        state.environments = result.environments
        if (state.environments.length && !state.environments.some((env) => env.slug === state.env))
          state.env = (
            state.environments.find((env) => env.is_primary) ?? state.environments[0]
          ).slug
        if (state.tab === 'environments') {
          state.rows = state.environments
          return
        }
      }
      if (state.tab === 'keys') {
        const options = (await api('/api/v1/key-scopes')) as {
          projects: { slug: string; name: string }[]
          permissions: string[]
          global_permissions: string[]
          all_projects: boolean
        }
        state.scopeProjects = (options.projects ?? []).map((p) => ({
          value: p.slug,
          label: `${p.name} (${p.slug})`,
        }))
        if (options.all_projects)
          state.scopeProjects.push({ value: '*', label: page.msg('keys.allProjects') })
        state.globalPermissions = options.global_permissions ?? []
        state.scopePermissions = (options.permissions ?? []).map((value) => ({
          value,
          label: page.msg(`scope.${value}`),
        }))
      }
      const query =
        data.page === 'projects'
          ? '?view=' + state.view
          : data.page === 'project' && state.tab === 'keys'
            ? '?project=' + enc(data.project!.slug)
            : state.tab === 'proposals'
              ? '?status=pending'
              : ''
      const result = await api(path() + query)
      const collection = ['project', 'keys'].includes(data.page) ? state.tab : data.page
      state.rows = result[collection] ?? []
    } finally {
      state.busy = false
    }
  }
  /** @returns {Promise<void>} */
  async function refresh(): Promise<void> {
    try {
      await load()
    } catch (err) {
      page.report(err)
    }
  }
  /** @param {string} tab */
  async function select(tab: string) {
    if (state.busy) return
    state.tab = tab
    state.rows = []
    await refresh()
  }
  const roles = () => ({
    key: 'role',
    label: 'dialog.role',
    options: ['user', 'admin'].map((value) => ({
      value,
      label: page.msg(`role.${value}`),
    })),
  })
  /** @param {string} url @param {string} method @param {unknown} [body] */
  async function mutate(url: string, method: string, body?: unknown) {
    const result = await api(url, { method, body, csrf: page.csrf })
    notify('success', page.msg('toast.saved'))
    await refresh()
    return result
  }
  /** @param {any} [row] */
  function edit(row?: any) {
    if (state.tab === 'environments') {
      newEnvironment()
      return
    }
    if (data.page === 'settings') {
      form.show(
        page.msg('common.edit'),
        [
          {
            key: 'allowed',
            label: 'settings.registration',
            required: true,
            options: [
              { value: 'true', label: page.msg('settings.allowed') },
              { value: 'false', label: page.msg('settings.closed') },
            ],
          },
        ],
        { allowed: row.secret ? '' : String(row.value) },
        async () => {
          if (!['true', 'false'].includes(form.values.allowed))
            throw new Error(page.msg('settings.chooseRegistration'), { cause: 422 })
          await mutate(path(), 'PUT', { allow_registration: form.values.allowed === 'true' })
        },
      )
      return
    }
    if (state.tab === 'keys') {
      editKey(row)
      return
    }
    const editing = Boolean(row)
    let fields,
      values,
      url = path(),
      method = 'POST'
    if (data.page === 'projects') {
      fields = [
        ...(editing ? [] : [field('slug', 'common.slug')]),
        field('name', 'common.name'),
        field('description', 'common.description', false),
      ]
      values = row ?? { slug: '', name: '', description: '' }
      if (editing) {
        url += '/' + enc(row.slug)
        method = 'PATCH'
      }
    } else if (data.page === 'users') {
      fields = [roles()]
      values = { role: row.role }
      url += '/' + row.id
      method = 'PATCH'
    } else {
      fields = [
        field('key', 'dialog.itemKey'),
        { ...field('value', 'dialog.itemValue', false), multiline: true, sensitive: true },
        field('description', 'common.description', false),
      ]
      values = {
        key: row?.key ?? '',
        value: row?.secret ? '' : (row?.value ?? ''),
        description: row?.description ?? '',
      }
      method = 'PUT'
    }
    form.show(page.msg(editing ? 'common.edit' : 'common.create'), fields, values, async () => {
      const target = method === 'PUT' ? url + '/' + enc(row?.key ?? form.values.key) : url
      const body: Record<string, string | boolean> = { ...form.values }
      if (form.secret !== null) body.secret = form.secret
      if (form.keepSecret) delete body.value
      const result = await mutate(target, method, body)
      if (result?.token) form.token = result.token
    })
    if (data.page === 'project' && state.tab === 'items') form.secret = row?.secret ?? false
    if (row?.secret && form.secret !== null) {
      form.keepSecret = true
      form.secretMask = row.value
      const revealPath = revealURL(row)
      form.readSecret = () => readSecret(revealPath)
    }
  }
  function editKey(row?: {
    id: number
    name: string
    scopes: { project: string; permissions: string[] }[]
  }) {
    form.show(
      page.msg(row ? 'common.edit' : 'common.create'),
      [field('name', 'dialog.keyName')],
      { name: row?.name ?? '' },
      async () => {
        const scopes = (form.scopes ?? []).map(({ project, permissions }) => ({
          project,
          permissions: [...permissions],
        }))
        if (scopes.some((scope) => !scope.project || !scope.permissions.length))
          throw new Error(page.msg('keys.scopeRequired'), { cause: 422 })
        if (new Set(scopes.map((scope) => scope.project)).size !== scopes.length)
          throw new Error(page.msg('keys.duplicateProject'), { cause: 422 })
        const url = row ? `/api/v1/keys/${row.id}` : '/api/v1/keys'
        const result = await mutate(url, row ? 'PATCH' : 'POST', { name: form.values.name, scopes })
        if (result?.token) form.token = result.token
      },
    )
    form.scopeProjects = [...state.scopeProjects]
    form.scopePermissions = [...state.scopePermissions]
    form.globalPermissions = [...state.globalPermissions]
    form.scopes = (row?.scopes ?? []).map((scope) => ({
      project: scope.project,
      id: form.nextScopeId++,
      permissions: [...scope.permissions],
    }))
    if (!row) {
      form.addScope()
      if (data.project && form.scopes[0]) form.scopes[0].project = data.project.slug
    }
  }
  function removal(row: any) {
    const id =
      data.page === 'users'
        ? 'deleteUser'
        : data.page === 'projects'
          ? 'deleteProject'
          : state.tab === 'keys'
            ? 'revokeKey'
            : 'deleteItem'
    const url = path() + '/' + enc(row.slug ?? row.key ?? String(row.id))
    return dangerAction(
      form,
      id,
      page.msg(dangerOperations[id]),
      row.name ?? row.key ?? row.slug,
      async () => {
        await mutate(url, 'DELETE')
      },
    )
  }
  function proposal(row: any, approve: boolean) {
    const id = approve ? 'approveProposal' : 'rejectProposal'
    return dangerAction(form, id, page.msg(dangerOperations[id]), row.key, async () => {
      await mutate(`${base}/proposals/${row.id}/review`, 'POST', { approve })
    })
  }
  function remove(row: any) {
    removal(row).request()
  }
  function review(row: any, approve: boolean) {
    proposal(row, approve).request()
  }
  async function toggleView() {
    state.view = state.view === 'all' ? 'mine' : 'all'
    await refresh()
  }
  function rotation(row: any) {
    return dangerAction(form, 'rotateKey', page.msg('projects.rotate'), row.name, async () => {
      const result = await mutate(path() + '/' + row.id + '/rotate', 'POST')
      form.token = result.token
    })
  }
  function actionsFor(row: any) {
    if (state.tab === 'environments') return environmentActions(row as Environment)
    if (state.tab === 'proposals') return [proposal(row, true), proposal(row, false)]
    if (state.tab === 'keys') return [rotation(row), removal(row)]
    return [removal(row)]
  }
  function environmentActions(row: Environment) {
    const url = `${base}/environments/${enc(row.slug)}`
    return [
      dangerAction(
        form,
        'renameEnvironment',
        page.msg('environments.rename'),
        row.slug,
        async () => {
          const result = (await api(url, {
            method: 'PATCH',
            body: form.values,
            csrf: page.csrf,
          })) as Environment
          if (state.env === row.slug) state.env = result.slug
          notify('success', page.msg('toast.saved'))
          await refresh()
        },
        {
          fields: [field('slug', 'common.slug'), field('name', 'common.name')],
          values: { slug: row.slug, name: row.name },
          description: page.msg('environments.renameWarning'),
        },
      ),
      dangerAction(
        form,
        'deleteEnvironment',
        page.msg('common.delete'),
        row.slug,
        async () => {
          await mutate(url, 'DELETE')
        },
        { description: page.msg('environments.deleteWarning') },
      ),
    ]
  }
  function setPrimaryEnvironment(row: Environment) {
    form.show(`${page.msg('environments.setPrimary')}: ${row.slug}`, [], {}, async () => {
      await mutate(`${base}/environments/${enc(row.slug)}/primary`, 'POST')
    })
    form.description = page.msg('environments.primaryHelp')
  }
  /** @returns {void} */
  function environment(): void {
    form.show(
      page.msg('projects.environment'),
      [
        {
          key: 'env',
          label: 'projects.environment',
          options: state.environments.map((e) => ({
            value: e.slug,
            label: `${e.name} (${e.slug})${e.is_primary ? ` — ${page.msg('environments.primary')}` : ''}`,
          })),
        },
      ],
      { env: state.env },
      async () => {
        state.env = form.values.env
        state.rows = []
        await load()
      },
    )
  }
  /** @returns {void} */
  function newEnvironment(): void {
    form.show(
      page.msg('common.newEnvironment'),
      [field('slug', 'common.slug'), field('name', 'common.name')],
      { slug: '', name: '' },
      async () => {
        await mutate(`${base}/environments`, 'POST', form.values)
      },
    )
  }
  return {
    secrets,
    toggleView,
    actionsFor,
    state,
    form,
    refresh,
    select,
    edit,
    remove,
    review,
    environment,
    newEnvironment,
    setPrimaryEnvironment,
  }
}
