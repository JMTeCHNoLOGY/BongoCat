const MIN_AVATAR_SCALE = 0.01
const AVATAR_SCREEN_PADDING = 12

interface AvatarScaleOptions {
  width: number
  height: number
  modelWidth: number
  modelHeight: number
  labelHeight: number
  maximumScale: number
}

export interface Point {
  x: number
  y: number
}

export interface AvatarCell {
  x: number
  y: number
  width: number
  height: number
}

interface DefaultAvatarPositionOptions {
  index: number
  count: number
  viewportWidth: number
  viewportHeight: number
  avatarWidth: number
  avatarHeight: number
  padding?: number
}

interface ClampAvatarPositionOptions {
  point: Point
  viewportWidth: number
  viewportHeight: number
  avatarWidth: number
  avatarHeight: number
  labelHeight: number
  padding?: number
}

export function getAvatarMaximumScale(scalePercent: number, pixelRatio: number) {
  const safePixelRatio = pixelRatio > 0 ? pixelRatio : 1

  return Math.max(MIN_AVATAR_SCALE, scalePercent / 100 / safePixelRatio)
}

export function calculateAvatarScale(options: AvatarScaleOptions) {
  const { width, height, modelWidth, modelHeight, labelHeight, maximumScale } = options
  if (modelWidth <= 0 || modelHeight <= 0) return MIN_AVATAR_SCALE

  const widthScale = (width * 0.92) / modelWidth
  const heightScale = (Math.max(0, height - labelHeight) * 0.94) / modelHeight

  return Math.max(MIN_AVATAR_SCALE, Math.min(widthScale, heightScale, maximumScale))
}

export function getAvatarRowCounts(count: number) {
  if (count <= 0) return []

  return count <= 5 ? [count] : [Math.ceil(count / 2), Math.floor(count / 2)]
}

export function getAvatarCell(index: number, count: number, viewportWidth: number, viewportHeight: number): AvatarCell {
  const rowCounts = getAvatarRowCounts(count)
  const rowHeight = viewportHeight / rowCounts.length
  let offset = 0

  for (const [row, columns] of rowCounts.entries()) {
    if (index < offset + columns) {
      const column = index - offset
      const width = viewportWidth / columns

      return { x: column * width, y: row * rowHeight, width, height: rowHeight }
    }
    offset += columns
  }

  return { x: 0, y: 0, width: viewportWidth, height: viewportHeight }
}

export function getDefaultAvatarPosition(options: DefaultAvatarPositionOptions): Point {
  const {
    index,
    count,
    viewportWidth,
    viewportHeight,
    avatarWidth,
    avatarHeight,
    padding = AVATAR_SCREEN_PADDING,
  } = options
  const cell = getAvatarCell(index, count, viewportWidth, viewportHeight)

  return {
    x: count === 1
      ? viewportWidth - avatarWidth / 2 - padding
      : cell.x + cell.width / 2,
    y: cell.y + cell.height - avatarHeight / 2 - padding,
  }
}

function clamp(value: number, minimum: number, maximum: number) {
  if (minimum > maximum) return (minimum + maximum) / 2

  return Math.max(minimum, Math.min(maximum, value))
}

export function clampAvatarPosition(options: ClampAvatarPositionOptions): Point {
  const {
    point,
    viewportWidth,
    viewportHeight,
    avatarWidth,
    avatarHeight,
    labelHeight,
    padding = AVATAR_SCREEN_PADDING,
  } = options
  const halfWidth = avatarWidth / 2
  const halfHeight = avatarHeight / 2

  return {
    x: clamp(point.x, halfWidth + padding, viewportWidth - halfWidth - padding),
    y: clamp(point.y, halfHeight + labelHeight + padding, viewportHeight - halfHeight - padding),
  }
}

export function normalizeAvatarPosition(point: Point, viewportWidth: number, viewportHeight: number): Point {
  return {
    x: viewportWidth > 0 ? point.x / viewportWidth : 0.5,
    y: viewportHeight > 0 ? point.y / viewportHeight : 0.5,
  }
}

export function restoreAvatarPosition(point: Point, viewportWidth: number, viewportHeight: number): Point {
  return {
    x: point.x * viewportWidth,
    y: point.y * viewportHeight,
  }
}
