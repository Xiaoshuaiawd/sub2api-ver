import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";

import JuiceFixerSection from "../settings/JuiceFixerSection.vue";

const {
  getJuiceFixerConfig,
  updateJuiceFixerConfig,
} = vi.hoisted(() => ({
  getJuiceFixerConfig: vi.fn(),
  updateJuiceFixerConfig: vi.fn(),
}));

vi.mock("@/api/admin", () => ({
  adminAPI: {
    juiceFixer: {
      getJuiceFixerConfig,
      updateJuiceFixerConfig,
    },
  },
}));

vi.mock("@/stores", () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
  }),
}));

vi.mock("@/utils/apiError", () => ({
  extractApiErrorMessage: (err: unknown, fallback: string) =>
    (err as { message?: string })?.message ?? fallback,
}));

vi.mock("vue-i18n", () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}));

function mountSection() {
  return mount(JuiceFixerSection, {
    global: {
      stubs: {
        Icon: { template: "<i />" },
      },
    },
  });
}

describe("JuiceFixerSection", () => {
  beforeEach(() => {
    getJuiceFixerConfig.mockReset();
    updateJuiceFixerConfig.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("loads config and renders existing rules", async () => {
    getJuiceFixerConfig.mockResolvedValue({
      enabled: true,
      rules: [{ model: "gpt-5.6-sol", reasoning_effort: "low", value: 8 }],
    });

    const wrapper = mountSection();
    await flushPromises();

    expect(getJuiceFixerConfig).toHaveBeenCalledTimes(1);
    const modelInput = wrapper.find('input[placeholder="admin.settings.juiceFixer.modelPlaceholder"]');
    expect((modelInput.element as HTMLInputElement).value).toBe("gpt-5.6-sol");
    const valueInput = wrapper.find('input[type="number"]');
    expect((valueInput.element as HTMLInputElement).value).toBe("8");
    const enabledInput = wrapper.find('input[type="checkbox"]');
    expect((enabledInput.element as HTMLInputElement).checked).toBe(true);
  });

  it("shows an empty state when config has no rules", async () => {
    getJuiceFixerConfig.mockResolvedValue({ enabled: false, rules: [] });

    const wrapper = mountSection();
    await flushPromises();

    const inputs = wrapper.findAll(
      'input[placeholder="admin.settings.juiceFixer.modelPlaceholder"]',
    );
    expect(inputs).toHaveLength(0);
    expect(wrapper.text()).toContain("admin.settings.juiceFixer.emptyRules");
  });

  it("saves the configured payload", async () => {
    getJuiceFixerConfig.mockResolvedValue({
      enabled: true,
      rules: [{ model: "gpt-5.6-sol", reasoning_effort: "low", value: 8 }],
    });
    updateJuiceFixerConfig.mockResolvedValue({
      enabled: true,
      rules: [{ model: "gpt-5.6-sol", reasoning_effort: "low", value: 8 }],
    });

    const wrapper = mountSection();
    await flushPromises();

    const saveButton = wrapper.findAll("button").find((btn) =>
      btn.text().includes("common.save"),
    );
    expect(saveButton).toBeTruthy();
    await saveButton!.trigger("click");
    await flushPromises();

    expect(updateJuiceFixerConfig).toHaveBeenCalledWith({
      enabled: true,
      rules: [{ model: "gpt-5.6-sol", reasoning_effort: "low", value: 8 }],
    });
  });

  it("adds and removes rules", async () => {
    getJuiceFixerConfig.mockResolvedValue({
      enabled: false,
      rules: [{ model: "gpt-5.6-sol", reasoning_effort: "", value: 8 }],
    });

    const wrapper = mountSection();
    await flushPromises();

    const addButton = wrapper.findAll("button").find((btn) =>
      btn.text().includes("admin.settings.juiceFixer.addRule"),
    );
    await addButton!.trigger("click");

    let inputs = wrapper.findAll(
      'input[placeholder="admin.settings.juiceFixer.modelPlaceholder"]',
    );
    expect(inputs).toHaveLength(2);

    const removeButtons = wrapper.findAll('button[title="admin.settings.juiceFixer.removeRule"]');
    await removeButtons[0]!.trigger("click");

    inputs = wrapper.findAll(
      'input[placeholder="admin.settings.juiceFixer.modelPlaceholder"]',
    );
    expect(inputs).toHaveLength(1);
  });

  it("omits a completely blank draft rule when saving", async () => {
    getJuiceFixerConfig.mockResolvedValue({ enabled: false, rules: [] });
    updateJuiceFixerConfig.mockResolvedValue({ enabled: false, rules: [] });

    const wrapper = mountSection();
    await flushPromises();
    const addButton = wrapper.findAll("button").find((btn) =>
      btn.text().includes("admin.settings.juiceFixer.addRule"),
    );
    await addButton!.trigger("click");

    const saveButton = wrapper.findAll("button").find((btn) =>
      btn.text().includes("common.save"),
    );
    await saveButton!.trigger("click");
    await flushPromises();

    expect(updateJuiceFixerConfig).toHaveBeenCalledWith({
      enabled: false,
      rules: [],
    });
  });

  it("removes the final rule and saves an empty rules list", async () => {
    getJuiceFixerConfig.mockResolvedValue({
      enabled: false,
      rules: [{ model: "gpt-5.6-sol", reasoning_effort: "", value: 8 }],
    });

    const wrapper = mountSection();
    await flushPromises();
    updateJuiceFixerConfig.mockResolvedValue({ enabled: false, rules: [] });

    const removeButton = wrapper.find(
      'button[title="admin.settings.juiceFixer.removeRule"]',
    );
    expect((removeButton.element as HTMLButtonElement).disabled).toBe(false);
    await removeButton.trigger("click");

    expect(
      wrapper.findAll('input[placeholder="admin.settings.juiceFixer.modelPlaceholder"]'),
    ).toHaveLength(0);
    const saveButton = wrapper.findAll("button").find((btn) =>
      btn.text().includes("common.save"),
    );
    await saveButton!.trigger("click");
    await flushPromises();

    expect(updateJuiceFixerConfig).toHaveBeenCalledWith({
      enabled: false,
      rules: [],
    });
  });
});
