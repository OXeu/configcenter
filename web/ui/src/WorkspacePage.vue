<script setup>
import { computed, defineAsyncComponent, onMounted, onUnmounted } from 'vue'
import DangerActions from './DangerActions.vue'
const DialogPanel = defineAsyncComponent(() => import('./DialogPanel.vue'))
import { createWorkspace } from './workspace.ts'

const props = defineProps({
  data: { type: Object, required: true },
  page: { type: Object, required: true },
  title: { type: String, required: true },
})
const data = /** @type {import('./data.ts').PageData} */ (props.data)
const page = /** @type {import('./page.ts').PageContext} */ (props.page)
const { msg } = page
const {
  state,
  form,
  secrets,
  refresh,
  select,
  edit,
  toggleView,
  actionsFor,
  environment,
  newEnvironment,
  setPrimaryEnvironment,
} = createWorkspace(data, page)
/** @param {any} row @param {string} column */
function isSecret(row, column) {
  return column === 'value' && row.secret && row.value !== null
}
/** @param {any} row */
function isRevealed(row) {
  return secrets.values.has(row)
}
/** @param {any} row @param {string} column */
function primaryBadge(row, column) {
  return managingEnvironments.value && column === 'name' && row.is_primary
}
/** @param {any} row */
function canSetPrimary(row) {
  return managingEnvironments.value && !row.is_primary
}
/** @param {any} row */
function secretLabel(row) {
  return msg(isRevealed(row) ? 'secret.hideValue' : 'secret.showValue', { key: row.key })
}
const canCreate = computed(
  () =>
    data.canWrite &&
    data.page !== 'users' &&
    data.page !== 'settings' &&
    !['proposals', 'environments'].includes(state.tab),
)
const showViewSwitch = computed(() => data.isAdmin && data.page === 'projects')
const viewLabel = computed(() => msg(state.view === 'all' ? 'projects.mine' : 'projects.all'))
const reviewing = computed(() => data.page === 'project' && state.tab === 'proposals')
const managingEnvironments = computed(() => data.page === 'project' && state.tab === 'environments')
const canEditRow = computed(() => !reviewing.value && !managingEnvironments.value)
const tabs = ['items', 'environments', 'keys', 'proposals']
const columns = computed(() => {
  if (data.page === 'settings') return ['key', 'value']
  if (data.page === 'projects') return ['name', 'slug', 'description']
  if (data.page === 'users') return ['name', 'email', 'role', 'last_login_at']
  if (state.tab === 'environments') return ['name', 'slug']
  if (state.tab === 'keys') return ['name', 'prefix', 'scopes', 'last_used_at']
  if (state.tab === 'proposals') return ['environment', 'key', 'action', 'value', 'reason']
  return ['key', 'value', 'description', 'updated_at']
})
/** @type {Record<string, string>} */
const labels = {
  name: 'common.name',
  slug: 'common.slug',
  description: 'common.description',
  email: 'users.email',
  role: 'users.role',
  last_login_at: 'users.lastLogin',
  prefix: 'projects.prefix',
  scopes: 'keys.scopes',
  last_used_at: 'projects.lastUsed',
  environment: 'projects.environment',
  key: 'projects.key',
  action: 'projects.proposalAction',
  value: 'projects.value',
  reason: 'projects.proposalReason',
  updated_at: 'projects.updated',
}
/** @param {any} row @param {string} column */
function cell(row, column) {
  if (isSecret(row, column)) return secrets.values.get(row) ?? row.value
  if (data.page === 'settings')
    return msg(
      column === 'key'
        ? 'settings.registration'
        : row.value
          ? 'settings.allowed'
          : 'settings.closed',
    )
  if (column === 'scopes') return scopeText(row.scopes)
  if (column === 'role') return msg(`role.${row[column]}`)
  if (column.endsWith('_at'))
    return row[column] ? new Date(row[column]).toLocaleString(data.lang) : ''
  return row[column] ?? ''
}
/** @param {{ project: string; permissions: string[] }[]} scopes */
function scopeText(scopes) {
  if (!scopes.length) return msg('keys.noScopes')
  return scopes
    .map((scope) => {
      const project = scope.project === '*' ? msg('keys.allProjects') : scope.project
      return `${project}: ${scope.permissions.map((permission) => msg(`scope.${permission}`)).join(', ')}`
    })
    .join('\n')
}
onMounted(refresh)
onUnmounted(() => {
  secrets.reset()
  form.open = false
  form.values = {}
  form.readSecret = null
})
</script>
<template>
  <p v-if="data.page === 'keys'" class="mt-2 text-sm text-muted-foreground">
    {{ msg('keys.description') }}
  </p>
  <div class="flex flex-wrap gap-2 my-4">
    <template v-if="data.page === 'project'">
      <button
        v-for="tab in tabs"
        :key="tab"
        class="tabs-trigger"
        :data-active="state.tab === tab"
        :disabled="state.busy"
        @click="select(tab)"
      >
        {{ msg(`projects.${tab}`) }}
      </button>
      <button class="btn-outline" :disabled="state.busy" @click="environment">
        {{ msg('projects.environment') }}: {{ state.env }}
      </button>
      <button
        v-if="data.canWrite"
        class="btn-outline"
        :disabled="state.busy"
        @click="newEnvironment"
      >
        {{ msg('common.newEnvironment') }}
      </button>
    </template>
    <button v-if="showViewSwitch" class="btn-outline" :disabled="state.busy" @click="toggleView">
      {{ viewLabel }}
    </button>
    <button class="btn-outline" :disabled="state.busy" @click="refresh">
      {{ msg('common.refresh') }}
    </button>
    <button v-if="canCreate" class="btn" :disabled="state.busy" @click="edit()">
      {{ msg('common.create') }}
    </button>
  </div>
  <div class="table-scroll">
    <table class="table" :aria-label="title" :aria-busy="state.busy">
      <thead>
        <tr>
          <th v-for="column in columns" :key="column" scope="col">
            {{ msg(labels[column]) }}
          </th>
          <th v-if="data.canWrite || data.isAdmin" scope="col">
            {{ msg('common.actions') }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in state.rows" :key="row.id ?? row.slug ?? row.key">
          <td v-for="column in columns" :key="column" :data-label="msg(labels[column])">
            <a
              v-if="data.page === 'projects' && column === 'name'"
              :href="`/projects/${encodeURIComponent(row.slug)}`"
              >{{ row.name }}</a
            >
            <span v-else class="whitespace-pre-wrap break-all">{{ cell(row, column) }}</span>
            <span v-if="primaryBadge(row, column)" class="badge ml-2">
              {{ msg('environments.primary') }}
            </span>
            <button
              v-if="isSecret(row, column)"
              type="button"
              class="btn-ghost"
              :aria-label="secretLabel(row)"
              :aria-pressed="isRevealed(row)"
              :disabled="secrets.pending.has(row)"
              @click="secrets.toggle(row)"
            >
              {{ msg(isRevealed(row) ? 'secret.hide' : 'secret.show') }}
            </button>
          </td>
          <td v-if="data.canWrite || data.isAdmin" :data-label="msg('common.actions')">
            <button
              v-if="canSetPrimary(row)"
              class="btn-ghost"
              :disabled="state.busy"
              @click="setPrimaryEnvironment(row)"
            >
              {{ msg('environments.setPrimary') }}
            </button>
            <button v-if="canEditRow" class="btn-ghost" :disabled="state.busy" @click="edit(row)">
              {{ msg('common.edit') }}
            </button>
            <DangerActions
              v-if="data.page !== 'settings'"
              :actions="actionsFor(row)"
              :label="msg('common.more')"
              :disabled="state.busy"
            />
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <DialogPanel v-if="form.open" :form="form" :msg="msg" />
</template>
