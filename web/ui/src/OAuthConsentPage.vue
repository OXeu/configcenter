<script setup lang="ts">
import ConsentCard from '@oxeu/center-common/ui/ConsentCard.vue'
import { navigateDocument } from '@oxeu/center-common/ui/navigation.ts'
import { computed, ref } from 'vue'
import { api } from './core.ts'
import type { PageData } from './data.ts'
import type { PageContext } from './page.ts'

const props = defineProps<{ data: PageData; page: PageContext }>()
const { msg } = props.page
const busy = ref(false)
const clientTitle = computed(() =>
  props.data.consent ? msg('oauth.client', { ClientName: props.data.consent.clientName }) : '',
)
const details = computed(() => {
  const consent = props.data.consent
  if (!consent) return []
  return [
    { label: msg('oauth.account'), value: props.data.me?.email ?? '' },
    { label: msg('oauth.resource'), value: consent.resource },
    { label: msg('oauth.redirect'), value: consent.redirectUri },
  ]
})

async function decide(approve: boolean) {
  if (busy.value || !props.data.consent) return
  busy.value = true
  try {
    const result = (await api('/oauth/consent', {
      method: 'POST',
      csrf: props.page.csrf,
      body: { request: props.data.consent.request, approve },
    })) as { redirect: string }
    navigateDocument(result.redirect, 'oauth-consent')
  } catch (err) {
    props.page.report(err)
    busy.value = false
  }
}
</script>

<template>
  <ConsentCard
    v-if="data.consent"
    :title="clientTitle"
    :description="msg('oauth.permissions')"
    :details="details"
    :allow-label="msg('oauth.allow')"
    :deny-label="msg('oauth.deny')"
    :busy="busy"
    @allow="decide(true)"
    @deny="decide(false)"
  />
</template>
