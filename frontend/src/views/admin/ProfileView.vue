<script setup lang="ts">
/*
  Minimal profile page (i18n infrastructure).
  There is no full profile management yet - for now only the preferred UI
  language can be set here, persisted via PATCH /api/me to
  models.User.PreferredLanguage.
  Route: /admin/profile (router/index.js)
*/
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { OnyxSelect, OnyxButton } from 'sit-onyx'
import { useAuth } from '../../composables/useAuth'
import { useLanguage } from '../../composables/useLanguage'
import AccountDeletion from '../../components/AccountDeletion.vue'
import SSOLinks from '../../components/SSOLinks.vue'
import DataExport from '../../components/DataExport.vue'
import HelpTooltipsToggle from '../../components/HelpTooltipsToggle.vue'
import PageHeader from '../../components/PageHeader.vue'
import ListCard from '../../components/ListCard.vue'
import { usePageCrumbs } from '../../composables/usePageCrumbs'

const { t } = useI18n()
const breadcrumbs = usePageCrumbs(computed(() => t('profile.title')), { global: true })
const { currentUser } = useAuth()
const { locale, supportedLocales, setLanguage, saveError } = useLanguage()

const selectedLanguage = ref(locale.value)
const isSaving = ref(false)
const statusMsg = ref('')
const statusType = ref('success')

const languageOptions = supportedLocales.map((code) => ({ label: t(`language.${code}`), value: code }))

const saveLanguage = async () => {
  isSaving.value = true
  statusMsg.value = ''
  try {
    await setLanguage(selectedLanguage.value)
    // setLanguage does not throw; it sets saveError instead.
    statusMsg.value = saveError.value ? t('profile.error') : t('profile.saved')
    statusType.value = saveError.value ? 'warning' : 'success'
  } catch (err) {
    statusMsg.value = t('profile.error')
    statusType.value = 'warning'
  } finally {
    isSaving.value = false
  }
}
</script>

<template>
  <div>
    <!-- Stage 4d: /admin/profile lives in AdminLayout, whose top bar
         carries the h1; the page title is therefore an h2 (PageHeader :level="2"). -->
    <PageHeader :level="2" :title="t('profile.title')" :breadcrumbs="breadcrumbs" />

    <ListCard :title="t('profile.cardTitle')" padded>
      <p class="profile-description">{{ t('profile.description') }}</p>

      <p v-if="currentUser" class="profile-email"><strong>{{ currentUser.email }}</strong></p>

      <div v-if="statusMsg" :class="`alert alert-${statusType} fade-in mb-16`">
        <span>{{ statusMsg }}</span>
      </div>

      <form novalidate class="edit-form" @submit.prevent="saveLanguage">
        <div id="profile-language" class="form-group">
          <OnyxSelect
            v-model="selectedLanguage"
            :options="languageOptions"
            :label="t('profile.languageLabel')"
            :list-label="t('profile.languageLabel')"
            required
          />
        </div>

        <OnyxButton type="submit" :label="t('profile.save')" color="primary" :loading="isSaving" />
      </form>

      <!-- takes effect immediately and is stored on the account without "Save". -->
      <HelpTooltipsToggle class="profile-help-toggle" />
    </ListCard>

    <!-- Link the account to a status page's SSO -->
    <SSOLinks />

    <!-- Data export (GDPR Art. 15/20) -->
    <DataExport />

    <!-- Delete account (GDPR) -->
    <AccountDeletion />
  </div>
</template>

<style scoped>
.profile-description { margin: 0 0 var(--sp-4); color: var(--text-2); }
.profile-email { margin: 0 0 var(--sp-4); }
</style>
