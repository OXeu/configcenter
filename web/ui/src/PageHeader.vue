<script setup lang="ts">
import Button from '@oxeu/center-common/ui/Button.vue'
import NavigationLink from '@oxeu/center-common/ui/NavigationLink.vue'
import DropdownMenu from '@oxeu/center-common/ui/DropdownMenu.vue'
import DangerActions from './DangerActions.vue'
import { computed, defineAsyncComponent, nextTick, ref, watch } from 'vue'
import { api } from './core.ts'
import { createPage } from './page.ts'
import { createForm } from './forms.ts'
import { dangerAction } from './danger.ts'

const DialogPanel = defineAsyncComponent(() => import('./DialogPanel.vue'))

import type { PageData } from './data.ts'

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

const data = props.data as PageData
const msg = props.msg as (id: string, vars?: Record<string, string>) => string
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
const userMenu = ref<InstanceType<typeof DropdownMenu> | null>(null)
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
async function logout(): Promise<void> {
  await api('/logout', { method: 'POST', csrf: data.csrf })
  location.assign('/login')
}
</script>

<template>
  <NavigationLink class="sr-only" href="#main">{{ msg('nav.skip') }}</NavigationLink>
  <header class="flex flex-wrap items-center gap-2 border-b px-4 py-3 sm:gap-4 sm:px-6">
    <Button
      class="mr-auto sm:mr-0"
      href="/projects"
      :aria-label="msg('app.name')"
      variant="ghost"
      content="icon"
    >
      <template #icon
        ><img :src="brandIcon.src" :srcset="brandIcon.srcset" width="32" height="32" alt=""
      /></template>
    </Button>
    <nav
      class="order-last flex w-full flex-wrap gap-1 sm:order-none sm:w-auto sm:flex-1"
      :aria-label="msg('nav.main')"
    >
      <Button v-if="data.loggedIn" href="/projects" variant="ghost">{{
        msg('nav.projects')
      }}</Button>
      <Button v-if="data.isAdmin" href="/settings" variant="ghost">{{
        msg('title.settings')
      }}</Button>
      <Button href="/docs/" variant="ghost">{{ msg('nav.docs') }}</Button>
    </nav>
    <Button
      type="button"
      :aria-label="themeLabel"
      @click="emit('toggle-theme')"
      variant="ghost"
      content="icon"
      :icon="themeIcon"
    >
    </Button>
    <DropdownMenu :label="data.languageLabel" compact>
      <template #trigger><i class="ri-global-line" aria-hidden="true"></i></template>
      <Button
        v-for="language in data.languages"
        :key="language.tag"
        :href="`/language?lang=${language.tag}`"
        :aria-current="language.tag === data.lang ? 'true' : undefined"
        variant="ghost"
        block
        align="start"
        >{{ language.label }}</Button
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
            <Button
              href="/keys"
              :aria-label="msg('title.keys')"
              variant="ghost"
              block
              align="start"
              content="icon-text"
              icon="ri-key-2-line"
            >
              <span>{{ msg('nav.keys') }}</span>
            </Button>
            <Button
              v-if="data.isAdmin"
              href="/users"
              variant="ghost"
              block
              align="start"
              content="icon-text"
              icon="ri-group-line"
            >
              <span>{{ msg('nav.users') }}</span>
            </Button>
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
