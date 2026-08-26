import { invoke } from '@tauri-apps/api/core'
import { getCurrentWebviewWindow } from '@tauri-apps/api/webviewWindow'
import { onMounted } from 'vue'

import type { MultiplayerStatus, ProtocolMessage } from '@/types/multiplayer'

import { INVOKE_KEY, LISTEN_KEY, WINDOW_LABEL } from '@/constants'
import { hideWindow, showWindow } from '@/plugins/window'
import { useMultiplayerStore } from '@/stores/multiplayer'
import { getCursorMonitor } from '@/utils/monitor'

import { useTauriListen } from './useTauriListen'

export function useMultiplayer() {
  const store = useMultiplayerStore()
  const appWindow = getCurrentWebviewWindow()

  async function syncWindow(state: MultiplayerStatus['state']) {
    const active = state !== 'disconnected'
    if (appWindow.label === WINDOW_LABEL.MAIN && active) hideWindow()
    if (appWindow.label === WINDOW_LABEL.MULTIPLAYER) {
      if (!active) return hideWindow()
      const monitor = await getCursorMonitor()
      if (monitor) {
        await appWindow.setPosition(monitor.position)
        await appWindow.setSize(monitor.size)
      }
      showWindow()
    }
  }

  useTauriListen<ProtocolMessage>(LISTEN_KEY.MULTIPLAYER_MESSAGE, ({ payload }) => {
    store.applyMessage(payload)
  })

  useTauriListen<MultiplayerStatus>(LISTEN_KEY.MULTIPLAYER_STATUS, ({ payload }) => {
    store.applyStatus(payload)
    void syncWindow(payload.state)
  })

  onMounted(async () => {
    await store.$tauri.start()
    const status = await invoke<MultiplayerStatus>(INVOKE_KEY.MULTIPLAYER_STATUS)
    store.applyStatus(status)
    await syncWindow(status.state)
  })

  return store
}
