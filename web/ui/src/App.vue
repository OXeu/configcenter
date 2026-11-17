<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, watch } from 'vue'
import PageHeader from './PageHeader.vue'
import { createPage } from './page.ts'
import { useTheme } from './theme.ts'
import { mountToasts, notify, setToastTheme } from './toast.ts'

const WorkspacePage = defineAsyncComponent(() => import('./WorkspacePage.vue'))
const OAuthConsentPage = defineAsyncComponent(() => import('./OAuthConsentPage.vue'))

const props = defineProps({ data: { type: Object, required: true } })
const data = props.data as import('./data.ts').PageData
const { theme, toggleTheme } = useTheme()
watch(theme, (value) => setToastTheme(value))
const isDocs = computed(() => data.page === 'docs')
const isWorkspace = computed(() =>
  ['projects', 'project', 'users', 'settings', 'keys'].includes(data.page),
)
const docsLinks = [
  { page: 'index', href: '/docs/', label: 'docs.overview' },
  { page: 'api', href: '/docs/api', label: 'docs.api' },
  { page: 'sdk', href: '/docs/sdk', label: 'docs.sdk' },
  { page: 'mcp', href: '/docs/mcp', label: 'docs.mcp' },
]
const rawDocument = computed(() => `/docs/${data.docPage ?? 'index'}.md`)
const page = createPage(data)
const { msg } = page
const title = computed(
  () => data.project?.name ?? msg(data.page === 'login' ? 'login.title' : data.title),
)
onMounted(() => {
  mountToasts(data.lang, data.nonce, theme.value)
  if (data.flash) notify(data.flash.level, data.flash.message)
  if (data.page === 'login' && !data.oauthEnabled) notify('info', msg('login.notConfigured'))
})
</script>
<template>
  <div
    :data-theme="theme"
    class="bg-background text-foreground"
    :class="{ 'min-h-screen': !isDocs }"
  >
    <PageHeader
      :data="data"
      :msg="msg"
      :title="title"
      :theme="theme"
      :header-only="isDocs"
      subtitle=""
      @toggle-theme="toggleTheme"
    >
      <a v-if="data.page === 'login' && data.oauthEnabled" class="btn mt-4" href="/auth/login">{{
        msg('login.continue', { ProviderName: data.providerName })
      }}</a>
      <WorkspacePage v-if="isWorkspace" :data="data" :page="page" :title="title" />
      <OAuthConsentPage v-if="data.page === 'oauth-consent'" :data="data" :page="page" />
    </PageHeader>
    <nav
      v-if="isDocs"
      class="mx-auto flex max-w-6xl flex-wrap gap-2 px-4 py-4 sm:px-6 sm:py-6"
      :aria-label="msg('docs.navigation')"
    >
      <a
        v-for="link in docsLinks"
        :key="link.page"
        :href="link.href"
        class="tabs-trigger inline-flex items-center"
        :data-active="data.docPage === link.page"
        :aria-current="data.docPage === link.page ? 'page' : undefined"
        >{{ msg(link.label) }}</a
      >
      <a class="btn-ghost ml-auto" :href="rawDocument">{{ msg('docs.raw') }}</a>
    </nav>
  </div>
</template>
