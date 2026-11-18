<script setup lang="ts">
import Button from '@oxeu/center-common/ui/Button.vue'
import { computed, ref, watch, nextTick, useId } from 'vue'
import DropdownSelect from '@oxeu/center-common/ui/DropdownSelect.vue'
import CheckboxField from '@oxeu/center-common/ui/CheckboxField.vue'
import ConfigValueEditor from './ConfigValueEditor.vue'
import type { createForm } from './forms.ts'
import type { Copy } from './i18n.ts'

const formId = useId()
const props = defineProps<{ form: ReturnType<typeof createForm>; msg: Copy['msg'] }>()
const canAddScope = computed(() =>
  props.form.scopeProjects.some(
    (option: import('./forms.ts').Choice) =>
      !props.form.scopes?.some(
        (scope: import('./forms.ts').ScopeDraft) => scope.project === option.value,
      ),
  ),
)
const dialog = ref<HTMLDialogElement | null>(null)
watch(
  () => props.form.open,
  async (open) => {
    await nextTick()
    if (open) dialog.value?.showModal()
    else dialog.value?.close()
  },
  { immediate: true },
)
function cancel(event: Event) {
  if (props.form.busy) {
    event.preventDefault()
    return
  }
  props.form.open = false
}
function hidden(field: { sensitive?: boolean }) {
  return sensitive(field) && !props.form.valueRevealed
}
function sensitive(field: { sensitive?: boolean }) {
  return field.sensitive && (props.form.secret || props.form.keepSecret)
}
function setValue(key: string, value: string) {
  props.form.values[key] = value
  props.form.touch(key)
}
</script>
<template>
  <dialog ref="dialog" class="dialog-panel" :aria-label="form.title" @cancel="cancel">
    <form class="space-y-3" @submit.prevent="form.submit">
      <h2 class="text-base font-semibold">{{ form.title }}</h2>
      <p v-if="form.description" class="field">{{ form.description }}</p>
      <template v-if="!form.token">
        <CheckboxField
          v-if="form.secret !== null"
          v-model="form.secret"
          :label="msg('dialog.itemSecret')"
          :disabled="form.busy"
        />
        <div v-for="field in form.fields" :key="field.key" class="field">
          <span v-if="hidden(field)">{{ msg(field.label) }}</span>
          <label v-else :for="`${formId}-${field.key}`">{{ msg(field.label) }}</label>
          <span v-if="hidden(field)">{{ form.secretMask || msg('secret.mask') }}</span>
          <DropdownSelect
            v-else-if="field.options"
            :id="`${formId}-${field.key}`"
            v-model="form.values[field.key]"
            :label="msg(field.label)"
            :options="field.options"
            :required="field.required"
            :disabled="form.busy"
          />
          <ConfigValueEditor
            v-else-if="field.multiline"
            v-slot="{ syncScroll, setComposing }"
            :value="form.values[field.key] ?? ''"
            :msg="msg"
            :disabled="form.busy"
            @format="setValue(field.key, $event)"
          >
            <textarea
              :id="`${formId}-${field.key}`"
              v-model="form.values[field.key]"
              class="input config-value-input"
              wrap="off"
              spellcheck="false"
              autocapitalize="off"
              :required="field.required"
              :disabled="form.busy"
              @input="form.touch(field.key)"
              @scroll="syncScroll"
              @compositionstart="setComposing(true)"
              @compositionend="setComposing(false)"
            ></textarea>
          </ConfigValueEditor>
          <input
            v-else
            :id="`${formId}-${field.key}`"
            v-model="form.values[field.key]"
            class="input"
            :required="field.required"
            :disabled="form.busy"
          />
          <Button
            v-if="sensitive(field)"
            type="button"
            :disabled="form.busy || form.revealing"
            :aria-pressed="form.valueRevealed"
            @click="form.toggleValue()"
            variant="ghost"
          >
            {{ msg(form.valueRevealed ? 'secret.hide' : 'secret.show') }}
          </Button>
          <Button
            v-if="field.sensitive && form.keepSecret"
            type="button"
            :disabled="form.busy || form.revealing"
            @click="form.replaceValue()"
            variant="secondary"
          >
            {{ msg('secret.replace') }}
          </Button>
        </div>
        <section v-if="form.scopes !== null" class="space-y-3">
          <h3 class="text-sm font-medium">{{ msg('keys.scopes') }}</h3>
          <p class="field">{{ msg('keys.scopeHelp') }}</p>
          <fieldset
            v-for="scope in form.scopes"
            :key="scope.id"
            class="card space-y-3"
            :aria-label="msg('keys.scopeProject')"
          >
            <div class="flex items-center gap-2">
              <DropdownSelect
                :id="`${formId}-scope-${scope.id}`"
                v-model="scope.project"
                @update:model-value="form.limitScope(scope)"
                class="min-w-0 flex-1"
                :label="msg('keys.scopeProject')"
                :options="form.scopeProjects"
                :disabled="form.busy"
                required
              />
              <Button
                type="button"
                :aria-label="msg('keys.excludeScope')"
                :disabled="form.busy"
                @click="form.excludeScope(scope.id)"
                variant="secondary"
                content="icon"
                icon="ri-close-line"
              >
              </Button>
            </div>
            <div class="grid gap-1 sm:grid-cols-2">
              <CheckboxField
                v-for="permission in form.permissionsFor(scope)"
                :key="permission.value"
                v-model="scope.permissions"
                :label="permission.label"
                :value="permission.value"
                :disabled="form.busy"
              />
            </div>
          </fieldset>
          <Button
            v-if="canAddScope"
            type="button"
            :disabled="form.busy"
            @click="form.addScope()"
            variant="secondary"
          >
            {{ msg('keys.addScope') }}
          </Button>
        </section>
      </template>
      <p v-if="form.token" class="field">
        {{ msg('projects.tokenIssued') }}<code class="break-all">{{ form.token }}</code>
      </p>
      <p v-if="form.error" class="field">{{ form.error }}</p>
      <div class="flex flex-wrap justify-end gap-2">
        <Button type="button" :disabled="form.busy" @click="form.open = false" variant="ghost">
          {{ msg('common.cancel') }}
        </Button>
        <Button
          v-if="!form.token"
          :variant="form.danger ? 'danger' : 'primary'"
          type="submit"
          :disabled="form.busy || form.revealing"
        >
          {{ msg('confirm.ok') }}
        </Button>
      </div>
    </form>
  </dialog>
</template>
