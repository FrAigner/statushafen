import pluginVue from 'eslint-plugin-vue'
import vueTsEslintConfig from '@vue/eslint-config-typescript'

export default [
  // StrykerJS sandbox and reports (npm run test:mutation) are copies or generated files and must not
  // be linted; the same goes for the coverage report (npm run test:coverage).
  { ignores: ['.stryker-tmp/**', 'reports/**', 'dist/**', 'coverage/**'] },
  ...pluginVue.configs['flat/essential'],
  // Also covers TypeScript: *.ts and <script lang="ts"> in SFCs get the typescript-eslint parser and its
  // recommended rules (e.g. no-explicit-any, ban-ts-comment). Type errors are vue-tsc's job (npm run typecheck).
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
