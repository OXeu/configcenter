import { describe, expect, it } from 'vite-plus/test'
import { readFileSync } from 'node:fs'
import { checkTemplate } from '../../../tools/oxlint/templates.ts'
import { checkDropdownStyles } from '../../../tools/oxlint/dropdown.ts'

describe('checkbox rules', () => {
  it.each([
    '<input type="checkbox" />',
    '<input type="CHECKBOX" />',
    '<input :type="\'checkbox\'" />',
    '<input :type="field.type" />',
    "<input :type=\"checked ? 'checkbox' : 'text'\" />",
    '<input v-bind="{ type: \'checkbox\' }" />',
    '<component is="input" type="checkbox" />',
    '<component :is="\'input\'" type="checkbox" />',
    '<component :is="tag" type="checkbox" />',
  ])('rejects native checkboxes: %s', (source) => {
    expect(checkTemplate(`<dialog>${source}</dialog>`)).toContain(
      'no-native-checkbox: use CheckboxField instead',
    )
  })
  it('requires shared checkbox implementations and dialog placement', () => {
    expect(checkTemplate('<dialog><button role="checkbox">Enabled</button></dialog>')).toContain(
      'checkbox-use-shared-component',
    )
    expect(checkTemplate('<dialog><CheckboxField label="Enabled" /></dialog>')).toEqual([])
    expect(checkTemplate('<dialog><input :type="\'text\'" /></dialog>')).toEqual([])
    expect(checkTemplate('<dialog><input :type="\'file\'" /></dialog>')).toEqual([])
    expect(checkTemplate('<CheckboxField label="Enabled" />')).toContain('input-outside-dialog')
    expect(checkTemplate('<checkbox-field label="Enabled" />')).toContain('input-outside-dialog')
  })
})

describe('dropdown overlay rules', () => {
  const filename = 'web/ui/src/DropdownSelect.vue'
  it('requires shared dropdowns and their floating panel class', () => {
    expect(checkTemplate('<div role="listbox" />')).toContain('dropdown-use-shared-component')
    expect(checkTemplate('<div class="dropdown-options" />', filename)).toContain(
      'dropdown-requires-overlay',
    )
    expect(
      checkTemplate('<div class="dropdown-panel dropdown-options" :style="position" />', filename),
    ).toEqual([])
  })
  it.each([
    'class="dropdown-panel static"',
    'class="dropdown-panel sm:relative"',
    'class="dropdown-panel sm:[position:static]"',
    'class="dropdown-panel contents"',
    'class="dropdown-panel" v-bind="panelProps"',
    'class="dropdown-panel" style="position: static"',
    'class="dropdown-panel" :style="panelStyle"',
    'class="dropdown-panel" :class="panelClass"',
  ])('rejects panel overrides: %s', (attributes) => {
    expect(checkTemplate(`<div ${attributes} />`, filename)).toContain(
      'dropdown-no-position-override',
    )
  })
  it.each([
    '.dropdown-panel { position: static; }',
    '@media (max-width: 639px) { .table .dropdown-content { @apply static mt-1; } }',
    '.dropdown-options { position: relative !important; }',
    '.dropdown-panel { @apply sm:sticky; }',
    '.dropdown-panel { @apply sm:[position:static]; }',
    '.dropdown-panel { all: initial; }',
    '.dropdown-panel { display: contents; }',
  ])('rejects CSS overrides at every breakpoint: %s', (source) => {
    expect(checkDropdownStyles(source)).toContain('dropdown-no-position-override')
  })
  it('requires fixed positioning and preserves parent overflow when opened', () => {
    expect(
      checkDropdownStyles(
        '@media (max-width: 639px) { .dropdown-panel { position: fixed; } }',
        true,
      ),
    ).toContain('dropdown-requires-fixed-style')
    expect(checkDropdownStyles('.dropdown-panel { color: red; }', true)).toContain(
      'dropdown-requires-fixed-style',
    )
    expect(
      checkDropdownStyles('.table-scroll:has(.dropdown-content) { overflow: visible; }'),
    ).toContain('dropdown-no-layout-toggle')
    expect(
      checkDropdownStyles(
        readFileSync('.tmp/common/ui/theme.css', 'utf8') + readFileSync('web/ui/theme.css', 'utf8'),
        true,
      ),
    ).toEqual([])
  })
})
