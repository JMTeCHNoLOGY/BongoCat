import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import type { MultiplayerStatus, ProtocolError, ProtocolMessage, RoomJoined } from '@/types/multiplayer'
import type { Point } from '@/utils/multiplayer-layout'

import { reduceRoomMessage } from '@/utils/multiplayer'

const DEFAULT_ENDPOINT = import.meta.env.VITE_MULTIPLAYER_WS_URL || 'ws://127.0.0.1:8080/v1/ws'

export const useMultiplayerStore = defineStore('multiplayer', () => {
  const endpoint = ref(DEFAULT_ENDPOINT)
  const name = ref('')
  const state = ref<MultiplayerStatus['state']>('disconnected')
  const room = ref<RoomJoined>()
  const lastError = ref<ProtocolError>()
  const layoutPositions = ref<Record<string, Point>>({})

  const connected = computed(() => state.value === 'connected')
  const players = computed(() => [...(room.value?.players ?? [])].sort((left, right) => left.order - right.order))

  function applyStatus(status: MultiplayerStatus) {
    state.value = status.state
    if (status.room) room.value = status.room
    if (status.error) lastError.value = status.error
    if (status.state === 'disconnected') room.value = void 0
  }

  function applyMessage(message: ProtocolMessage) {
    if (message.type === 'room_joined') {
      room.value = message.payload as RoomJoined
      state.value = 'connected'
      lastError.value = void 0
      return
    }
    if (!room.value) return

    if (message.type === 'member_joined' || message.type === 'member_updated') {
      room.value = reduceRoomMessage(room.value, message)
    } else if (message.type === 'member_left') {
      room.value = reduceRoomMessage(room.value, message)
    } else if (message.type === 'error') {
      lastError.value = message.payload as ProtocolError
    }
  }

  function layoutPositionKey(playerName: string) {
    return room.value ? `${room.value.roomCode}:${playerName}` : ''
  }

  function getLayoutPosition(playerName: string) {
    const key = layoutPositionKey(playerName)
    return key ? layoutPositions.value[key] : void 0
  }

  function setLayoutPosition(playerName: string, position: Point) {
    const key = layoutPositionKey(playerName)
    if (!key) return
    layoutPositions.value[key] = position

    const keys = Object.keys(layoutPositions.value)
    if (keys.length > 128) delete layoutPositions.value[keys[0]]
  }

  return {
    endpoint,
    name,
    state,
    room,
    lastError,
    layoutPositions,
    connected,
    players,
    applyStatus,
    applyMessage,
    getLayoutPosition,
    setLayoutPosition,
  }
}, {
  tauri: {
    filterKeys: ['state', 'room', 'lastError'],
  },
})
