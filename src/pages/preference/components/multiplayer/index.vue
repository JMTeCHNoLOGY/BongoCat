<script setup lang="ts">
import { invoke } from '@tauri-apps/api/core'
import { writeText } from '@tauri-apps/plugin-clipboard-manager'
import { Alert, Button, Flex, Input, message, Tag } from 'ant-design-vue'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import type { ProtocolError, RoomJoined } from '@/types/multiplayer'

import ProListItem from '@/components/pro-list-item/index.vue'
import ProList from '@/components/pro-list/index.vue'
import { INVOKE_KEY } from '@/constants'
import { useGeneralStore } from '@/stores/general'
import { useModelStore } from '@/stores/model'
import { useMultiplayerStore } from '@/stores/multiplayer'
import { generatePlayerName, getLatencyTagColor, normalizeRoomCode, validateMultiplayerEndpoint } from '@/utils/multiplayer'

const store = useMultiplayerStore()
const modelStore = useModelStore()
const generalStore = useGeneralStore()
const roomCode = ref('')
const loading = ref(false)
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

async function connect(command: typeof INVOKE_KEY.MULTIPLAYER_CREATE_ROOM | typeof INVOKE_KEY.MULTIPLAYER_JOIN_ROOM) {
  if (!validateMultiplayerEndpoint(store.endpoint)) {
    return message.error(t('pages.preference.multiplayer.hints.invalidEndpoint'))
  }
  const model = modelStore.currentModel
  if (!model) return

  loading.value = true
  store.lastError = void 0
  const enteredName = store.name.trim()

  try {
    for (let attempt = 0; attempt < 5; attempt++) {
      const name = enteredName || generatePlayerName(generalStore.appearance.language)
      try {
        const joined = await invoke<RoomJoined>(command, {
          endpoint: store.endpoint.trim(),
          roomCode: normalizeRoomCode(roomCode.value),
          profile: { name, skinId: model.skinId, mode: model.mode },
        })
        store.name = name
        store.applyMessage({ v: 'v1', type: 'room_joined', payload: joined })
        roomCode.value = joined.roomCode
        return
      } catch (error) {
        const nextError = protocolError(error)
        if (enteredName || nextError.code !== 'NAME_TAKEN' || attempt === 4) throw nextError
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
    v-if="store.room"
    :title="$t('pages.preference.multiplayer.labels.room')"
  >
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
