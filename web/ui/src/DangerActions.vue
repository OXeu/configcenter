<script setup lang="ts">
import Button from '@oxeu/center-common/ui/Button.vue'
import DropdownMenu from '@oxeu/center-common/ui/DropdownMenu.vue'
import type { DangerAction } from './danger.ts'
defineProps<{ actions: DangerAction[]; label: string; disabled: boolean }>()
</script>
<template>
  <DropdownMenu v-if="actions.length" :label="label" :disabled="disabled">
    <Button
      v-for="action in actions"
      :key="action.id"
      type="button"
      :disabled="disabled"
      :data-danger-action="action.id"
      @click="action.request()"
      variant="danger"
      block
      align="start"
    >
      {{ action.label }}
    </Button>
  </DropdownMenu>
</template>
