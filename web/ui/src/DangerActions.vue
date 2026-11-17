<script setup lang="ts">
import DropdownMenu from '@oxeu/center-common/ui/DropdownMenu.vue'
import type { DangerAction } from './danger.ts'
defineProps<{ actions: DangerAction[]; label: string; disabled: boolean }>()
</script>
<template>
  <DropdownMenu v-if="actions.length" :label="label" :disabled="disabled">
    <button
      v-for="action in actions"
      :key="action.id"
      type="button"
      class="btn-danger"
      :disabled="disabled"
      :data-danger-action="action.id"
      @click="action.request()"
    >
      {{ action.label }}
    </button>
  </DropdownMenu>
</template>
