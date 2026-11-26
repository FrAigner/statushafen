// Reusable @vue/test-utils stubs for the sit-onyx form components that
// are used the same way in several admin views (AdminIncidents.vue,
// AdminSubscribers.vue, ...). They are deliberately stubbed here instead of
// mounted for real: in multiple mode OnyxSelect brings its own flyout/positioning
// logic (Teleport, ResizeObserver, floating-ui-like calculations), which is
// irrelevant for the respective view tests - the SUT is the view's CRUD logic,
// not sit-onyx' internals.
import { defineComponent } from 'vue'

export const OnyxButtonStub = {
  props: ['label', 'disabled', 'mode', 'color'],
  template: '<button :disabled="disabled">{{ label }}</button>',
}

export const OnyxInputStub = {
  props: ['modelValue', 'label', 'placeholder', 'required'],
  emits: ['update:modelValue'],
  template: '<input :aria-label="label" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

export const OnyxTextareaStub = {
  props: ['modelValue', 'label', 'placeholder', 'required'],
  emits: ['update:modelValue'],
  template: '<textarea :aria-label="label" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

export const OnyxSelectStub = defineComponent({
  // "multiple" must be declared as a typed Boolean prop, otherwise Vue does
  // not cast the valueless attribute `multiple` (without :multiple="true")
  // to `true` but passes it through as an empty, falsy string - v-if="multiple"
  // would then never apply.
  props: {
    modelValue: {}, options: { type: Array, default: () => [] }, multiple: { type: Boolean, default: false },
    required: { type: Boolean, default: false }, label: {}, placeholder: {},
    // As with the real OnyxSelect, on the operable element instead of the
    // root <div> (help tooltips, helpCoverage.test.js).
    ariaDescribedby: { type: String, default: undefined },
  },
  emits: ['update:modelValue'],
  // The label sits on the operable element as with the real OnyxSelect
  // (there via <label for>), not on a role-less <div> - axe reports
  // aria-label on a <div> without a role (aria-prohibited-attr) and a
  // <select> without a name (select-name). In multiple mode a named group
  // bundles the checkboxes.
  template: `
    <div class="onyx-select-stub" :role="multiple ? 'group' : undefined" :aria-label="multiple ? label : undefined" :aria-describedby="multiple ? ariaDescribedby : undefined">
      <template v-if="multiple">
        <label v-for="opt in options" :key="opt.value" class="onyx-select-stub-option">
          <input type="checkbox" :value="opt.value" :checked="(modelValue || []).includes(opt.value)" @change="toggle(opt.value)" />
          {{ opt.label }}
        </label>
      </template>
      <select v-else :aria-label="label" :aria-describedby="ariaDescribedby" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
        <option v-if="placeholder" value="">{{ placeholder }}</option>
        <option v-for="opt in options" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
      </select>
    </div>`,
  methods: {
    toggle(value: unknown) {
      const current = (this.modelValue || []) as unknown[]
      const next = current.includes(value) ? current.filter(v => v !== value) : [...current, value]
      this.$emit('update:modelValue', next)
    },
  },
})

export const OnyxCheckboxStub = {
  props: {
    modelValue: { type: Boolean, default: false },
    label: {}, disabled: { type: Boolean, default: false },
  },
  emits: ['update:modelValue'],
  template: `
    <label class="onyx-checkbox-stub">
      <input type="checkbox" :checked="modelValue" :disabled="disabled"
             @change="$emit('update:modelValue', $event.target.checked)" />
      {{ label }}
    </label>`,
}

export const onyxFormStubs = {
  OnyxButton: OnyxButtonStub,
  OnyxInput: OnyxInputStub,
  OnyxTextarea: OnyxTextareaStub,
  OnyxSelect: OnyxSelectStub,
}
