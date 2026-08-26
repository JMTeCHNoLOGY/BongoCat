import { describe, expect, it } from 'vitest'

import type { Model } from '@/stores/model'
import type { RoomJoined } from '@/types/multiplayer'

import { generatePlayerName, normalizeRoomCode, reduceRoomMessage, resolveSkinModel, validateMultiplayerEndpoint } from './multiplayer'

const models: Model[] = [
  { id: 'default', skinId: 'builtin:keyboard:v1', path: '/keyboard', mode: 'keyboard', isPreset: true },
  { id: 'custom', skinId: 'sha256:custom', path: '/custom', mode: 'keyboard', isPreset: false },
]

function room(): RoomJoined {
  const self = { playerId: '1', name: 'One', skinId: 'skin', mode: 'standard' as const, order: 1, online: true }
  return {
    roomCode: 'ABC12345',
    self,
    players: [self],
    resumeToken: 'token',
    policy: { streamMode: 'raw', maxPlayers: 8, maxEventsPerSecond: 512, continuousHz: 20, maxMessageBytes: 16384, snapshotIntervalMs: 1000 },
  }
}

describe('multiplayer helpers', () => {
  it('generates a localized deterministic name', () => {
    expect(generatePlayerName('en-US', () => 0)).toBe('HappyCat100')
  })

  it('normalizes room codes and validates endpoints', () => {
    expect(normalizeRoomCode(' ab-c 1234 ')).toBe('ABC1234')
    expect(validateMultiplayerEndpoint('ws://127.0.0.1:8080/v1/ws')).toBe(true)
    expect(validateMultiplayerEndpoint('ws://example.com/v1/ws')).toBe(false)
    expect(validateMultiplayerEndpoint('wss://example.com/v1/ws')).toBe(true)
  })

  it('uses an installed skin and otherwise falls back by mode', () => {
    expect(resolveSkinModel(models, 'sha256:custom', 'keyboard')?.id).toBe('custom')
    expect(resolveSkinModel(models, 'missing', 'keyboard')?.id).toBe('default')
  })

  it('applies member lifecycle messages without duplicates', () => {
    const joined = reduceRoomMessage(room(), {
      v: 'v1',
      type: 'member_joined',
      payload: { player: { playerId: '2', name: 'Two', skinId: 'skin', mode: 'keyboard', order: 2, online: true } },
    })
    expect(joined.players.map(player => player.playerId)).toEqual(['1', '2'])
    const offline = reduceRoomMessage(joined, {
      v: 'v1',
      type: 'member_updated',
      payload: { player: { ...joined.players[1], online: false } },
    })
    expect(offline.players).toHaveLength(2)
    expect(offline.players[1].online).toBe(false)
    expect(reduceRoomMessage(offline, { v: 'v1', type: 'member_left', payload: { playerId: '2' } }).players).toHaveLength(1)
  })
})
