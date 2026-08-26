<script setup lang="ts">
import { getCurrentWebviewWindow } from '@tauri-apps/api/webviewWindow'
import { confirm } from '@tauri-apps/plugin-dialog'
import { relaunch } from '@tauri-apps/plugin-process'
import { useEventListener } from '@vueuse/core'
import { Button, Space } from 'ant-design-vue'
import { checkInputMonitoringPermission, requestInputMonitoringPermission } from 'tauri-plugin-macos-permissions-api'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ProListItem from '@/components/pro-list-item/index.vue'
import ProList from '@/components/pro-list/index.vue'
import { isMac } from '@/utils/platform'

const authorized = ref(false)
const restartRequired = ref(false)
const { t } = useI18n()
let permissionChecked = false

async function refreshPermission() {
  const nextAuthorized = await checkInputMonitoringPermission()

  if (permissionChecked && !authorized.value && nextAuthorized) {
    restartRequired.value = true
  }

  authorized.value = nextAuthorized
  permissionChecked = true
}

useEventListener(window, 'focus', () => {
  void refreshPermission()
})

onMounted(async () => {
  await refreshPermission()

  if (authorized.value) return

  const appWindow = getCurrentWebviewWindow()

  await appWindow.setAlwaysOnTop(true)

  const confirmed = await confirm(t('pages.preference.general.hints.inputMonitoringPermissionGuide'), {
    title: t('pages.preference.general.labels.inputMonitoringPermission'),
    okLabel: t('pages.preference.general.buttons.openNow'),
    cancelLabel: t('pages.preference.general.buttons.openLater'),
    kind: 'warning',
  })

  if (!confirmed) return

  await appWindow.setAlwaysOnTop(false)

  requestInputMonitoringPermission()
})
</script>

<template>
  <ProList
    v-if="isMac"
    :title="$t('pages.preference.general.labels.permissionsSettings')"
  >
    <ProListItem
      :description="$t('pages.preference.general.hints.inputMonitoringPermission')"
      :title="$t('pages.preference.general.labels.inputMonitoringPermission')"
    >
      <div
        v-if="authorized"
        class="flex items-center gap-2"
      >
        <Space
          class="text-success font-bold"
          :size="4"
        >
          <div class="i-solar:verified-check-bold text-4.5" />

          <span class="whitespace-nowrap">{{ $t('pages.preference.general.status.authorized') }}</span>
        </Space>

        <Button
          v-if="restartRequired"
          size="small"
          type="link"
          @click="relaunch"
        >
          {{ $t('composables.useAppMenu.labels.restartApp') }}
        </Button>
      </div>

      <Space
        v-else
        class="cursor-pointer text-danger font-bold"
        :size="4"
        @click="requestInputMonitoringPermission"
      >
        <div class="i-solar:round-arrow-right-bold text-4.5" />

        <span class="whitespace-nowrap">{{ $t('pages.preference.general.status.authorize') }}</span>
      </Space>
    </ProListItem>
  </ProList>
</template>
