<script setup lang="ts">
/*
  Delete account (GDPR Art. 17) - a separate component instead of
  code directly in ProfileView.vue, so parallel work on the profile page
  only shares one line of inclusion.

  Flow: the confirmation dialog asks for the email address (all accounts) and
  the password (accounts with a password; leave empty for SSO accounts). If the
  user is the sole admin of status pages, DELETE /api/me answers 409 with
  the list of those pages; the dialog names them and asks for a second,
  explicit confirmation before it lets them be deleted along with the account
  via confirm_delete_page_ids.
*/
import { ref, computed, inject } from 'vue'
// In the full test run v8 reports a never-taken branch on this import line
// that does not exist in the source (mapping of the compiled SFC, same
// pattern as in AdminSSO.vue); the component is fully tested in
// AccountDeletion.test.js.
/* v8 ignore next */
import { useI18n } from 'vue-i18n'
import { routerKey } from 'vue-router'
import { useApi, isForbidden } from '../composables/useApi'
import { useAuth } from '../composables/useAuth'
import AppIcon from './AppIcon.vue'
import type { ApiError, SoleAdminPage } from '../types/api'

const { t } = useI18n()
const { apiFetch } = useApi()
const { logout } = useAuth()
// inject with a default instead of useRouter(): without a router (e.g. in the
// profile test) there is no Vue warning, and signing out still works.
const router = inject(routerKey, null)

const dialogOpen = ref(false)
const confirmEmail = ref('')
const password = ref('')
const affectedPages = ref<SoleAdminPage[]>([])
const pagesAcknowledged = ref(false)
const errorMsg = ref('')
const isDeleting = ref(false)

const canSubmit = computed(() =>
  !isDeleting.value &&
  confirmEmail.value.trim() !== '' &&
  (affectedPages.value.length === 0 || pagesAcknowledged.value)
)

const openDialog = () => {
  confirmEmail.value = ''
  password.value = ''
  affectedPages.value = []
  pagesAcknowledged.value = false
  errorMsg.value = ''
  dialogOpen.value = true
}

const closeDialog = () => {
  dialogOpen.value = false
}

// Sessions that may not act on the account (custom domain, page SSO) get the
// server's translated reason instead of "confirmation invalid".
const SESSION_CODES: (string | undefined)[] = ['custom_domain_session', 'page_sso_session']

const errorMessageFor = (err: ApiError) => {
  const code = err.data?.code
  if (SESSION_CODES.includes(code)) return t(`api.errors.${code}`)
  if (isForbidden(err)) return t('accountDeletion.confirmationInvalid')
  if (code === 'last_global_admin') return t('accountDeletion.lastGlobalAdmin')
  return t('accountDeletion.error')
}

const submit = async () => {
  isDeleting.value = true
  errorMsg.value = ''
  const body: { confirm_email: string, password: string, confirm_delete_page_ids?: number[] } = { confirm_email: confirmEmail.value, password: password.value }
  if (affectedPages.value.length > 0) {
    body.confirm_delete_page_ids = affectedPages.value.map((p) => p.id)
  }
  try {
    await apiFetch('/api/me', { method: 'DELETE', body: JSON.stringify(body) })
    logout()
    router?.push('/login')
  } catch (caught) {
    const err = caught as ApiError
    if (err.data?.code === 'sole_admin_pages') {
      // Second step: name the pages and ask for explicit confirmation again,
      // even if the list changed since the first confirmation.
      // ApiErrorBody types `pages` as string[]; for this code the backend sends objects.
      affectedPages.value = err.data.pages as unknown as SoleAdminPage[]
      pagesAcknowledged.value = false
    } else {
      errorMsg.value = errorMessageFor(err)
    }
  } finally {
    isDeleting.value = false
  }
}
</script>

<template>
  <section class="edit-card account-deletion">
    <h3>{{ t('accountDeletion.title') }}</h3>
    <p class="account-deletion-text">{{ t('accountDeletion.description') }}</p>

    <button
      v-if="!dialogOpen"
      type="button"
      class="btn btn-danger"
      data-test="account-delete-open"
      @click="openDialog"
    >
      {{ t('accountDeletion.open') }}
    </button>

    <div
      v-else
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="account-delete-dialog-title"
      class="account-deletion-dialog"
    >
      <h4 id="account-delete-dialog-title"><AppIcon name="alert-triangle" :size="16" /> {{ t('accountDeletion.dialogTitle') }}</h4>
      <p>{{ t('accountDeletion.warning') }}</p>

      <div v-if="errorMsg" class="alert alert-warning fade-in mb-16">
        <span>{{ errorMsg }}</span>
      </div>

      <form novalidate class="edit-form" @submit.prevent="submit">
        <p class="account-deletion-text" data-test="required-hint">{{ t('patterns.requiredHint') }}</p>
        <div class="form-group">
          <label class="onyx-required-marker" for="account-delete-email">{{ t('accountDeletion.emailLabel') }}</label>
          <input id="account-delete-email" v-model="confirmEmail" type="email" autocomplete="off" required aria-required="true" />
        </div>
        <div class="form-group">
          <label for="account-delete-password">{{ t('accountDeletion.passwordLabel') }}</label>
          <input id="account-delete-password" v-model="password" type="password" autocomplete="current-password" />
        </div>

        <div v-if="affectedPages.length > 0" class="account-deletion-pages" data-test="account-delete-pages">
          <p><strong>{{ t('accountDeletion.pagesIntro') }}</strong></p>
          <ul>
            <li v-for="page in affectedPages" :key="page.id">{{ page.name }} ({{ page.slug }})</li>
          </ul>
          <label class="account-deletion-ack">
            <input v-model="pagesAcknowledged" type="checkbox" data-test="account-delete-pages-ack" />
            {{ t('accountDeletion.pagesConfirm') }}
          </label>
        </div>

        <div class="account-deletion-actions">
          <button type="submit" class="btn btn-danger" data-test="account-delete-submit" :disabled="!canSubmit">
            {{ isDeleting ? t('common.deleting') : (affectedPages.length > 0 ? t('accountDeletion.submitWithPages') : t('accountDeletion.submit')) }}
          </button>
          <button type="button" class="btn btn-secondary" data-test="account-delete-cancel" @click="closeDialog">
            {{ t('common.cancel') }}
          </button>
        </div>
      </form>
    </div>
  </section>
</template>

<style scoped>
.account-deletion {
  margin-top: 24px;
  border-color: var(--major);
}
.account-deletion-text {
  color: var(--text-3);
  margin-bottom: 16px;
}
.account-deletion-dialog {
  border-top: 1px solid var(--border);
  padding-top: 16px;
}
.account-deletion-pages {
  margin-bottom: 16px;
}
.account-deletion-pages ul {
  margin: 8px 0 12px 20px;
}
.account-deletion-ack {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}
.account-deletion-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
