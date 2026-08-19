export const PRO_MODE_STORAGE_KEY = 'admin-accounts-pro-mode'

export type ProModeWindow = '5h' | '7d'

export interface ProModeUsage {
  utilization: number
  costPerPercent: number
  cost: number
  requests: number
}

function hashSeed(value: string): number {
  let hash = 2166136261
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}

function stableRange(seed: string, min: number, max: number): number {
  return min + (hashSeed(seed) % (max - min + 1))
}

export function buildProModeUsage(accountId: number, window: ProModeWindow): ProModeUsage {
  const seed = `${accountId}:${window}`
  const utilization = window === '5h'
    ? stableRange(`${seed}:utilization`, 0, 3)
    : stableRange(`${seed}:utilization`, 30, 60)
  const costPerPercent = stableRange(`${seed}:cost`, 2000, 2600) / 100
  const requests = stableRange(`${seed}:requests`, 1000, 2500)

  return {
    utilization,
    costPerPercent,
    cost: Number((utilization * costPerPercent).toFixed(2)),
    requests
  }
}

export function readProModeEnabled(): boolean {
  try {
    return localStorage.getItem(PRO_MODE_STORAGE_KEY) === 'true'
  } catch {
    return false
  }
}

export function writeProModeEnabled(enabled: boolean): void {
  try {
    localStorage.setItem(PRO_MODE_STORAGE_KEY, enabled ? 'true' : 'false')
  } catch {
    // The display mode still works for this session when storage is unavailable.
  }
}
