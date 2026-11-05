import pluginVue from 'eslint-plugin-vue'
import vueTsEslintConfig from '@vue/eslint-config-typescript'

export default [
  // Sandbox und Reports von StrykerJS (npm run test:mutation)
  // sind Kopien bzw. generierte Dateien und dürfen nicht gelintet werden.
  { ignores: ['.stryker-tmp/**', 'reports/**', 'dist/**'] },
  ...pluginVue.configs['flat/essential'],
  ...vueTsEslintConfig(),
  {
    languageOptions: {
      ecmaVersion: 2020,
      sourceType: 'module',
    },
    rules: {
      'vue/block-lang': 'off',
      'vue/multi-word-component-names': 'off',
      '@typescript-eslint/no-unused-vars': 'off',
    },
  },
]
