import { describe, expect, it } from 'vite-plus/test'
import { mount } from '@vue/test-utils'
import CheckboxField from '@oxeu/center-common/ui/CheckboxField.vue'

describe('shared checkbox', () => {
  it('exposes its label and checked state without a native checkbox or form submission', async () => {
    const wrapper = mount(CheckboxField, { props: { label: 'Enabled', modelValue: false } })
    const control = wrapper.get('[role="checkbox"]')
    expect(wrapper.find('input').exists()).toBe(false)
    expect(control.attributes('type')).toBe('button')
    expect(control.attributes('aria-label')).toBe('Enabled')
    expect(control.attributes('aria-checked')).toBe('false')
    expect(wrapper.get('label').attributes('for')).toBe(control.attributes('id'))
    await control.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    await wrapper.setProps({ modelValue: true })
    expect(control.attributes('aria-checked')).toBe('true')
    await control.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
    wrapper.unmount()
  })

  it('adds and removes only its permission without mutating the source array', async () => {
    const selected = ['config:read']
    const wrapper = mount(CheckboxField, {
      props: { label: 'Write configuration', value: 'config:write', modelValue: selected },
    })
    const control = wrapper.get('[role="checkbox"]')
    await control.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['config:read', 'config:write']]])
    expect(selected).toEqual(['config:read'])
    await wrapper.setProps({ modelValue: ['config:read', 'config:write'] })
    await control.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['config:read']])
    wrapper.unmount()
  })

  it('does not change disabled checkboxes', async () => {
    const wrapper = mount(CheckboxField, {
      props: { label: 'Enabled', modelValue: true, disabled: true },
    })
    const control = wrapper.get('[role="checkbox"]')
    expect(control.attributes('disabled')).toBeDefined()
    await control.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(control.attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })
})
