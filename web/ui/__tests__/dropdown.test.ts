import { describe, expect, it } from 'vite-plus/test'
import { mount } from '@vue/test-utils'
import DropdownSelect from '@oxeu/center-common/ui/DropdownSelect.vue'
import DropdownMenu from '@oxeu/center-common/ui/DropdownMenu.vue'

function dropdown() {
  return mount(DropdownSelect, {
    props: {
      id: 'environment',
      label: 'Environment',
      modelValue: 'development',
      options: [
        { value: 'development', label: 'Development' },
        { value: 'staging', label: 'Staging' },
        { value: 'production', label: 'Production' },
      ],
    },
  })
}

describe('dropdown selection', () => {
  it('selects with the keyboard without submitting the form', async () => {
    const wrapper = dropdown()
    const trigger = wrapper.get('[role="combobox"]')
    expect(trigger.attributes('type')).toBe('button')
    expect(trigger.text()).toBe('Development')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    expect(trigger.attributes('aria-expanded')).toBe('true')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await trigger.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toEqual([['staging']])
    expect(trigger.attributes('aria-expanded')).toBe('false')
    wrapper.unmount()
  })

  it('supports boundaries, typeahead, Escape and Tab', async () => {
    const wrapper = dropdown()
    const trigger = wrapper.get('[role="combobox"]')
    await trigger.trigger('keydown', { key: 'End' })
    expect(wrapper.get('[data-active="true"]').text()).toBe('Production')
    await trigger.trigger('keydown', { key: 'Home' })
    expect(wrapper.get('[data-active="true"]').text()).toBe('Development')
    await trigger.trigger('keydown', { key: 's' })
    expect(wrapper.get('[data-active="true"]').text()).toBe('Staging')
    await trigger.trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await trigger.trigger('click')
    await trigger.trigger('keydown', { key: 'Tab' })
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('selects by pointer, closes on blur and disables while saving', async () => {
    const wrapper = dropdown()
    const trigger = wrapper.get('[role="combobox"]')
    await trigger.trigger('click')
    expect(wrapper.get('[aria-selected="true"]').text()).toBe('Development')
    await wrapper.findAll('[role="option"]')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['staging']])
    await trigger.trigger('click')
    await trigger.trigger('blur')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    await trigger.trigger('click')
    await wrapper.setProps({ disabled: true })
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(trigger.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})

describe('dropdown menu', () => {
  it('stays open when clicking the panel, account details or disabled actions', async () => {
    const wrapper = mount(DropdownMenu, {
      props: { label: 'Account' },
      slots: { default: '<p>Account details</p><button disabled>Unavailable</button>' },
      attachTo: document.body,
    })
    const trigger = wrapper.get('button[aria-controls]')
    await trigger.trigger('click')
    const panel = wrapper.get<HTMLElement>('.dropdown-content')
    panel.element.focus()
    await panel.trigger('click')
    await panel.get('p').trigger('click')
    await panel.get('button').trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(wrapper.find('.dropdown-content').exists()).toBe(true)
    await panel.trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('.dropdown-content').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })

  it.each(['a', 'button'])('closes when selecting nested content in a %s action', async (tag) => {
    const wrapper = mount(DropdownMenu, {
      props: { label: 'Account' },
      slots: {
        default: '<a href="#keys"><span>API keys</span></a><button><span>Sign out</span></button>',
      },
      attachTo: document.body,
    })
    const trigger = wrapper.get('button[aria-controls]')
    await trigger.trigger('click')
    await wrapper.get(`.dropdown-content ${tag} span`).trigger('click')
    expect(wrapper.find('.dropdown-content').exists()).toBe(false)
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })
})
