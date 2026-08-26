import type { Language } from '@/stores/general'
import type { Model, ModelMode } from '@/stores/model'
import type { PlayerProfile, ProtocolMessage, RoomJoined } from '@/types/multiplayer'

const WORDS: Record<string, { adjectives: string[], animals: string[] }> = {
  'zh-CN': { adjectives: ['快乐', '勇敢', '安静', '机灵', '软萌'], animals: ['橘猫', '熊猫', '水獭', '兔子', '狐狸'] },
  'zh-TW': { adjectives: ['快樂', '勇敢', '安靜', '機靈', '軟萌'], animals: ['橘貓', '熊貓', '水獺', '兔子', '狐狸'] },
  'vi-VN': { adjectives: ['Vui', 'Nhanh', 'Hiền', 'Dũng cảm', 'May mắn'], animals: ['Mèo', 'Gấu', 'Rái cá', 'Thỏ', 'Cáo'] },
  'pt-BR': { adjectives: ['Feliz', 'Veloz', 'Calmo', 'Valente', 'Sortudo'], animals: ['Gato', 'Panda', 'Lontra', 'Coelho', 'Raposa'] },
  'en-US': { adjectives: ['Happy', 'Swift', 'Quiet', 'Brave', 'Lucky'], animals: ['Cat', 'Panda', 'Otter', 'Rabbit', 'Fox'] },
}

export function generatePlayerName(language?: Language, random = Math.random) {
  const words = WORDS[language ?? 'en-US'] ?? WORDS['en-US']
  const adjective = words.adjectives[Math.floor(random() * words.adjectives.length)]
  const animal = words.animals[Math.floor(random() * words.animals.length)]
  const number = Math.floor(random() * 900 + 100)

  return `${adjective}${animal}${number}`
}

export function normalizeRoomCode(value: string) {
  return value.trim().toUpperCase().replace(/[ILOU\s-]/g, '')
}

export function validateMultiplayerEndpoint(value: string) {
  try {
    const endpoint = new URL(value)
    if (!['ws:', 'wss:'].includes(endpoint.protocol)) return false

    const local = ['127.0.0.1', 'localhost', '::1', '[::1]'].includes(endpoint.hostname)
    return endpoint.protocol === 'wss:' || local
  } catch {
    return false
  }
}

export function resolveSkinModel(models: Model[], skinId: string, mode: ModelMode) {
  return models.find(model => model.skinId === skinId)
    ?? models.find(model => model.isPreset && model.mode === mode)
    ?? models[0]
}

export function reduceRoomMessage(room: RoomJoined, message: ProtocolMessage) {
  if (message.type === 'member_joined' || message.type === 'member_updated') {
    const player = (message.payload as { player: PlayerProfile }).player
    return {
      ...room,
      players: [...room.players.filter(item => item.playerId !== player.playerId), player],
    }
  }
  if (message.type === 'member_left') {
    const { playerId } = message.payload as { playerId: string }
    return { ...room, players: room.players.filter(player => player.playerId !== playerId) }
  }
  return room
}
