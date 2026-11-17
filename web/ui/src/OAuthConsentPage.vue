<script setup lang="ts">
import { ref } from 'vue'
import { api } from './core.ts'
import type { PageData } from './data.ts'
import type { PageContext } from './page.ts'

const props = defineProps<{ data: PageData; page: PageContext }>()
const { msg } = props.page
const busy = ref(false)

async function decide(approve: boolean) {
  if (busy.value || !props.data.consent) return
  busy.value = true
  try {
    const result = (await api('/oauth/consent', {
      method: 'POST',
      csrf: props.page.csrf,
      body: { request: props.data.consent.request, approve },
    })) as { redirect: string }
    location.assign(result.redirect)
  } catch (err) {
    props.page.report(err)
    busy.value = false
  }
}
</script>

<template>
  <section v-if="data.consent" class="card mt-4 max-w-2xl space-y-4">
    <h2 class="text-base font-semibold">
      {{ msg('oauth.client', { ClientName: data.consent.clientName }) }}
    </h2>
    <p>{{ msg('oauth.permissions') }}</p>
    <dl class="space-y-3 text-sm">
      <div>
        <dt class="font-medium">{{ msg('oauth.account') }}</dt>
        <dd>{{ data.me?.email }}</dd>
      </div>
      <div>
        <dt class="font-medium">{{ msg('oauth.resource') }}</dt>
        <dd class="break-all">{{ data.consent.resource }}</dd>
      </div>
      <div>
        <dt class="font-medium">{{ msg('oauth.redirect') }}</dt>
        <dd class="break-all">{{ data.consent.redirectUri }}</dd>
      </div>
    </dl>
    <div class="flex flex-wrap gap-2">
      <button class="btn" type="button" :disabled="busy" @click="decide(true)">
        {{ msg('oauth.allow') }}
      </button>
      <button class="btn-outline" type="button" :disabled="busy" @click="decide(false)">
        {{ msg('oauth.deny') }}
      </button>
    </div>
  </section>
</template>
