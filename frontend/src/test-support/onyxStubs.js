// Wiederverwendbare @vue/test-utils-Stubs für die sit-onyx-Formkomponenten,
// die in mehreren Admin-Views (AdminIncidents.vue, AdminSubscribers.vue, ...)
// gleich verwendet werden. Sie sind hier bewusst gestubbt statt echt gemountet:
// OnyxSelect bringt im multiple-Modus eigene Flyout-/Positionierungslogik mit
// (Teleport, ResizeObserver, floating-ui-artige Berechnungen), die für die
// jeweiligen View-Tests irrelevant ist - der SUT ist die CRUD-Logik der View,
// nicht sit-onyx' Interna.
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

export const OnyxSelectStub = {
  // "multiple" muss als typisiertes Boolean-Prop deklariert sein, sonst
  // castet Vue das wertlose Attribut `multiple` (ohne :multiple="true") nicht
  // zu `true`, sondern lässt es als leeren, falsy String durch - v-if="multiple"
  // würde dann nie greifen.
  props: {
    modelValue: {}, options: { type: Array, default: () => [] }, multiple: { type: Boolean, default: false },
    required: { type: Boolean, default: false }, label: {}, placeholder: {},
    // wie beim echten OnyxSelect am bedienbaren Element statt am
    // Wurzel-<div> (Hilfe-Tooltips, helpCoverage.test.js).
    ariaDescribedby: { type: String, default: undefined },
  },
  emits: ['update:modelValue'],
  // Die Beschriftung sitzt am bedienbaren Element wie beim echten
  // OnyxSelect (dort per <label for>), nicht an einem rollenlosen <div> -
  // axe meldet aria-label auf einem <div> ohne Rolle (aria-prohibited-attr)
  // und ein <select> ohne Namen (select-name). Im multiple-Modus fasst eine
  // benannte Gruppe die Checkboxen zusammen.
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
    toggle(value) {
      const current = this.modelValue || []
      const next = current.includes(value) ? current.filter(v => v !== value) : [...current, value]
      this.$emit('update:modelValue', next)
    },
  },
}

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
