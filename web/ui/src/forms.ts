import { reactive, watch } from 'vue'

export type ScopeDraft = { id: number; project: string; permissions: string[] }
export type Choice = { value: string; label: string }

/** @param {import('./page.ts').PageContext} page */
export function createForm(page: import('./page.ts').PageContext) {
  const form = reactive({
    open: false,
    danger: false,
    title: '',
    description: '',
    busy: false,
    error: '',
    token: '',
    secret: null as boolean | null,
    valueRevealed: false,
    keepSecret: false,
    secretMask: '',
    revealing: false,
    readSecret: null as (() => Promise<string>) | null,
    scopes: null as ScopeDraft[] | null,
    scopeProjects: [] as Choice[],
    scopePermissions: [] as Choice[],
    globalPermissions: [] as string[],
    nextScopeId: 0,
    fields: /** @type {any[]} */ ([]) as any[],
    values: /** @type {Record<string, string>} */ ({}) as Record<string, string>,
    action: /** @type {() => Promise<void>} */ (async () => {}) as () => Promise<void>,
    /** @param {string} title @param {any[]} fields @param {Record<string,string>} values @param {() => Promise<void>} action */
    show(
      title: string,
      fields: any[],
      values: Record<string, string>,
      action: () => Promise<void>,
    ) {
      this.danger = false
      this.description = ''
      this.title = title
      this.fields = fields
      this.values = { ...values }
      this.action = action
      this.error = ''
      this.token = ''
      this.secret = null
      this.valueRevealed = false
      this.keepSecret = false
      this.secretMask = ''
      this.revealing = false
      this.readSecret = null
      this.scopes = null
      this.open = true
    },
    async toggleValue() {
      if (this.busy || this.revealing) return
      if (this.valueRevealed) {
        this.valueRevealed = false
        if (this.keepSecret) this.values.value = ''
        return
      }
      const draft = this.values
      this.revealing = true
      try {
        const value = this.keepSecret && this.readSecret ? await this.readSecret() : draft.value
        if (this.open && draft === this.values) {
          this.values.value = value
          this.valueRevealed = true
        }
      } catch (error) {
        if (this.open && draft === this.values) page.report(error)
      } finally {
        if (draft === this.values) this.revealing = false
      }
    },
    replaceValue() {
      this.keepSecret = false
      this.secretMask = ''
      this.values.value = ''
      this.valueRevealed = true
    },
    touch(key: string) {
      if (key === 'value') {
        this.keepSecret = false
        this.secretMask = ''
      }
    },
    addScope() {
      if (this.busy || this.scopes === null) return
      const project = this.scopeProjects.find(
        (option) => !this.scopes?.some((scope) => scope.project === option.value),
      )
      if (project)
        this.scopes.push({ id: this.nextScopeId++, project: project.value, permissions: [] })
    },
    permissionsFor(scope: ScopeDraft) {
      return this.scopePermissions.filter(
        (permission) => scope.project === '*' || !this.globalPermissions.includes(permission.value),
      )
    },
    limitScope(scope: ScopeDraft) {
      const allowed = new Set(this.permissionsFor(scope).map((permission) => permission.value))
      scope.permissions = scope.permissions.filter((permission) => allowed.has(permission))
    },
    excludeScope(id: number) {
      if (this.busy || this.scopes === null) return
      this.scopes = this.scopes.filter((scope) => scope.id !== id)
    },
    async submit() {
      if (this.busy || this.revealing) return
      this.busy = true
      this.error = ''
      try {
        await this.action()
        if (!this.token) this.open = false
      } catch (err) {
        this.error = page.describe(err)
      } finally {
        this.busy = false
      }
    },
  })
  watch(
    () => form.open,
    (open) => {
      if (!open && form.secret !== null) {
        form.values = {}
        form.readSecret = null
        form.valueRevealed = false
      }
    },
    { flush: 'sync' },
  )
  return form
}
