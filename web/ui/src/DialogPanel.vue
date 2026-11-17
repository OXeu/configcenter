<script setup>
import { computed, ref, watch, nextTick, useId } from 'vue'
import DropdownSelect from '@oxeu/center-common/ui/DropdownSelect.vue'
import CheckboxField from '@oxeu/center-common/ui/CheckboxField.vue'
const formId = useId()
const props = defineProps({
  form: { type: Object, required: true },
  msg: { type: Function, required: true },
})
const canAddScope = computed(() =>
  props.form.scopeProjects.some(
    (/** @type {import('./forms.ts').Choice} */ option) =>
      !props.form.scopes?.some(
        (/** @type {import('./forms.ts').ScopeDraft} */ scope) => scope.project === option.value,
      ),
  ),
)
const dialog = ref(/** @type {HTMLDialogElement | null} */ (null))
watch(
  () => props.form.open,
  async (open) => {
    await nextTick()
    if (open) dialog.value?.showModal()
    else dialog.value?.close()
  },
  { immediate: true },
)
/** @param {Event} event */
function cancel(event) {
  if (props.form.busy) {
    event.preventDefault()
    return
  }
  props.form.open = false
}
/** @param {{ sensitive?: boolean }} field */
function hidden(field) {
  return sensitive(field) && !props.form.valueRevealed
}
/** @param {{ sensitive?: boolean }} field */
function sensitive(field) {
  return field.sensitive && (props.form.secret || props.form.keepSecret)
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
          <textarea
            v-else-if="field.multiline"
            :id="`${formId}-${field.key}`"
            v-model="form.values[field.key]"
            class="input"
            @input="form.touch(field.key)"
            :required="field.required"
            :disabled="form.busy"
          ></textarea>
          <input
            v-else
            :id="`${formId}-${field.key}`"
            v-model="form.values[field.key]"
            class="input"
            :required="field.required"
            :disabled="form.busy"
          />
          <button
            v-if="sensitive(field)"
            type="button"
            class="btn-ghost"
            :disabled="form.busy || form.revealing"
            :aria-pressed="form.valueRevealed"
            @click="form.toggleValue"
          >
            {{ msg(form.valueRevealed ? 'secret.hide' : 'secret.show') }}
          </button>
          <button
            v-if="field.sensitive && form.keepSecret"
            type="button"
            class="btn-outline"
            :disabled="form.busy || form.revealing"
            @click="form.replaceValue"
          >
            {{ msg('secret.replace') }}
          </button>
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
              <button
                type="button"
                class="btn-outline btn-icon"
                :aria-label="msg('keys.excludeScope')"
                :disabled="form.busy"
                @click="form.excludeScope(scope.id)"
              >
                <i class="ri-close-line" aria-hidden="true"></i>
              </button>
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
          <button
            v-if="canAddScope"
            type="button"
            class="btn-outline"
            :disabled="form.busy"
            @click="form.addScope"
          >
            {{ msg('keys.addScope') }}
          </button>
        </section>
      </template>
      <p v-if="form.token" class="field">
        {{ msg('projects.tokenIssued') }}<code class="break-all">{{ form.token }}</code>
      </p>
      <p v-if="form.error" class="field">{{ form.error }}</p>
      <div class="flex flex-wrap justify-end gap-2">
        <button class="btn-ghost" type="button" :disabled="form.busy" @click="form.open = false">
          {{ msg('common.cancel') }}
        </button>
        <button
          v-if="!form.token"
          :class="form.danger ? 'btn-danger' : 'btn'"
          type="submit"
          :disabled="form.busy || form.revealing"
        >
          {{ msg('confirm.ok') }}
        </button>
      </div>
    </form>
  </dialog>
</template>
