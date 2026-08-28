<script setup lang="ts">
import { invoke } from '@tauri-apps/api/core'
import { writeText } from '@tauri-apps/plugin-clipboard-manager'
import { useDebounceFn } from '@vueuse/core'
import { Alert, Button, Empty, Flex, Input, message, Spin, Tag } from 'ant-design-vue'
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import type { ProtocolError, RoomJoined, RoomSummary } from '@/types/multiplayer'

import ProListItem from '@/components/pro-list-item/index.vue'
import ProList from '@/components/pro-list/index.vue'
import { INVOKE_KEY } from '@/constants'
import { useGeneralStore } from '@/stores/general'
import { useModelStore } from '@/stores/model'
import { useMultiplayerStore } from '@/stores/multiplayer'
import { generatePlayerName, generateRoomName, getLatencyTagColor, normalizeRoomCode, validateMultiplayerEndpoint } from '@/utils/multiplayer'

const store = useMultiplayerStore()
const modelStore = useModelStore()
const generalStore = useGeneralStore()
const roomCode = ref('')
const roomName = ref('')
const loading = ref(false)
const rooms = ref<RoomSummary[]>([])
const roomsLoading = ref(false)
const roomsError = ref<ProtocolError>()
let roomListRequestId = 0
const { t } = useI18n()

function protocolError(error: unknown): ProtocolError {
  if (typeof error === 'object' && error && 'code' in error) return error as ProtocolError
  if (typeof error === 'string') {
    try {
      const parsed = JSON.parse(error)
      if (parsed && typeof parsed === 'object' && 'code' in parsed) return parsed as ProtocolError
    } catch { /* use the original message */ }
  }
  return { code: 'CONNECTION_FAILED', message: String(error) }
}

async function connect(
  command: typeof INVOKE_KEY.MULTIPLAYER_CREATE_ROOM | typeof INVOKE_KEY.MULTIPLAYER_JOIN_ROOM,
  selectedRoomCode?: string,
) {
  if (!validateMultiplayerEndpoint(store.endpoint)) {
    return message.error(t('pages.preference.multiplayer.hints.invalidEndpoint'))
  }
  const model = modelStore.currentModel
  if (!model) return

  loading.value = true
  store.lastError = void 0
  const enteredName = store.name.trim()
  const isCreating = command === INVOKE_KEY.MULTIPLAYER_CREATE_ROOM
  const enteredRoomName = roomName.value.trim()

  try {
    for (let attempt = 0; attempt < 5; attempt++) {
      const name = enteredName || generatePlayerName(generalStore.appearance.language)
      const generatedRoomName = isCreating && !enteredRoomName
        ? generateRoomName(generalStore.appearance.language)
        : enteredRoomName
      if (isCreating) roomName.value = generatedRoomName
      try {
        const payload = isCreating
          ? {
              endpoint: store.endpoint.trim(),
              roomName: generatedRoomName,
              profile: { name, skinId: model.skinId, mode: model.mode },
            }
          : {
              endpoint: store.endpoint.trim(),
              roomCode: normalizeRoomCode(selectedRoomCode ?? roomCode.value),
              profile: { name, skinId: model.skinId, mode: model.mode },
            }
        const joined = await invoke<RoomJoined>(command, payload)
        store.name = name
        store.applyMessage({ v: 'v1', type: 'room_joined', payload: joined })
        roomCode.value = joined.roomCode
        roomName.value = joined.roomName || generatedRoomName
        return
      } catch (error) {
        const nextError = protocolError(error)
        if (!enteredName && nextError.code === 'NAME_TAKEN' && attempt < 4) continue
        if (isCreating && !enteredRoomName && nextError.code === 'ROOM_NAME_TAKEN' && attempt < 4) continue
        throw nextError
      }
    }
  } catch (error) {
    store.lastError = protocolError(error)
    message.error(t(`pages.preference.multiplayer.errors.${store.lastError.code}`, store.lastError.message))
  } finally {
    loading.value = false
  }
}

async function leaveRoom() {
  await invoke(INVOKE_KEY.MULTIPLAYER_LEAVE_ROOM)
  store.applyStatus({ state: 'disconnected' })
  void refreshRooms()
}

async function copyRoomCode() {
  if (!store.room) return
  await writeText(store.room.roomCode)
  message.success(t('pages.preference.multiplayer.hints.copied'))
}

function latencyText(playerId: string, online: boolean) {
  const latency = store.memberLatencies[playerId]
  return online && latency !== undefined ? `${latency} ms` : '--'
}

async function refreshRooms() {
  const requestId = ++roomListRequestId
  const endpoint = store.endpoint.trim()

  if (!validateMultiplayerEndpoint(endpoint)) {
    rooms.value = []
    roomsError.value = undefined
    roomsLoading.value = false
    return
  }

  roomsLoading.value = true
  roomsError.value = undefined

  try {
    const nextRooms = await invoke<RoomSummary[]>(INVOKE_KEY.MULTIPLAYER_LIST_ROOMS, { endpoint })
    if (requestId !== roomListRequestId || endpoint !== store.endpoint.trim()) return
    rooms.value = nextRooms
  } catch (error) {
    if (requestId !== roomListRequestId) return
    rooms.value = []
    roomsError.value = protocolError(error)
  } finally {
    if (requestId === roomListRequestId) roomsLoading.value = false
  }
}

const scheduleRefreshRooms = useDebounceFn(() => {
  void refreshRooms()
}, 400)

function isRoomFull(room: RoomSummary) {
  return room.playerCount >= room.maxPlayers
}

function joinListedRoom(room: RoomSummary) {
  roomCode.value = room.roomCode
  void connect(INVOKE_KEY.MULTIPLAYER_JOIN_ROOM, room.roomCode)
}

onMounted(() => {
  void refreshRooms()
})

watch(() => store.endpoint, () => {
  roomListRequestId++
  rooms.value = []
  roomsLoading.value = false
  roomsError.value = undefined
  scheduleRefreshRooms()
})
</script>

<template>
  <Alert
    v-if="store.lastError"
    class="mb-4"
    closable
    :message="$t(`pages.preference.multiplayer.errors.${store.lastError.code}`, store.lastError.message)"
    show-icon
    type="error"
    @close="store.lastError = undefined"
  />

  <ProList :title="$t('pages.preference.multiplayer.labels.connection')">
    <ProListItem
      :description="$t('pages.preference.multiplayer.hints.endpoint')"
      :title="$t('pages.preference.multiplayer.labels.endpoint')"
    >
      <Input
        v-model:value="store.endpoint"
        class="w-80"
        :disabled="store.state !== 'disconnected'"
      />
    </ProListItem>

    <ProListItem
      :description="$t('pages.preference.multiplayer.hints.name')"
      :title="$t('pages.preference.multiplayer.labels.name')"
    >
      <Input
        v-model:value="store.name"
        class="w-60"
        :disabled="store.state !== 'disconnected'"
        :maxlength="24"
        :placeholder="$t('pages.preference.multiplayer.placeholders.randomName')"
      />
    </ProListItem>

    <ProListItem
      v-if="!store.room"
      :description="$t('pages.preference.multiplayer.hints.roomName')"
      :title="$t('pages.preference.multiplayer.labels.roomName')"
    >
      <Input
        v-model:value="roomName"
        class="w-60"
        :disabled="loading"
        :maxlength="24"
        :placeholder="$t('pages.preference.multiplayer.placeholders.roomName')"
      />
    </ProListItem>

    <ProListItem :title="$t('pages.preference.multiplayer.labels.roomCode')">
      <Flex
        v-if="store.room"
        align="center"
        gap="small"
      >
        <strong class="text-lg font-mono">{{ store.room.roomCode }}</strong>
        <Button @click="copyRoomCode">
          {{ $t('pages.preference.multiplayer.buttons.copy') }}
        </Button>
      </Flex>
      <Flex
        v-else
        gap="small"
      >
        <Input
          v-model:value="roomCode"
          class="w-40 font-mono uppercase"
          :maxlength="8"
          :placeholder="$t('pages.preference.multiplayer.placeholders.roomCode')"
        />
        <Button
          :loading="loading"
          @click="connect(INVOKE_KEY.MULTIPLAYER_JOIN_ROOM)"
        >
          {{ $t('pages.preference.multiplayer.buttons.join') }}
        </Button>
        <Button
          :loading="loading"
          type="primary"
          @click="connect(INVOKE_KEY.MULTIPLAYER_CREATE_ROOM)"
        >
          {{ $t('pages.preference.multiplayer.buttons.create') }}
        </Button>
      </Flex>
    </ProListItem>
  </ProList>

  <ProList
    v-if="!store.room"
    :title="$t('pages.preference.multiplayer.labels.availableRooms')"
  >
    <Flex justify="end">
      <Button
        :disabled="loading"
        :loading="roomsLoading"
        @click="refreshRooms"
      >
        {{ $t('pages.preference.multiplayer.buttons.refresh') }}
      </Button>
    </Flex>

    <Alert
      v-if="roomsError"
      :message="$t(`pages.preference.multiplayer.errors.${roomsError.code}`, roomsError.message)"
      show-icon
      type="warning"
    />

    <Spin :spinning="roomsLoading">
      <Empty
        v-if="!rooms.length"
        :description="$t('pages.preference.multiplayer.hints.noRooms')"
        :image="Empty.PRESENTED_IMAGE_SIMPLE"
      />
      <template v-else>
        <ProListItem
          v-for="room in rooms"
          :key="room.roomCode"
          :description="$t('pages.preference.multiplayer.hints.roomSummary', { code: room.roomCode, count: room.playerCount, max: room.maxPlayers })"
          :title="room.roomName"
        >
          <Button
            :disabled="loading || isRoomFull(room)"
            :loading="loading && normalizeRoomCode(roomCode) === room.roomCode"
            @click="joinListedRoom(room)"
          >
            {{ isRoomFull(room) ? $t('pages.preference.multiplayer.buttons.full') : $t('pages.preference.multiplayer.buttons.join') }}
          </Button>
        </ProListItem>
      </template>
    </Spin>
  </ProList>

  <ProList
    v-if="store.room"
    :title="$t('pages.preference.multiplayer.labels.room')"
  >
    <ProListItem :title="$t('pages.preference.multiplayer.labels.roomName')">
      <strong>{{ store.room.roomName }}</strong>
    </ProListItem>

    <ProListItem :title="$t('pages.preference.multiplayer.labels.streamMode')">
      <Tag color="blue">
        {{ store.room.policy.streamMode }}
      </Tag>
    </ProListItem>

    <ProListItem :title="$t('pages.preference.multiplayer.labels.members')">
      <Flex
        gap="small"
        wrap="wrap"
      >
        <Tag
          v-for="player in store.players"
          :key="player.playerId"
          :color="player.online ? 'green' : 'default'"
        >
          <span>{{ player.name }}{{ player.playerId === store.room.self.playerId ? ` (${$t('pages.preference.multiplayer.labels.you')})` : '' }}</span>
          <span class="ml-1 font-mono opacity-70">· {{ player.playerId }}</span>
        </Tag>
      </Flex>
    </ProListItem>

    <ProListItem :title="$t('pages.preference.multiplayer.labels.networkLatency')">
      <Flex
        gap="small"
        wrap="wrap"
      >
        <Tag
          v-for="player in store.players"
          :key="player.playerId"
          :color="getLatencyTagColor(store.memberLatencies[player.playerId], player.online)"
        >
          <span>{{ player.name }}</span>
          <span class="ml-1 font-mono opacity-70">· {{ latencyText(player.playerId, player.online) }}</span>
        </Tag>
      </Flex>
    </ProListItem>

    <ProListItem :title="$t('pages.preference.multiplayer.labels.actions')">
      <Button
        danger
        @click="leaveRoom"
      >
        {{ $t('pages.preference.multiplayer.buttons.leave') }}
      </Button>
    </ProListItem>
  </ProList>
</template>
