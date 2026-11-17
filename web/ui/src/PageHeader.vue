<script setup>
import DropdownMenu from '@oxeu/center-common/ui/DropdownMenu.vue'
import DangerActions from './DangerActions.vue'
import { computed, defineAsyncComponent, nextTick, ref, watch } from 'vue'
import { api } from './core.ts'
import { createPage } from './page.ts'
import { createForm } from './forms.ts'
import { dangerAction } from './danger.ts'

const DialogPanel = defineAsyncComponent(() => import('./DialogPanel.vue'))

/**
 * @typedef {import('./data.ts').PageData} PageData
 */

const props = defineProps({
  data: { type: Object, required: true },
  msg: { type: Function, required: true },
  title: { type: String, required: true },
  subtitle: { type: String, required: true },
  theme: { type: String, required: true },
  headerOnly: { type: Boolean, default: false },
})

const emit = defineEmits(['toggle-theme'])
const brandIcon = {
  src: '/static/brand/icon-32.webp',
  srcset: '/static/brand/icon-64.webp 2x, /static/brand/icon-128.webp 4x',
}
const themeLabel = computed(() => msg(props.theme === 'dark' ? 'theme.toLight' : 'theme.toDark'))
const themeIcon = computed(() => (props.theme === 'dark' ? 'ri-moon-line' : 'ri-sun-line'))

const data = /** @type {PageData} */ (props.data)
const msg = /** @type {(id: string, vars?: Record<string, string>) => string} */ (props.msg)
const avatarFailed = ref(false)
const avatarSource = computed(() => data.me?.avatar_url ?? '')
const showAvatar = computed(() => Boolean(avatarSource.value) && !avatarFailed.value)
watch(avatarSource, () => {
  avatarFailed.value = false
})
const roleIcon = computed(() => {
  if (data.me?.role === 'admin') return 'ri-vip-crown-line'
  return 'ri-eye-line'
})
const avatarInitial = computed(() => {
  const name = data.me?.name.trim() || data.me?.email || ''
  return Array.from(name)[0]?.toLocaleUpperCase() ?? ''
})
const identityLabel = computed(() => `${data.me?.name ?? ''} ${roleLabel.value}`)
const roleLabel = computed(() => (data.me === null ? '' : msg(`role.${data.me.role}`)))
const form = createForm(createPage(data))
const userMenu = ref(/** @type {InstanceType<typeof DropdownMenu> | null} */ (null))
const accountActions = computed(() => [
  dangerAction(form, 'signOut', msg('nav.signOut'), data.me?.name ?? '', logout),
])
watch(
  () => form.open,
  async (open) => {
    if (!open) {
      await nextTick()
      userMenu.value?.focus()
    }
  },
)
/** @returns {Promise<void>} */
async function logout() {
  await api('/logout', { method: 'POST', csrf: data.csrf })
  location.assign('/login')
}
</script>

<template>
  <a class="sr-only" href="#main">{{ msg('nav.skip') }}</a>
  <header class="flex flex-wrap items-center gap-2 border-b px-4 py-3 sm:gap-4 sm:px-6">
    <a class="btn-ghost btn-icon mr-auto sm:mr-0" href="/projects" :aria-label="msg('app.name')">
      <img :src="brandIcon.src" :srcset="brandIcon.srcset" width="32" height="32" alt="" />
    </a>
    <nav
      class="order-last flex w-full flex-wrap gap-1 sm:order-none sm:w-auto sm:flex-1"
      :aria-label="msg('nav.main')"
    >
      <a v-if="data.loggedIn" class="btn-ghost" href="/projects">{{ msg('nav.projects') }}</a>
      <a v-if="data.isAdmin" class="btn-ghost" href="/settings">{{ msg('title.settings') }}</a>
      <a class="btn-ghost" href="/docs/">{{ msg('nav.docs') }}</a>
    </nav>
    <button
      class="btn-ghost btn-icon"
      type="button"
      :aria-label="themeLabel"
      @click="emit('toggle-theme')"
    >
      <i :class="themeIcon" aria-hidden="true"></i>
    </button>
    <DropdownMenu :label="data.languageLabel" compact>
      <template #trigger><i class="ri-global-line" aria-hidden="true"></i></template>
      <a
        v-for="language in data.languages"
        :key="language.tag"
        class="block rounded-sm px-2 py-1 text-sm hover:bg-muted"
        :href="`/language?lang=${language.tag}`"
        :aria-current="language.tag === data.lang ? 'true' : undefined"
        >{{ language.label }}</a
      >
    </DropdownMenu>
    <template v-if="data.loggedIn && data.me">
      <DropdownMenu ref="userMenu" :label="identityLabel" compact>
        <template #trigger>
          <div class="user-avatar" role="img" :aria-label="identityLabel">
            <img
              v-if="showAvatar"
              :src="avatarSource"
              class="avatar-image"
              alt=""
              referrerpolicy="no-referrer"
              @error="avatarFailed = true"
            />
            <span v-else-if="avatarInitial" aria-hidden="true">{{ avatarInitial }}</span>
            <i v-else class="ri-user-line" aria-hidden="true"></i>
          </div>
        </template>
        <div class="w-56 max-w-full">
          <div class="flex flex-col items-center gap-2 p-3 text-center">
            <div class="user-avatar h-10 w-10" role="img" :aria-label="identityLabel">
              <img
                v-if="showAvatar"
                :src="avatarSource"
                class="avatar-image"
                alt=""
                referrerpolicy="no-referrer"
                @error="avatarFailed = true"
              />
              <span v-else-if="avatarInitial" aria-hidden="true">{{ avatarInitial }}</span>
              <i v-else class="ri-user-line" aria-hidden="true"></i>
            </div>
            <p class="w-full break-words text-sm font-semibold">{{ data.me.name }}</p>
            <div class="flex items-center justify-center gap-2">
              <span class="badge role-badge" :data-role="data.me.role" aria-hidden="true">
                <i :class="roleIcon" aria-hidden="true"></i>
              </span>
              <p class="text-sm text-muted-foreground">{{ roleLabel }}</p>
            </div>
          </div>
          <div class="border-t border-gray-200 pt-1">
            <a
              class="btn-ghost w-full justify-start gap-2"
              href="/keys"
              :aria-label="msg('title.keys')"
            >
              <i class="ri-key-2-line" aria-hidden="true"></i>
              <span>{{ msg('nav.keys') }}</span>
            </a>
            <a v-if="data.isAdmin" class="btn-ghost w-full justify-start gap-2" href="/users">
              <i class="ri-group-line" aria-hidden="true"></i>
              <span>{{ msg('nav.users') }}</span>
            </a>
          </div>
          <div class="mt-1 border-t pt-1">
            <DangerActions
              :actions="accountActions"
              :label="msg('common.more')"
              :disabled="form.busy"
            />
          </div>
        </div>
      </DropdownMenu>
    </template>
  </header>
  <DialogPanel v-if="form.open" :form="form" :msg="msg" />
  <main v-if="!headerOnly" id="main" class="mx-auto max-w-6xl px-4 py-6 sm:px-6 sm:py-8">
    <h1 class="break-words text-xl font-semibold">{{ title }}</h1>
    <p v-if="subtitle" class="text-sm text-muted-foreground">{{ subtitle }}</p>
    <slot />
  </main>
</template>
