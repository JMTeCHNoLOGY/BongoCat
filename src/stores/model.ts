import type { ExpressionInfo, MotionInfo } from 'easy-live2d'

import { invoke } from '@tauri-apps/api/core'
import { resolveResource } from '@tauri-apps/api/path'
import { filter, find } from 'es-toolkit/compat'
import { nanoid } from 'nanoid'
import { defineStore } from 'pinia'
import { reactive, ref } from 'vue'

import { INVOKE_KEY } from '@/constants'
import { resolveSkinModel } from '@/utils/multiplayer'
import { join } from '@/utils/path'

export type ModelMode = 'standard' | 'keyboard' | 'gamepad'

export interface Model {
  id: string
  skinId: string
  path: string
  mode: ModelMode
  isPreset: boolean
}

export const useModelStore = defineStore('model', () => {
  const modelReady = ref(true)
  const models = ref<Model[]>([])
  const currentModel = ref<Model>()
  const supportKeys = reactive<Record<string, string>>({})
  const pressedKeys = reactive<Record<string, string>>({})
  const currentMotions = ref<Array<[string, MotionInfo[]]>>([])
  const currentExpressions = ref<ExpressionInfo[]>([])
  const shortcuts = reactive<Record<string, string>>({})

  const init = async () => {
    const modelsPath = await resolveResource('assets/models')

    const nextModels = filter(models.value, { isPreset: false })
    const presetModels = filter(models.value, { isPreset: true })

    const modes: ModelMode[] = ['gamepad', 'keyboard', 'standard']

    for (const mode of modes) {
      const matched = find(presetModels, { mode })

      nextModels.unshift({
        id: matched?.id ?? nanoid(),
        skinId: `builtin:${mode}:v1`,
        mode,
        isPreset: true,
        path: join(modelsPath, mode),
      })
    }

    for (const model of nextModels) {
      if (model.isPreset || model.skinId) continue

      model.skinId = await invoke<string>(INVOKE_KEY.COMPUTE_SKIN_ID, { path: model.path })
    }

    const matched = find(nextModels, { id: currentModel.value?.id })

    currentModel.value = matched ?? nextModels[0]

    models.value = nextModels
  }

  return {
    modelReady,
    models,
    currentModel,
    supportKeys,
    pressedKeys,
    currentMotions,
    currentExpressions,
    shortcuts,
    init,
    resolveModel(skinId: string, mode: ModelMode) {
      return resolveSkinModel(models.value, skinId, mode)
    },
  }
}, {
  tauri: {
    filterKeys: ['supportKeys', 'pressedKeys'],
  },
})
