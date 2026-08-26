import { convertFileSrc } from '@tauri-apps/api/core'
import { exists, readDir, readTextFile } from '@tauri-apps/plugin-fs'
import { Config, CubismSetting, Live2DSprite } from 'easy-live2d'
import JSON5 from 'json5'
import { Application, Container, Ticker } from 'pixi.js'

import type { Model } from '@/stores/model'
import type { DisplayBounds, InputEvent, InputSnapshot, PlayerProfile } from '@/types/multiplayer'
import type { Point } from '@/utils/multiplayer-layout'

import { isImage } from '@/utils/is'
import {
  calculateAvatarScale,
  clampAvatarPosition,
  getAvatarCell,
  getDefaultAvatarPosition,
  normalizeAvatarPosition,
  restoreAvatarPosition,
} from '@/utils/multiplayer-layout'
import { join } from '@/utils/path'

Config.MouseFollow = false

const LABEL_HEIGHT = 48
const LABEL_GAP = 8
const LABEL_MAX_WIDTH = 300

interface GamepadValue { name: string, value: number }
interface CursorPoint extends Point { bounds?: DisplayBounds }

interface TrackedInputState {
  sequence: number
  clientTimeMs: number
  pressedKeys: Set<string>
  mouseButtons: Set<string>
  cursor?: CursorPoint
  gamepad: Record<string, number>
}

interface DragState {
  avatar: AvatarInstance
  offset: Point
}

interface SceneLayers {
  backgrounds: HTMLElement
  overlays: HTMLElement
}

function createFrameElement() {
  const element = document.createElement('div')
  Object.assign(element.style, {
    position: 'absolute',
    left: '0',
    top: '0',
    pointerEvents: 'none',
    transformOrigin: 'top left',
    willChange: 'transform',
  })
  return element
}

function createFrameImage(source: string) {
  const image = document.createElement('img')
  image.src = source
  image.draggable = false
  Object.assign(image.style, {
    position: 'absolute',
    inset: '0',
    width: '100%',
    height: '100%',
    pointerEvents: 'none',
  })
  return image
}

function createTrackedInputState(): TrackedInputState {
  return {
    sequence: 0,
    clientTimeMs: 0,
    pressedKeys: new Set(),
    mouseButtons: new Set(),
    gamepad: {},
  }
}

function inputSnapshot(state: TrackedInputState): InputSnapshot {
  return {
    sequence: state.sequence,
    clientTimeMs: state.clientTimeMs,
    pressedKeys: [...state.pressedKeys],
    mouseButtons: [...state.mouseButtons],
    cursor: state.cursor,
    gamepad: { ...state.gamepad },
  }
}

function trackInputEvent(state: TrackedInputState, event: InputEvent) {
  if (event.sequence <= state.sequence) return false
  state.sequence = event.sequence
  state.clientTimeMs = event.clientTimeMs

  switch (event.kind) {
    case 'KeyboardPress':
      state.pressedKeys.add(String(event.value))
      break
    case 'KeyboardRelease':
      state.pressedKeys.delete(String(event.value))
      break
    case 'MousePress':
      state.mouseButtons.add(String(event.value))
      break
    case 'MouseRelease':
      state.mouseButtons.delete(String(event.value))
      break
    case 'MouseMove': {
      const point = event.value as Point
      state.cursor = { ...point, bounds: event.bounds }
      break
    }
    case 'ButtonChanged':
    case 'AxisChanged': {
      const value = event.value as GamepadValue
      state.gamepad[value.name] = value.value
      break
    }
    case 'ContinuousState': {
      const value = event.value as { cursor?: CursorPoint, gamepad?: Record<string, number> }
      if (value.cursor) state.cursor = { ...value.cursor, bounds: value.cursor.bounds ?? event.bounds }
      if (value.gamepad) state.gamepad = { ...value.gamepad }
      break
    }
  }
  return true
}

function trackInputSnapshot(state: TrackedInputState, snapshot: InputSnapshot) {
  if (snapshot.sequence <= state.sequence) return false
  state.sequence = snapshot.sequence
  state.clientTimeMs = snapshot.clientTimeMs
  state.pressedKeys = new Set(snapshot.pressedKeys)
  state.mouseButtons = new Set(snapshot.mouseButtons)
  state.cursor = snapshot.cursor ? { ...snapshot.cursor } : void 0
  state.gamepad = { ...snapshot.gamepad }
  return true
}

class AvatarInstance {
  readonly root = new Container()
  readonly content = new Container()
  readonly model: Live2DSprite
  readonly modelId: string
  order: number
  private profile: PlayerProfile
  private frameWidth = 1
  private frameHeight = 1
  private displayScale = 1
  private supportKeys = new Map<string, string>()
  private rawKeys = new Map<string, string>()
  private resolvedKeyCounts = new Map<string, number>()
  private visibleKeys = new Map<string, string>()
  private overlays = new Map<string, HTMLImageElement>()
  private lastSequence = 0
  private gamepad: Record<string, number> = {}
  private readonly backgroundElement = createFrameElement()
  private readonly overlayElement = createFrameElement()
  private readonly labelElement = document.createElement('div')
  private readonly nameElement = document.createElement('div')
  private readonly idElement = document.createElement('div')

  private constructor(model: Live2DSprite, profile: PlayerProfile, local: boolean, modelId: string, layers: SceneLayers) {
    this.model = model
    this.modelId = modelId
    this.profile = profile
    this.order = profile.order

    Object.assign(this.labelElement.style, {
      position: 'absolute',
      boxSizing: 'border-box',
      maxWidth: `${LABEL_MAX_WIDTH}px`,
      height: `${LABEL_HEIGHT}px`,
      padding: '4px 10px',
      border: '1px solid rgba(255, 255, 255, 0.35)',
      borderRadius: '9px',
      background: 'rgba(0, 0, 0, 0.68)',
      color: '#fff',
      fontFamily: 'sans-serif',
      lineHeight: '19px',
      textAlign: 'center',
      whiteSpace: 'nowrap',
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      pointerEvents: 'none',
      transform: 'translate(-50%, -100%)',
      willChange: 'left, top',
    })
    Object.assign(this.nameElement.style, { fontSize: '15px', fontWeight: '700', overflow: 'hidden', textOverflow: 'ellipsis' })
    Object.assign(this.idElement.style, { fontFamily: 'monospace', fontSize: '12px', opacity: '0.88' })
    this.labelElement.append(this.nameElement, this.idElement)

    this.content.addChild(model)
    this.root.addChild(this.content)
    layers.backgrounds.appendChild(this.backgroundElement)
    layers.overlays.append(this.overlayElement, this.labelElement)
    this.setProfile(profile, local)
  }

  static async create(modelInfo: Model, profile: PlayerProfile, local: boolean, stage: Container, layers: SceneLayers) {
    const files = await readDir(modelInfo.path)
    const modelFile = files.find(file => file.name.endsWith('.model3.json'))
    if (!modelFile) throw new Error(`model configuration not found: ${modelInfo.path}`)

    const modelJSON = JSON5.parse(await readTextFile(join(modelInfo.path, modelFile.name)))
    const modelSetting = new CubismSetting({ modelJSON })
    modelSetting.redirectPath(({ file }) => convertFileSrc(join(modelInfo.path, file)))
    const model = new Live2DSprite({ modelSetting, ticker: Ticker.shared })
    const avatar = new AvatarInstance(model, profile, local, modelInfo.id, layers)
    stage.addChild(avatar.root)
    try {
      await model.ready
      model.anchor.set(0.5)
      await avatar.loadResources(modelInfo.path)
      return avatar
    } catch (error) {
      stage.removeChild(avatar.root)
      avatar.destroy()
      throw error
    }
  }

  private async loadResources(path: string) {
    this.frameWidth = Math.max(1, this.model.width)
    this.frameHeight = Math.max(1, this.model.height)
    this.syncFrameSize()

    const resources = join(path, 'resources')
    const background = join(resources, 'background.png')
    if (await exists(background)) {
      this.backgroundElement.appendChild(createFrameImage(convertFileSrc(background)))
    }

    for (const group of ['left-keys', 'right-keys']) {
      const directory = join(resources, group)
      const files = await readDir(directory).catch(() => [])
      for (const file of files.filter(file => isImage(file.name))) {
        this.supportKeys.set(file.name.split('.')[0], join(directory, file.name))
      }
    }
  }

  get playerId() {
    return this.profile.playerId
  }

  get playerProfile() {
    return this.profile
  }

  get displayWidth() {
    return this.frameWidth * this.displayScale
  }

  get displayHeight() {
    return this.frameHeight * this.displayScale
  }

  setProfile(profile: PlayerProfile, local: boolean) {
    this.profile = profile
    this.order = profile.order
    this.nameElement.textContent = profile.name
    this.idElement.textContent = `ID ${profile.playerId.slice(0, 8)}`
    this.labelElement.title = `${profile.name} · ${profile.playerId}`
    this.labelElement.style.borderColor = local ? 'rgba(64, 150, 255, 0.9)' : 'rgba(255, 255, 255, 0.35)'
    this.root.alpha = profile.online ? 1 : 0.45
    const opacity = String(this.root.alpha)
    this.backgroundElement.style.opacity = opacity
    this.overlayElement.style.opacity = opacity
    this.labelElement.style.opacity = opacity
    this.setZIndex(profile.order)
  }

  layout(width: number, height: number, maximumScale: number) {
    this.displayScale = calculateAvatarScale({
      width,
      height,
      modelWidth: this.frameWidth,
      modelHeight: this.frameHeight,
      labelHeight: LABEL_HEIGHT + LABEL_GAP,
      maximumScale,
    })
    this.content.scale.set(this.displayScale)
    this.syncDom()
  }

  place(point: Point, viewportWidth: number, viewportHeight: number) {
    const next = clampAvatarPosition({
      point,
      viewportWidth,
      viewportHeight,
      avatarWidth: this.displayWidth,
      avatarHeight: this.displayHeight,
      labelHeight: LABEL_HEIGHT + LABEL_GAP,
    })
    this.root.position.set(next.x, next.y)
    this.syncDom()
    return next
  }

  hitTest(point: Point) {
    const halfWidth = this.displayWidth / 2
    const halfHeight = this.displayHeight / 2
    return point.x >= this.root.x - halfWidth
      && point.x <= this.root.x + halfWidth
      && point.y >= this.root.y - halfHeight
      && point.y <= this.root.y + halfHeight
  }

  setZIndex(value: number) {
    this.root.zIndex = value
    const zIndex = String(value)
    this.backgroundElement.style.zIndex = zIndex
    this.overlayElement.style.zIndex = zIndex
    this.labelElement.style.zIndex = zIndex
  }

  private syncFrameSize() {
    const width = `${this.frameWidth}px`
    const height = `${this.frameHeight}px`
    for (const element of [this.backgroundElement, this.overlayElement]) {
      element.style.width = width
      element.style.height = height
    }
  }

  private syncDom() {
    const left = this.root.x - this.displayWidth / 2
    const top = this.root.y - this.displayHeight / 2
    const transform = `translate3d(${left}px, ${top}px, 0) scale(${this.displayScale})`
    this.backgroundElement.style.transform = transform
    this.overlayElement.style.transform = transform

    const labelHalfWidth = Math.min(LABEL_MAX_WIDTH / 2, Math.max(0, innerWidth / 2 - 12))
    const labelX = Math.max(labelHalfWidth + 12, Math.min(innerWidth - labelHalfWidth - 12, this.root.x))
    this.labelElement.style.left = `${labelX}px`
    this.labelElement.style.top = `${this.root.y - this.displayHeight / 2 - LABEL_GAP}px`
  }

  applyEvent(event: InputEvent) {
    if (event.sequence <= this.lastSequence) return
    this.lastSequence = event.sequence

    switch (event.kind) {
      case 'KeyboardPress': return this.press(String(event.value))
      case 'KeyboardRelease': return this.release(String(event.value))
      case 'MousePress': return this.setMouse(String(event.value), true)
      case 'MouseRelease': return this.setMouse(String(event.value), false)
      case 'MouseMove': return this.setCursor(event.value as Point, event.bounds)
      case 'ButtonChanged':
      case 'AxisChanged': return this.setGamepad(event.value as GamepadValue)
      case 'ContinuousState': {
        const value = event.value as { cursor?: CursorPoint, gamepad?: Record<string, number> }
        if (value.cursor) this.setCursor(value.cursor, value.cursor.bounds ?? event.bounds)
        if (value.gamepad) this.setGamepadState(value.gamepad)
      }
    }
  }

  applySnapshot(snapshot: InputSnapshot) {
    if (snapshot.sequence <= this.lastSequence) return
    this.lastSequence = snapshot.sequence

    for (const key of [...this.rawKeys.keys()]) {
      if (!snapshot.pressedKeys.includes(key)) this.release(key)
    }
    for (const key of snapshot.pressedKeys) this.press(key)
    this.setMouse('Left', snapshot.mouseButtons.includes('Left'))
    this.setMouse('Right', snapshot.mouseButtons.includes('Right'))
    if (snapshot.cursor) this.setCursor(snapshot.cursor, snapshot.cursor.bounds)
    this.setGamepadState(snapshot.gamepad)
  }

  private resolveSupportedKey(key: string) {
    if (this.supportKeys.has(key)) return key
    if (key.startsWith('F') && this.supportKeys.has('Fn')) return 'Fn'

    for (const modifier of ['Meta', 'Shift', 'Alt', 'Control']) {
      if (key.startsWith(modifier) && this.supportKeys.has(modifier)) return modifier
    }
    return key
  }

  private keyGroup(key: string) {
    const path = this.supportKeys.get(key) ?? ''
    if (path.includes('left-keys')) return 'left'
    if (path.includes('right-keys')) return 'right'
    return `key:${key}`
  }

  private press(rawKey: string) {
    if (this.rawKeys.has(rawKey)) return
    const key = this.resolveSupportedKey(rawKey)
    const path = this.supportKeys.get(key)
    if (!path) return

    this.rawKeys.set(rawKey, key)
    const count = (this.resolvedKeyCounts.get(key) ?? 0) + 1
    this.resolvedKeyCounts.set(key, count)
    if (count > 1) return

    const group = this.keyGroup(key)
    const previous = this.visibleKeys.get(group)
    if (previous && previous !== key) this.hideOverlay(previous)
    this.showOverlay(key, path)
    this.visibleKeys.set(group, key)
    this.updateHands()
  }

  private release(rawKey: string) {
    const key = this.rawKeys.get(rawKey)
    if (!key) return
    this.rawKeys.delete(rawKey)
    const count = Math.max(0, (this.resolvedKeyCounts.get(key) ?? 1) - 1)
    if (count > 0) {
      this.resolvedKeyCounts.set(key, count)
      return
    }

    this.resolvedKeyCounts.delete(key)
    const group = this.keyGroup(key)
    if (this.visibleKeys.get(group) !== key) return
    this.hideOverlay(key)
    this.visibleKeys.delete(group)

    const fallback = [...this.resolvedKeyCounts.keys()].find(candidate => this.keyGroup(candidate) === group)
    const fallbackPath = fallback ? this.supportKeys.get(fallback) : void 0
    if (fallback && fallbackPath) {
      this.showOverlay(fallback, fallbackPath)
      this.visibleKeys.set(group, fallback)
    }
    this.updateHands()
  }

  private showOverlay(key: string, path: string) {
    if (this.overlays.has(key)) return
    const image = createFrameImage(convertFileSrc(path))
    this.overlays.set(key, image)
    this.overlayElement.appendChild(image)
  }

  private hideOverlay(key: string) {
    this.overlays.get(key)?.remove()
    this.overlays.delete(key)
  }

  private updateHands() {
    this.setParameter('CatParamLeftHandDown', this.visibleKeys.has('left'))
    this.setParameter('CatParamRightHandDown', this.visibleKeys.has('right'))
  }

  private setMouse(button: string, pressed: boolean) {
    this.setParameter(button === 'Left' ? 'ParamMouseLeftDown' : 'ParamMouseRightDown', pressed)
  }

  private setCursor(point: Point, bounds?: DisplayBounds) {
    if (!bounds || !bounds.width || !bounds.height) return
    const xRatio = Math.max(0, Math.min(1, (point.x - bounds.x) / bounds.width))
    const yRatio = Math.max(0, Math.min(1, (point.y - bounds.y) / bounds.height))
    for (const id of ['ParamMouseX', 'ParamMouseY', 'ParamAngleX', 'ParamAngleY', 'ParamAngleZ', 'ParamEyeBallX', 'ParamEyeBallY']) {
      const range = this.model.getParameterValueRangeById(id)
      if (!range) continue
      const { min, max } = range
      if (id.endsWith('Z')) this.setParameter(id, (1 - 2 * xRatio) * (1 - 2 * yRatio) * min)
      else this.setParameter(id, max - (id.endsWith('X') ? xRatio : yRatio) * (max - min))
    }
  }

  private setGamepad(value: GamepadValue) {
    this.gamepad[value.name] = value.value
    this.applyGamepad(value.name, value.value)
  }

  private setGamepadState(state: Record<string, number>) {
    const names = new Set([...Object.keys(this.gamepad), ...Object.keys(state)])
    this.gamepad = { ...state }
    for (const name of names) this.applyGamepad(name, state[name] ?? 0)
  }

  private applyGamepad(name: string, value: number) {
    const axes: Record<string, string> = {
      LeftStickX: 'CatParamStickLX',
      LeftStickY: 'CatParamStickLY',
      RightStickX: 'CatParamStickRX',
      RightStickY: 'CatParamStickRY',
    }
    if (axes[name]) {
      const range = this.model.getParameterValueRangeById(axes[name])
      if (range) this.setParameter(axes[name], Math.max(range.min, value * range.max))
    } else if (name === 'LeftThumb' || name === 'RightThumb') {
      this.setParameter(name === 'LeftThumb' ? 'CatParamStickLeftDown' : 'CatParamStickRightDown', value !== 0)
    } else {
      if (value > 0) this.press(name)
      else this.release(name)
    }
    const leftActive = Boolean(this.gamepad.LeftStickX || this.gamepad.LeftStickY || this.gamepad.LeftThumb)
    const rightActive = Boolean(this.gamepad.RightStickX || this.gamepad.RightStickY || this.gamepad.RightThumb)
    this.setParameter('CatParamStickShowLeftHand', leftActive)
    this.setParameter('CatParamStickShowRightHand', rightActive)
  }

  private setParameter(id: string, value: number | boolean) {
    this.model.setParameterValueById(id, Number(value))
  }

  retire() {
    this.backgroundElement.remove()
    this.overlayElement.remove()
    this.labelElement.remove()
  }

  destroy() {
    this.retire()
    this.root.destroy({ children: true })
  }
}

export class MultiplayerScene {
  private app = new Application()
  private avatars = new Map<string, AvatarInstance>()
  private revisions = new Map<string, number>()
  private retired: AvatarInstance[] = []
  private inputStates = new Map<string, TrackedInputState>()
  private positions = new Map<string, Point>()
  private dragging?: DragState
  private layers?: SceneLayers

  constructor(
    private readonly getMaximumScale = () => 1,
    private readonly getSavedPosition: (profile: PlayerProfile) => Point | undefined = () => void 0,
    private readonly savePosition: (profile: PlayerProfile, point: Point) => void = () => {},
  ) {}

  async init(canvas: HTMLCanvasElement, backgrounds: HTMLElement, overlays: HTMLElement) {
    this.layers = { backgrounds, overlays }
    this.app.stage.sortableChildren = true
    await this.app.init({ view: canvas, resizeTo: window, backgroundAlpha: 0, autoDensity: true, resolution: devicePixelRatio })
  }

  async upsert(profile: PlayerProfile, model: Model, local: boolean) {
    if (!this.layers) return
    const current = this.avatars.get(profile.playerId)
    if (current?.modelId === model.id) {
      current.setProfile(profile, local)
      this.layout()
      return
    }

    const revision = (this.revisions.get(profile.playerId) ?? 0) + 1
    this.revisions.set(profile.playerId, revision)
    const avatar = await AvatarInstance.create(model, profile, local, this.app.stage, this.layers)
    if (this.revisions.get(profile.playerId) !== revision) {
      this.app.stage.removeChild(avatar.root)
      avatar.retire()
      this.retired.push(avatar)
      return
    }

    if (current) {
      this.app.stage.removeChild(current.root)
      current.retire()
      this.retired.push(current)
    }
    this.avatars.set(profile.playerId, avatar)
    this.layout()

    const state = this.inputStates.get(profile.playerId)
    if (state?.sequence) avatar.applySnapshot(inputSnapshot(state))
  }

  remove(playerId: string) {
    this.revisions.set(playerId, (this.revisions.get(playerId) ?? 0) + 1)
    const avatar = this.avatars.get(playerId)
    if (!avatar) return
    if (this.dragging?.avatar === avatar) this.dragging = void 0
    this.app.stage.removeChild(avatar.root)
    avatar.retire()
    this.retired.push(avatar)
    this.avatars.delete(playerId)
    this.inputStates.delete(playerId)
    this.positions.delete(playerId)
    this.layout()
  }

  applyEvent(playerId: string, event: InputEvent) {
    const state = this.inputStates.get(playerId) ?? createTrackedInputState()
    this.inputStates.set(playerId, state)
    if (!trackInputEvent(state, event)) return
    return this.avatars.get(playerId)?.applyEvent(event)
  }

  applySnapshot(playerId: string, snapshot: InputSnapshot) {
    const state = this.inputStates.get(playerId) ?? createTrackedInputState()
    this.inputStates.set(playerId, state)
    if (!trackInputSnapshot(state, snapshot)) return
    return this.avatars.get(playerId)?.applySnapshot(snapshot)
  }

  applyLocal(playerId: string, kind: string, value: unknown, bounds?: DisplayBounds) {
    const state = this.inputStates.get(playerId) ?? createTrackedInputState()
    const event = {
      sequence: state.sequence + 1,
      clientTimeMs: Date.now(),
      kind,
      value,
      bounds,
    } as InputEvent
    return this.applyEvent(playerId, event)
  }

  playerIds() {
    return this.avatars.keys()
  }

  pointerMove(point: Point) {
    if (this.dragging) {
      const { avatar, offset } = this.dragging
      const position = avatar.place({ x: point.x + offset.x, y: point.y + offset.y }, innerWidth, innerHeight)
      this.positions.set(avatar.playerId, normalizeAvatarPosition(position, innerWidth, innerHeight))
      return true
    }
    return Boolean(this.hitTest(point))
  }

  pointerDown(point: Point, button: string) {
    if (button !== 'Left') return Boolean(this.hitTest(point))
    const avatar = this.hitTest(point)
    if (!avatar) return false

    this.dragging = {
      avatar,
      offset: { x: avatar.root.x - point.x, y: avatar.root.y - point.y },
    }
    avatar.setZIndex(10_000)
    return true
  }

  pointerUp(point: Point, button: string) {
    if (button === 'Left' && this.dragging) {
      const { avatar } = this.dragging
      avatar.setZIndex(avatar.order)
      const position = this.positions.get(avatar.playerId)
      if (position) this.savePosition(avatar.playerProfile, position)
      this.dragging = void 0
    }
    return Boolean(this.hitTest(point))
  }

  cancelDrag() {
    if (this.dragging) this.dragging.avatar.setZIndex(this.dragging.avatar.order)
    this.dragging = void 0
  }

  private hitTest(point: Point) {
    return [...this.avatars.values()]
      .sort((left, right) => right.root.zIndex - left.root.zIndex)
      .find(avatar => avatar.hitTest(point))
  }

  clear() {
    this.cancelDrag()
    for (const avatar of this.avatars.values()) {
      this.app.stage.removeChild(avatar.root)
      this.retired.push(avatar)
    }
    this.avatars.clear()
    this.inputStates.clear()
    this.positions.clear()
    for (const avatar of this.retired) avatar.destroy()
    this.retired = []
  }

  layout() {
    const avatars = [...this.avatars.values()].sort((left, right) => left.order - right.order)
    const count = avatars.length
    if (!count) return
    const maximumScale = this.getMaximumScale()

    avatars.forEach((avatar, index) => {
      const cell = getAvatarCell(index, count, innerWidth, innerHeight)
      avatar.layout(cell.width, cell.height, maximumScale)
      avatar.setZIndex(avatar.order)

      let normalized = this.positions.get(avatar.playerId)
      if (!normalized) {
        normalized = this.getSavedPosition(avatar.playerProfile)
        if (normalized) this.positions.set(avatar.playerId, normalized)
      }

      const point = normalized
        ? restoreAvatarPosition(normalized, innerWidth, innerHeight)
        : getDefaultAvatarPosition({
            index,
            count,
            viewportWidth: innerWidth,
            viewportHeight: innerHeight,
            avatarWidth: avatar.displayWidth,
            avatarHeight: avatar.displayHeight,
          })
      avatar.place(point, innerWidth, innerHeight)
    })
  }

  destroy() {
    this.clear()
    this.app.destroy()
  }
}
