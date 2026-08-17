import { apiClient } from "../client";

export interface JuiceFixerRule {
  model: string;
  reasoning_effort: string;
  value: number;
}

export interface JuiceFixerSetting {
  enabled: boolean;
  rules: JuiceFixerRule[];
}

/**
 * Get the Juice value fixer configuration
 * GET /api/v1/admin/settings/juice-fixer
 */
export async function getJuiceFixerConfig(): Promise<JuiceFixerSetting> {
  const { data } = await apiClient.get<JuiceFixerSetting>(
    "/admin/settings/juice-fixer",
  );
  return data;
}

/**
 * Update the Juice value fixer configuration
 * PUT /api/v1/admin/settings/juice-fixer
 */
export async function updateJuiceFixerConfig(
  setting: JuiceFixerSetting,
): Promise<JuiceFixerSetting> {
  const { data } = await apiClient.put<JuiceFixerSetting>(
    "/admin/settings/juice-fixer",
    setting,
  );
  return data;
}

export const juiceFixerAPI = {
  getJuiceFixerConfig,
  updateJuiceFixerConfig,
};
