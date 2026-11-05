<script setup>
/*
  Minimale Profilseite (i18n-Infrastruktur).
  Es existiert noch keine vollständige Profilverwaltung - hier lässt sich
  vorerst nur die bevorzugte UI-Sprache einstellen, die per PATCH /api/me
  auf models.User.PreferredLanguage persistiert wird.
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
    // setLanguage wirft nicht, sondern setzt saveError.
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
    <!-- Stufe 4d: /admin/profile liegt im AdminLayout, dessen Topbar
         die h1 trägt; der Seitentitel ist deshalb eine h2 (PageHeader :level="2"). -->
    <PageHeader :level="2" :title="t('profile.title')" :breadcrumbs="breadcrumbs" />

    <ListCard :title="t('profile.cardTitle')" padded>
      <p class="profile-description">{{ t('profile.description') }}</p>

      <p v-if="currentUser" class="profile-email"><strong>{{ currentUser.email }}</strong></p>

      <div v-if="statusMsg" :class="`alert alert-${statusType} fade-in mb-16`">
        <span>{{ statusMsg }}</span>
      </div>

      <form novalidate class="edit-form" @submit.prevent="saveLanguage">
        <div class="form-group">
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

      <!-- wirkt sofort und wird ohne „Speichern“ am Konto gespeichert. -->
      <HelpTooltipsToggle class="profile-help-toggle" />
    </ListCard>

    <!-- Konto mit dem SSO einer Statusseite verknüpfen -->
    <SSOLinks />

    <!-- Datenexport (DSGVO Art. 15/20) -->
    <DataExport />

    <!-- Konto löschen (DSGVO) -->
    <AccountDeletion />
  </div>
</template>

<style scoped>
.profile-description { margin: 0 0 var(--sp-4); color: var(--text-2); }
.profile-email { margin: 0 0 var(--sp-4); }
</style>
