<script setup lang="ts">
import Button from '@oxeu/center-common/ui/Button.vue'
import DropdownMenu from '@oxeu/center-common/ui/DropdownMenu.vue'
import { computed, ref, shallowRef, useId, watch } from 'vue'
import type { editorHighlight, Language } from './editor-highlight.ts'
import { formatJson } from './format-json.ts'
import type { Copy } from './i18n.ts'

type Mode = 'auto' | 'text' | Language
const props = defineProps<{ value: string; msg: Copy['msg']; disabled?: boolean }>()
const emit = defineEmits<{ format: [value: string] }>()
const mode = ref<Mode>('auto')
const modes = computed<Record<Mode, string>>(() => ({
  auto: props.msg('editor.autoHighlight'),
  json: 'JSON',
  yaml: 'YAML',
  javascript: 'JavaScript',
  text: props.msg('editor.plainText'),
}))
const language = computed(() => {
  if (mode.value === 'text') return null
  if (mode.value !== 'auto') return mode.value
  const first = props.value.trimStart()[0]
  return first === '{' || first === '[' ? 'json' : null
})
const highlight = shallowRef<Awaited<ReturnType<typeof editorHighlight>>>()
watch(
  language,
  async (lang, _, onCleanup) => {
    highlight.value = undefined
    if (!lang) return
    let current = true
    onCleanup(() => {
      current = false
    })
    try {
      const { editorHighlight } = await import('./editor-highlight.ts')
      const tokenize = await editorHighlight(lang)
      if (current) highlight.value = tokenize
    } catch {
      // Editing stays available if the syntax grammar fails to load.
    }
  },
  { immediate: true },
)

const composing = ref(false)
function setComposing(active: boolean) {
  composing.value = active
}
const scroll = shallowRef({ x: 0, y: 0 })
function syncScroll(event: Event) {
  const input = event.currentTarget
  if (input instanceof HTMLTextAreaElement) {
    scroll.value = { x: input.scrollLeft, y: input.scrollTop }
  }
}
const offset = computed(() => ({
  transform: 'translate(' + -scroll.value.x + 'px, ' + -scroll.value.y + 'px)',
}))
const lines = computed(() => {
  if (composing.value || props.disabled || !props.value || props.value.length > 50_000) return null
  try {
    return highlight.value?.(props.value) ?? null
  } catch {
    return null
  }
})

const invalidJson = ref(false)
const validationId = useId()
watch(
  () => props.value,
  () => {
    invalidJson.value = false
  },
)
function format() {
  try {
    const formatted = formatJson(props.value)
    if (formatted !== props.value) emit('format', formatted)
  } catch {
    invalidJson.value = true
  }
}
</script>

<template>
  <div class="config-value-frame" :class="{ 'is-highlighted': lines }">
    <div v-if="lines" class="config-value-mirror" aria-hidden="true">
      <pre
        :style="offset"
      ><template v-for="(line, index) in lines" :key="index">{{ index ? '\n' : '' }}<span v-for="(token, i) in line" :key="i" :style="token.colors">{{ token.content }}</span></template></pre>
    </div>
    <slot :sync-scroll="syncScroll" :set-composing="setComposing" />
  </div>
  <div class="flex flex-wrap items-center justify-between gap-2">
    <DropdownMenu :label="modes[mode]" :disabled="disabled">
      <Button
        v-for="(label, option) in modes"
        :key="option"
        type="button"
        variant="ghost"
        block
        align="start"
        :aria-pressed="mode === option"
        :data-active="mode === option"
        @click="mode = option"
      >
        {{ label }}
      </Button>
    </DropdownMenu>
    <Button
      type="button"
      variant="secondary"
      size="sm"
      :disabled="disabled || composing || !value.trim()"
      :aria-describedby="invalidJson ? validationId : undefined"
      @click="format"
    >
      {{ msg('editor.formatJson') }}
    </Button>
  </div>
  <p v-if="invalidJson" :id="validationId" class="field">{{ msg('editor.invalidJson') }}</p>
</template>

<style scoped>
.config-value-frame {
  position: relative;
  color: var(--color-foreground);
}
.config-value-mirror {
  position: absolute;
  inset: 1px;
  overflow: hidden;
  pointer-events: none;
}
.config-value-mirror pre,
:slotted(.config-value-input) {
  padding: 0.625rem 0.75rem;
  font-family: var(--font-mono, monospace);
  font-size: 0.875rem;
  font-variant-ligatures: none;
  line-height: 1.5;
  white-space: pre;
  overflow-wrap: normal;
  tab-size: 4;
}
:slotted(.config-value-input) {
  position: relative;
  display: block;
  height: 10rem;
  min-height: 5rem;
  max-height: 50dvh;
  overflow: auto;
  resize: vertical;
}
.is-highlighted :slotted(.config-value-input) {
  color: transparent;
  caret-color: var(--color-foreground);
}
.is-highlighted :slotted(.config-value-input)::selection {
  color: var(--color-primary-foreground);
  background: var(--color-primary);
}
.config-value-mirror span {
  color: var(--token-light, inherit);
}
:global(:root:has([data-theme='dark']) .config-value-mirror span) {
  color: var(--token-dark, inherit);
}
@media (max-width: 639px) {
  .config-value-mirror pre,
  :slotted(.config-value-input) {
    font-size: 1rem;
  }
}
@media (forced-colors: active) {
  .config-value-mirror {
    display: none;
  }
  .is-highlighted :slotted(.config-value-input) {
    color: CanvasText;
    caret-color: auto;
  }
}
</style>
