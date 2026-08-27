import type { ModelMode } from '@/stores/model'

export type MultiplayerState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting'

export interface RoomPolicy {
  streamMode: 'raw' | 'limited'
  maxPlayers: number
  maxEventsPerSecond: number
  continuousHz: number
  maxMessageBytes: number
  snapshotIntervalMs: number
}

export interface PlayerProfile {
  playerId: string
  name: string
  skinId: string
  mode: ModelMode
  order: number
  online: boolean
}

export interface RoomJoined {
  roomCode: string
  self: PlayerProfile
  players: PlayerProfile[]
  resumeToken: string
  policy: RoomPolicy
}

export interface ProtocolError {
  code: string
  message: string
}

export interface MultiplayerStatus {
  state: MultiplayerState
  room?: RoomJoined
  error?: ProtocolError
}

export interface DisplayBounds {
  x: number
  y: number
  width: number
  height: number
}

export interface InputEvent {
  sequence: number
  clientTimeMs: number
  kind: string
  value: unknown
  bounds?: DisplayBounds
}

export interface CursorState {
  x: number
  y: number
  bounds?: DisplayBounds
}

export interface InputSnapshot {
  sequence: number
  clientTimeMs: number
  pressedKeys: string[]
  mouseButtons: string[]
  cursor?: CursorState
  gamepad: Record<string, number>
}

export interface ProtocolMessage<T = unknown> {
  v: 'v1'
  type: string
  requestId?: string
  payload: T
}

export interface InputPayload {
  playerId: string
  event: InputEvent
}

export interface SnapshotPayload {
  playerId: string
  snapshot: InputSnapshot
}

export interface MemberLatencyPayload {
  playerId: string
  latencyMs: number | null
}
