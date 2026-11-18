<script setup lang="ts">
import Button from '@oxeu/center-common/ui/Button.vue'
import DropdownMenu from '@oxeu/center-common/ui/DropdownMenu.vue'
import NavigationLink from '@oxeu/center-common/ui/NavigationLink.vue'
import { computed, defineAsyncComponent, onMounted, onUnmounted } from 'vue'
import DangerActions from './DangerActions.vue'
const DialogPanel = defineAsyncComponent(() => import('./DialogPanel.vue'))
import { createWorkspace } from './workspace.ts'

const props = defineProps({
  data: { type: Object, required: true },
  page: { type: Object, required: true },
  title: { type: String, required: true },
})
const data = props.data as import('./data.ts').PageData
const page = props.page as import('./page.ts').PageContext
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
  editChoices,
  choose,
  newEnvironment,
  setPrimaryEnvironment,
} = createWorkspace(data, page)
function isSecret(row: any, column: string) {
  return column === 'value' && row.secret && row.value !== null
}
function isRevealed(row: any) {
  return secrets.values.has(row)
}
function primaryBadge(row: any, column: string) {
  return managingEnvironments.value && column === 'name' && row.is_primary
}
function canSetPrimary(row: any) {
  return managingEnvironments.value && !row.is_primary
}
function secretLabel(row: any) {
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
const choiceEditor = computed(() => ['settings', 'users'].includes(data.page))
const environmentLabel = computed(() => `${msg('projects.environment')}: ${state.env}`)
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
const labels: Record<string, string> = {
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
function cell(row: any, column: string) {
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
function scopeText(scopes: { project: string; permissions: string[] }[]) {
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
      <Button
        v-for="tab in tabs"
        :key="tab"
        :data-active="state.tab === tab"
        :disabled="state.busy"
        @click="select(tab)"
        variant="ghost"
        size="sm"
      >
        {{ msg(`projects.${tab}`) }}
      </Button>
      <DropdownMenu :disabled="state.busy" :label="environmentLabel">
        <Button
          v-for="env in state.environments"
          :key="env.slug"
          :aria-pressed="state.env === env.slug"
          :data-active="state.env === env.slug"
          :disabled="state.busy"
          @click="environment(env.slug)"
          variant="ghost"
          block
          align="start"
        >
          {{ env.name }} ({{ env.slug }})
          <span v-if="env.is_primary"> — {{ msg('environments.primary') }}</span>
        </Button>
      </DropdownMenu>
      <Button
        v-if="data.canWrite"
        :disabled="state.busy"
        @click="newEnvironment"
        variant="secondary"
      >
        {{ msg('common.newEnvironment') }}
      </Button>
    </template>
    <Button v-if="showViewSwitch" :disabled="state.busy" @click="toggleView" variant="secondary">
      {{ viewLabel }}
    </Button>
    <Button :disabled="state.busy" @click="refresh" variant="secondary">
      {{ msg('common.refresh') }}
    </Button>
    <Button v-if="canCreate" :disabled="state.busy" @click="edit()" variant="primary">
      {{ msg('common.create') }}
    </Button>
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
            <NavigationLink
              v-if="data.page === 'projects' && column === 'name'"
              :href="`/projects/${encodeURIComponent(row.slug)}`"
              >{{ row.name }}</NavigationLink
            >
            <span v-else class="whitespace-pre-wrap break-all">{{ cell(row, column) }}</span>
            <span v-if="primaryBadge(row, column)" class="badge ml-2">
              {{ msg('environments.primary') }}
            </span>
            <Button
              v-if="isSecret(row, column)"
              type="button"
              :aria-label="secretLabel(row)"
              :aria-pressed="isRevealed(row)"
              :disabled="secrets.pending.has(row)"
              @click="secrets.toggle(row)"
              variant="ghost"
            >
              {{ msg(isRevealed(row) ? 'secret.hide' : 'secret.show') }}
            </Button>
          </td>
          <td v-if="data.canWrite || data.isAdmin" :data-label="msg('common.actions')">
            <Button
              v-if="canSetPrimary(row)"
              :disabled="state.busy"
              @click="setPrimaryEnvironment(row)"
              variant="ghost"
            >
              {{ msg('environments.setPrimary') }}
            </Button>
            <DropdownMenu v-if="choiceEditor" :disabled="state.busy" :label="msg('common.edit')">
              <Button
                v-for="choice in editChoices(row)"
                :key="choice.value"
                :aria-pressed="choice.selected"
                :data-active="choice.selected"
                :disabled="state.busy"
                @click="choose(row, choice.value)"
                variant="ghost"
                block
                align="start"
              >
                {{ choice.label }}
              </Button>
            </DropdownMenu>
            <Button
              v-else-if="canEditRow"
              :disabled="state.busy"
              @click="edit(row)"
              variant="ghost"
            >
              {{ msg('common.edit') }}
            </Button>
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
