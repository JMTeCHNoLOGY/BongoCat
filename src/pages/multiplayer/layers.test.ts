import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(resolve(process.cwd(), 'src/pages/multiplayer/index.vue'), 'utf8')

function openingTagFor(ref: string) {
  const marker = `ref="${ref}"`
  const markerIndex = source.indexOf(marker)
  if (markerIndex < 0) throw new Error(`missing multiplayer layer: ${ref}`)
  return source.slice(source.lastIndexOf('<', markerIndex), source.indexOf('>', markerIndex) + 1)
}

function zIndexFor(ref: string) {
  const match = openingTagFor(ref).match(/z-index:\s*(-?\d+)/)
  if (!match) throw new Error(`missing explicit z-index for multiplayer layer: ${ref}`)
  return Number(match[1])
}

describe('multiplayer DOM layer contract', () => {
  it('contains avatar backgrounds below the canvas and overlays above it', () => {
    expect(source).toMatch(/<template>\s*<div[^>]+isolation:\s*isolate/)
    for (const ref of ['backgrounds', 'canvas', 'overlays']) {
      expect(openingTagFor(ref)).toMatch(/\babsolute\b/)
    }
    expect(zIndexFor('backgrounds')).toBeLessThan(zIndexFor('canvas'))
    expect(zIndexFor('canvas')).toBeLessThan(zIndexFor('overlays'))
  })
})
