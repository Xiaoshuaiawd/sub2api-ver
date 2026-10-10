import { apiClient } from './client'

export interface HomeCatalogModel {
  name: string
  platform: string
}

export interface HomeCatalogGroup {
  id: number
  name: string
  description: string
  platform: string
  subscription_type: string
  is_exclusive: boolean
  models: HomeCatalogModel[]
}

export async function getHomeCatalog(options?: { signal?: AbortSignal }): Promise<HomeCatalogGroup[]> {
  const { data } = await apiClient.get<{ groups: HomeCatalogGroup[] }>('/home/catalog', {
    signal: options?.signal,
  })
  return data.groups
}
