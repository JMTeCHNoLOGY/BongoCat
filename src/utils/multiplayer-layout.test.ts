import { describe, expect, it } from 'vitest'

import {
  calculateAvatarScale,
  clampAvatarPosition,
  getAvatarCell,
  getAvatarMaximumScale,
  getAvatarRowCounts,
  getDefaultAvatarPosition,
  normalizeAvatarPosition,
  restoreAvatarPosition,
} from './multiplayer-layout'

describe('multiplayer layout', () => {
  it.each([
    { scalePercent: 50, pixelRatio: 2, expected: 0.25 },
    { scalePercent: 100, pixelRatio: 2, expected: 0.5 },
    { scalePercent: 200, pixelRatio: 2, expected: 1 },
    { scalePercent: 100, pixelRatio: 1, expected: 1 },
  ])('converts $scalePercent% at DPR $pixelRatio to a display scale', ({ scalePercent, pixelRatio, expected }) => {
    expect(getAvatarMaximumScale(scalePercent, pixelRatio)).toBe(expected)
  })

  it('does not enlarge an avatar beyond the single-window size', () => {
    expect(calculateAvatarScale({
      width: 2560,
      height: 1440,
      modelWidth: 612,
      modelHeight: 354,
      labelHeight: 8,
      maximumScale: getAvatarMaximumScale(100, 2),
    })).toBe(0.5)
  })

  it('still shrinks an avatar when its cell is smaller than the preferred size', () => {
    const scale = calculateAvatarScale({
      width: 200,
      height: 120,
      modelWidth: 612,
      modelHeight: 354,
      labelHeight: 34,
      maximumScale: 1,
    })

    expect(scale).toBeCloseTo((86 * 0.94) / 354)
    expect(scale).toBeLessThan(1)
  })

  it('returns a finite minimum scale for an invalid model size', () => {
    expect(calculateAvatarScale({
      width: 1920,
      height: 1080,
      modelWidth: 0,
      modelHeight: 0,
      labelHeight: 8,
      maximumScale: 1,
    })).toBe(0.01)
  })

  it('places one avatar at the bottom-right instead of the screen centre', () => {
    const point = getDefaultAvatarPosition({
      index: 0,
      count: 1,
      viewportWidth: 1920,
      viewportHeight: 1080,
      avatarWidth: 306,
      avatarHeight: 177,
    })

    expect(point).toEqual({ x: 1755, y: 979.5 })
    expect(point).not.toEqual({ x: 960, y: 540 })
  })

  it.each([
    { count: 5, rows: [5] },
    { count: 8, rows: [4, 4] },
  ])('creates bounded, non-overlapping cells for $count avatars', ({ count, rows }) => {
    expect(getAvatarRowCounts(count)).toEqual(rows)
    const cells = Array.from({ length: count }, (_, index) => getAvatarCell(index, count, 1920, 1080))

    expect(cells).toHaveLength(count)
    for (const cell of cells) {
      expect(cell.x).toBeGreaterThanOrEqual(0)
      expect(cell.y).toBeGreaterThanOrEqual(0)
      expect(cell.x + cell.width).toBeLessThanOrEqual(1920)
      expect(cell.y + cell.height).toBeLessThanOrEqual(1080)
    }
    for (const [index, cell] of cells.entries()) {
      for (const other of cells.slice(index + 1)) {
        const overlaps = cell.x < other.x + other.width
          && cell.x + cell.width > other.x
          && cell.y < other.y + other.height
          && cell.y + cell.height > other.y
        expect(overlaps).toBe(false)
      }
    }
  })

  it('keeps a dragged avatar and its label inside the viewport', () => {
    expect(clampAvatarPosition({
      point: { x: -100, y: 2000 },
      viewportWidth: 1920,
      viewportHeight: 1080,
      avatarWidth: 306,
      avatarHeight: 177,
      labelHeight: 48,
    })).toEqual({ x: 165, y: 979.5 })
  })

  it('restores a normalized position after the viewport size changes', () => {
    const normalized = normalizeAvatarPosition({ x: 1440, y: 810 }, 1920, 1080)

    expect(normalized).toEqual({ x: 0.75, y: 0.75 })
    expect(restoreAvatarPosition(normalized, 2560, 1440)).toEqual({ x: 1920, y: 1080 })
  })
})
