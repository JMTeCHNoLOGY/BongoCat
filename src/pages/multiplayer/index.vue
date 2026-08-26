<script setup lang="ts">
import { PhysicalPosition } from '@tauri-apps/api/dpi'
import { emit, listen } from '@tauri-apps/api/event'
import { getCurrentWebviewWindow } from '@tauri-apps/api/webviewWindow'
import { cursorPosition } from '@tauri-apps/api/window'
import { info, error as logError } from '@tauri-apps/plugin-log'
import { useEventListener } from '@vueuse/core'
import { onMounted, onUnmounted, useTemplateRef, watch } from 'vue'

import type { InputPayload, ProtocolMessage, SnapshotPayload } from '@/types/multiplayer'
import type { Point } from '@/utils/multiplayer-layout'

import { useTauriListen } from '@/composables/useTauriListen'
import { LISTEN_KEY } from '@/constants'
import { useCatStore } from '@/stores/cat'
import { useModelStore } from '@/stores/model'
import { useMultiplayerStore } from '@/stores/multiplayer'
import { getCursorMonitor } from '@/utils/monitor'
import { getAvatarMaximumScale } from '@/utils/multiplayer-layout'
import { MultiplayerScene } from '@/utils/multiplayer-scene'

const canvas = useTemplateRef<HTMLCanvasElement>('canvas')
const backgrounds = useTemplateRef<HTMLElement>('backgrounds')
const overlays = useTemplateRef<HTMLElement>('overlays')
const store = useMultiplayerStore()
const catStore = useCatStore()
const modelStore = useModelStore()
const appWindow = getCurrentWebviewWindow()
const scene = new MultiplayerScene(
  () => getAvatarMaximumScale(catStore.window.scale, devicePixelRatio),
  profile => store.getLayoutPosition(profile.name),
  (profile, position) => store.setLayoutPosition(profile.name, position),
)
let initialized = false
let rendererAvailable = false
let unlistenSingleModel = () => {}
let syncRunning = false
let syncPending = false
let windowPosition = new PhysicalPosition(0, 0)
let windowScaleFactor = 1
let latestPointer: Point | undefined
let ignoringCursor: boolean | undefined
let cursorUpdate = Promise.resolve()
let unlistenMoved = () => {}
let unlistenScaleChanged = () => {}

async function syncWindowMetrics() {
  [windowPosition, windowScaleFactor] = await Promise.all([
    appWindow.outerPosition(),
    appWindow.scaleFactor(),
  ])
}

function scenePoint(point: Point) {
  return {
    x: (point.x - windowPosition.x) / windowScaleFactor,
    y: (point.y - windowPosition.y) / windowScaleFactor,
  }
}

function setCursorCapture(capture: boolean) {
  const ignore = !capture
  if (ignore === ignoringCursor) return
  ignoringCursor = ignore
  cursorUpdate = cursorUpdate
    .then(() => appWindow.setIgnoreCursorEvents(ignore))
    .catch(error => logError(`multiplayer cursor mode failed: ${String(error)}`))
}

async function refreshPointerState() {
  if (!initialized) return
  const point = scenePoint(await cursorPosition())
  latestPointer = point
  setCursorCapture(scene.pointerMove(point))
}

async function syncPlayersNow() {
  if (!initialized || !rendererAvailable || !store.room) return
  void info(`multiplayer scene sync: players=${store.players.length} models=${modelStore.models.length}`)
  const activeIds = new Set(store.players.map(player => player.playerId))
  for (const player of store.players) {
    const model = modelStore.resolveModel(player.skinId, player.mode)
    if (!model) {
      void logError(`multiplayer model unavailable: skin=${player.skinId} mode=${player.mode}`)
      continue
    }
    try {
      await scene.upsert(player, model, player.playerId === store.room.self.playerId)
      void info(`multiplayer avatar ready: player=${player.playerId} model=${model.id}`)
    } catch (error) {
      void logError(`multiplayer avatar failed: ${String(error)}`)
    }
  }
  for (const child of [...scene.playerIds()]) {
    if (!activeIds.has(child)) scene.remove(child)
  }
  await refreshPointerState()
}

async function syncPlayers() {
  syncPending = true
  if (syncRunning) return

  syncRunning = true
  try {
    while (syncPending) {
      syncPending = false
      await syncPlayersNow()
    }
  } finally {
    syncRunning = false
  }
}

onMounted(async () => {
  unlistenSingleModel = await listen(LISTEN_KEY.SINGLE_MODEL_RELEASED, () => {
    rendererAvailable = true
    void syncPlayers()
  })
  if (!canvas.value || !backgrounds.value || !overlays.value) return
  await syncWindowMetrics()
  unlistenMoved = await appWindow.onMoved(({ payload }) => {
    windowPosition = payload
    void refreshPointerState()
  })
  unlistenScaleChanged = await appWindow.onScaleChanged(({ payload }) => {
    windowScaleFactor = payload.scaleFactor
    void syncWindowMetrics().then(refreshPointerState)
  })
  await scene.init(canvas.value, backgrounds.value, overlays.value)
  initialized = true
  void info('multiplayer scene initialized')
  setCursorCapture(false)
  if (store.state !== 'disconnected') await emit(LISTEN_KEY.MULTIPLAYER_RENDERER_REQUESTED)
})

onUnmounted(() => {
  unlistenSingleModel()
  unlistenMoved()
  unlistenScaleChanged()
  setCursorCapture(false)
  scene.destroy()
})
useEventListener('resize', () => {
  scene.layout()
  void syncWindowMetrics().then(refreshPointerState)
})

watch([() => store.players, () => modelStore.models], syncPlayers, { deep: true })
watch(() => catStore.window.scale, () => scene.layout())
watch(() => store.state, (state) => {
  if (state !== 'disconnected') {
    rendererAvailable = false
    void emit(LISTEN_KEY.MULTIPLAYER_RENDERER_REQUESTED)
    return
  }

  rendererAvailable = false
  scene.cancelDrag()
  scene.clear()
  setCursorCapture(false)
  void emit(LISTEN_KEY.MULTIPLAYER_MODELS_RELEASED)
})

useTauriListen<ProtocolMessage>(LISTEN_KEY.MULTIPLAYER_MESSAGE, ({ payload }) => {
  if (payload.type === 'input') {
    const { playerId, event } = payload.payload as InputPayload
    void scene.applyEvent(playerId, event)
  } else if (payload.type === 'snapshot') {
    const { playerId, snapshot } = payload.payload as SnapshotPayload
    void scene.applySnapshot(playerId, snapshot)
  }
})

useTauriListen<{ kind: string, value: unknown }>(LISTEN_KEY.DEVICE_CHANGED, async ({ payload }) => {
  const playerId = store.room?.self.playerId
  if (!playerId) return

  if (payload.kind === 'MouseMove') {
    const point = payload.value as { x: number, y: number }
    latestPointer = scenePoint(point)
    setCursorCapture(scene.pointerMove(latestPointer))
    const monitor = await getCursorMonitor(new PhysicalPosition(point.x, point.y))
    if (monitor) {
      return scene.applyLocal(playerId, payload.kind, payload.value, {
        x: monitor.position.x,
        y: monitor.position.y,
        width: monitor.size.width,
        height: monitor.size.height,
      })
    }
  } else if (payload.kind === 'MousePress' && latestPointer) {
    setCursorCapture(scene.pointerDown(latestPointer, String(payload.value)))
  } else if (payload.kind === 'MouseRelease' && latestPointer) {
    setCursorCapture(scene.pointerUp(latestPointer, String(payload.value)))
  }
  void scene.applyLocal(playerId, payload.kind, payload.value)
})

useTauriListen<{ kind: string, name: string, value: number }>(LISTEN_KEY.GAMEPAD_CHANGED, ({ payload }) => {
  const playerId = store.room?.self.playerId
  if (playerId) void scene.applyLocal(playerId, payload.kind, { name: payload.name, value: payload.value })
})
</script>

<template>
  <div
    class="relative size-screen overflow-hidden"
    style="isolation: isolate"
  >
    <div
      ref="backgrounds"
      class="pointer-events-none absolute inset-0 overflow-hidden"
      style="z-index: 0"
    />
    <canvas
      ref="canvas"
      class="pointer-events-none absolute inset-0 block size-screen"
      style="z-index: 1"
    />
    <div
      ref="overlays"
      class="pointer-events-none absolute inset-0 overflow-hidden"
      style="z-index: 2"
    />
  </div>
</template>
