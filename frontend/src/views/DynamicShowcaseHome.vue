<template>
  <div data-testid="showcase-home" class="min-h-screen bg-[#f8f9fc] text-slate-900 dark:bg-[#0b1020] dark:text-slate-100">
    <div class="relative overflow-hidden bg-[#111936] text-white">
      <div class="pointer-events-none absolute -right-24 -top-32 h-96 w-96 rounded-full bg-violet-500/25 blur-3xl"></div>
      <div class="pointer-events-none absolute bottom-0 left-1/4 h-80 w-80 rounded-full bg-cyan-400/10 blur-3xl"></div>
      <div class="pointer-events-none absolute inset-0 opacity-20 [background-image:radial-gradient(#a5b4fc_1px,transparent_1px)] [background-size:28px_28px]"></div>

      <header class="relative z-10 border-b border-white/10">
        <nav class="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-4 px-5 py-4 sm:px-8">
          <router-link to="/home" class="flex min-w-0 items-center gap-3" :aria-label="siteName">
            <img :src="siteLogo || '/logo.svg'" :alt="siteName" class="h-10 w-10 shrink-0 rounded-xl bg-white/10 object-contain p-1" />
            <span class="truncate text-base font-bold tracking-tight sm:text-lg">{{ siteName }}</span>
          </router-link>
          <div class="flex flex-wrap items-center gap-2 sm:gap-4">
            <a href="#clients" class="hidden text-sm text-slate-300 transition hover:text-white sm:inline">{{ t('showcase.navClients') }}</a>
            <a href="#groups" class="hidden text-sm text-slate-300 transition hover:text-white sm:inline">{{ t('showcase.navGroups') }}</a>
            <a href="#models" class="hidden text-sm text-slate-300 transition hover:text-white sm:inline">{{ t('showcase.navModels') }}</a>
            <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="hidden text-sm text-slate-300 transition hover:text-white md:inline">{{ t('home.docs') }}</a>
            <LocaleSwitcher />
            <button type="button" class="flex h-9 w-9 items-center justify-center rounded-lg border border-white/15 text-slate-200 transition hover:bg-white/10" :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="toggleTheme">
              <Icon :name="isDark ? 'sun' : 'moon'" size="sm" />
            </button>
            <router-link :to="entryPath" class="rounded-lg bg-white px-3 py-2 text-sm font-semibold text-[#111936] transition hover:bg-indigo-100 sm:px-4">
              {{ isAuthenticated ? t('home.dashboard') : t('showcase.start') }}
            </router-link>
          </div>
        </nav>
      </header>

      <section class="relative z-10 mx-auto grid max-w-7xl items-center gap-12 px-5 pb-20 pt-16 sm:px-8 lg:grid-cols-[1.08fr_0.92fr] lg:pb-28 lg:pt-24">
        <div>
          <div class="mb-6 inline-flex items-center gap-2 rounded-full border border-indigo-300/25 bg-indigo-300/10 px-3 py-1.5 text-xs font-semibold tracking-wide text-indigo-200">
            <span class="h-1.5 w-1.5 rounded-full bg-emerald-300"></span>{{ t('showcase.eyebrow') }}
          </div>
          <h1 class="max-w-3xl text-4xl font-bold leading-tight tracking-tight sm:text-5xl lg:text-6xl">
            {{ t('showcase.heroLead') }}<span class="bg-gradient-to-r from-cyan-300 via-sky-300 to-violet-300 bg-clip-text text-transparent">{{ t('showcase.heroAccent') }}</span>
          </h1>
          <p class="mt-6 max-w-2xl text-base leading-8 text-slate-300 sm:text-lg">{{ siteSubtitle }}</p>
          <p class="mt-2 max-w-2xl text-sm leading-7 text-slate-400">{{ t('showcase.heroDescription') }}</p>
          <div class="mt-8 flex flex-wrap gap-3">
            <router-link :to="entryPath" class="inline-flex min-h-11 items-center gap-2 rounded-xl bg-gradient-to-r from-indigo-500 to-violet-500 px-5 py-2.5 text-sm font-semibold text-white shadow-lg shadow-indigo-950/30 transition hover:from-indigo-400 hover:to-violet-400">
              {{ isAuthenticated ? t('home.goToDashboard') : t('showcase.start') }} <span aria-hidden="true">↗</span>
            </router-link>
            <a href="#groups" class="inline-flex min-h-11 items-center rounded-xl border border-white/20 px-5 py-2.5 text-sm font-semibold text-white transition hover:bg-white/10">{{ t('showcase.exploreGroups') }}</a>
          </div>
          <div class="mt-10 flex flex-wrap gap-x-8 gap-y-3 border-t border-white/10 pt-6 text-xs text-slate-400">
            <span>{{ t('showcase.liveCatalog') }}</span>
            <span>{{ t('showcase.permissionAware') }}</span>
            <span>{{ t('showcase.clientFriendly') }}</span>
          </div>
        </div>

        <div class="relative mx-auto w-full max-w-xl lg:ml-auto">
          <div class="absolute -inset-5 rounded-[2rem] border border-white/10 bg-white/5 blur-sm"></div>
          <div class="relative overflow-hidden rounded-3xl border border-white/15 bg-[#172144]/95 p-5 shadow-2xl shadow-black/25 sm:p-7">
            <div class="flex items-center justify-between gap-4">
              <div class="flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.22em] text-slate-400"><span class="h-2 w-2 rounded-full bg-emerald-400"></span>{{ t('showcase.catalogTitle') }}</div>
              <span class="rounded-full bg-white/10 px-2.5 py-1 text-[11px] text-indigo-200">LIVE</span>
            </div>
            <div class="mt-7 grid grid-cols-2 gap-3">
              <div class="rounded-2xl border border-white/10 bg-white/[0.06] p-5"><div class="text-3xl font-bold text-white">{{ loading ? '—' : groups.length }}</div><div class="mt-1 text-xs text-slate-400">{{ t('showcase.groupsCount') }}</div></div>
              <div class="rounded-2xl border border-white/10 bg-white/[0.06] p-5"><div class="text-3xl font-bold text-white">{{ loading ? '—' : uniqueModels.length }}</div><div class="mt-1 text-xs text-slate-400">{{ t('showcase.modelsCount') }}</div></div>
            </div>
            <div class="mt-5 rounded-2xl border border-indigo-300/15 bg-gradient-to-br from-indigo-400/15 to-cyan-400/5 p-5">
              <div class="mb-4 flex items-center justify-between text-xs font-semibold text-slate-300"><span>{{ t('showcase.activePlatforms') }}</span><span>{{ activePlatforms.length }}</span></div>
              <div class="flex min-h-12 flex-wrap items-center gap-2">
                <span v-for="platform in activePlatforms.slice(0, 5)" :key="platform" class="rounded-lg border border-white/10 bg-white/10 px-3 py-1.5 text-xs font-medium text-white">{{ platformLabel(platform) }}</span>
                <span v-if="!loading && activePlatforms.length === 0" class="text-xs text-slate-400">{{ t('showcase.noPlatforms') }}</span>
                <span v-if="activePlatforms.length > 5" class="text-xs text-slate-300">+{{ activePlatforms.length - 5 }}</span>
              </div>
            </div>
            <div class="mt-5 flex items-center justify-between gap-4 border-t border-white/10 pt-5 text-xs text-slate-400"><span>{{ t('showcase.catalogFootnote') }}</span><span aria-hidden="true" class="text-lg text-cyan-300">↗</span></div>
          </div>
        </div>
      </section>
    </div>

    <main>
      <section id="clients" class="mx-auto max-w-7xl scroll-mt-8 px-5 py-20 sm:px-8">
        <div class="mb-9 flex flex-wrap items-end justify-between gap-4">
          <div><p class="text-xs font-bold uppercase tracking-[0.2em] text-indigo-600 dark:text-indigo-400">01 / CLIENTS</p><h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">{{ t('showcase.clientsTitle') }}</h2><p class="mt-3 max-w-2xl text-sm leading-7 text-slate-600 dark:text-slate-400">{{ t('showcase.clientsDescription') }}</p></div>
        </div>
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
          <div v-for="client in clients" :key="client.name" class="group rounded-2xl border border-slate-200 bg-white p-5 shadow-sm transition hover:-translate-y-1 hover:border-indigo-200 hover:shadow-xl hover:shadow-indigo-100/50 dark:border-white/10 dark:bg-white/[0.04] dark:hover:border-indigo-400/40 dark:hover:shadow-none">
            <div class="flex h-11 w-11 items-center justify-center rounded-xl text-lg font-bold" :class="client.badgeClass">{{ client.initial }}</div>
            <h3 class="mt-6 text-base font-bold">{{ client.name }}</h3>
            <p class="mt-2 min-h-12 text-xs leading-6 text-slate-500 dark:text-slate-400">{{ t(client.descriptionKey) }}</p>
            <div class="mt-5 border-t border-slate-100 pt-4 text-[11px] font-medium text-indigo-600 dark:border-white/10 dark:text-indigo-300">{{ t('showcase.customApi') }} <span aria-hidden="true">↗</span></div>
          </div>
        </div>
        <p class="mt-5 text-xs text-slate-500 dark:text-slate-400">{{ t('showcase.clientNote') }}</p>
      </section>

      <section id="groups" class="scroll-mt-8 border-y border-slate-200 bg-white/70 py-20 dark:border-white/10 dark:bg-white/[0.02]">
        <div class="mx-auto max-w-7xl px-5 sm:px-8">
          <div class="flex flex-wrap items-end justify-between gap-5">
            <div><p class="text-xs font-bold uppercase tracking-[0.2em] text-indigo-600 dark:text-indigo-400">02 / GROUPS</p><h2 class="mt-3 text-3xl font-bold tracking-tight sm:text-4xl">{{ t('showcase.groupsTitle') }}</h2><p class="mt-3 text-sm text-slate-600 dark:text-slate-400">{{ t('showcase.groupsDescription') }}</p></div>
            <div class="relative w-full sm:w-72"><label for="showcase-search" class="sr-only">{{ t('showcase.searchPlaceholder') }}</label><input id="showcase-search" v-model.trim="search" type="search" class="w-full rounded-xl border border-slate-200 bg-white px-4 py-3 text-sm outline-none transition focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/15 dark:border-white/10 dark:bg-[#151b2e]" :placeholder="t('showcase.searchPlaceholder')" /></div>
          </div>
          <div v-if="loading" class="mt-8 grid gap-4 md:grid-cols-2 lg:grid-cols-3" aria-live="polite"><div v-for="n in 3" :key="n" class="h-56 animate-pulse rounded-2xl bg-slate-100 dark:bg-white/5"></div></div>
          <div v-else-if="error" class="mt-8 rounded-2xl border border-rose-200 bg-rose-50 p-8 text-center dark:border-rose-500/30 dark:bg-rose-500/10" role="alert"><p class="text-sm text-rose-700 dark:text-rose-300">{{ t('showcase.loadError') }}</p><button type="button" class="mt-4 rounded-lg bg-rose-600 px-4 py-2 text-sm font-semibold text-white" @click="loadCatalog">{{ t('showcase.retry') }}</button></div>
          <div v-else-if="filteredGroups.length === 0" class="mt-8 rounded-2xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500 dark:border-white/15 dark:text-slate-400">{{ search ? t('showcase.noSearchResults') : t('showcase.noGroups') }}</div>
          <div v-else class="mt-8 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            <article v-for="group in filteredGroups" :key="group.id" class="flex min-w-0 flex-col rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-white/10 dark:bg-[#151b2e]">
              <div class="flex items-start justify-between gap-3"><span class="rounded-lg bg-indigo-50 px-3 py-1.5 text-xs font-semibold text-indigo-700 dark:bg-indigo-400/10 dark:text-indigo-300">{{ platformLabel(group.platform) }}</span><span class="rounded-full border border-slate-200 px-2.5 py-1 text-[11px] font-medium text-slate-500 dark:border-white/10 dark:text-slate-300">{{ billingLabel(group.subscription_type) }}</span></div>
              <h3 class="mt-5 break-words text-lg font-bold">{{ group.name }}</h3>
              <p class="mt-2 min-h-12 text-sm leading-6 text-slate-500 dark:text-slate-400">{{ group.description || t('showcase.groupFallback') }}</p>
              <div class="mt-5 flex flex-wrap gap-2"><span v-for="model in group.models.slice(0, 4)" :key="`${model.platform}:${model.name}`" class="max-w-full truncate rounded-md bg-slate-100 px-2.5 py-1 text-[11px] font-medium text-slate-600 dark:bg-white/10 dark:text-slate-300" :title="model.name">{{ model.name }}</span><span v-if="group.models.length > 4" class="px-1 py-1 text-[11px] text-slate-500">+{{ group.models.length - 4 }}</span><span v-if="group.models.length === 0" class="text-xs text-slate-400">{{ t('showcase.modelsPending') }}</span></div>
              <div class="mt-auto flex items-center justify-between border-t border-slate-100 pt-5 text-xs text-slate-500 dark:border-white/10 dark:text-slate-400"><span>{{ t('showcase.groupModelCount', { count: group.models.length }) }}</span><span v-if="group.is_exclusive" class="text-amber-600 dark:text-amber-300">{{ t('showcase.exclusive') }}</span></div>
            </article>
          </div>
        </div>
      </section>

      <section id="models" class="mx-auto max-w-7xl scroll-mt-8 px-5 py-20 sm:px-8">
        <p class="text-xs font-bold uppercase tracking-[0.2em] text-indigo-600 dark:text-indigo-400">03 / MODELS</p>
        <div class="mt-3 flex flex-wrap items-end justify-between gap-4"><div><h2 class="text-3xl font-bold tracking-tight sm:text-4xl">{{ t('showcase.modelsTitle') }}</h2><p class="mt-3 text-sm text-slate-600 dark:text-slate-400">{{ t('showcase.modelsDescription') }}</p></div><span class="rounded-full bg-indigo-50 px-3 py-1.5 text-xs font-semibold text-indigo-700 dark:bg-indigo-400/10 dark:text-indigo-300">{{ uniqueModels.length }} {{ t('showcase.modelsCount') }}</span></div>
        <div v-if="!loading && !error && uniqueModels.length" class="mt-8 flex flex-wrap gap-2">
          <button type="button" class="rounded-full px-3 py-1.5 text-xs font-medium transition" :class="selectedPlatform === 'all' ? activeFilterClass : inactiveFilterClass" @click="selectedPlatform = 'all'; showAllModels = false">{{ t('showcase.allPlatforms') }}</button>
          <button v-for="platform in modelPlatforms" :key="platform" type="button" class="rounded-full px-3 py-1.5 text-xs font-medium transition" :class="selectedPlatform === platform ? activeFilterClass : inactiveFilterClass" @click="selectedPlatform = platform; showAllModels = false">{{ platformLabel(platform) }}</button>
        </div>
        <div v-if="!loading && !error && filteredModels.length" class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div v-for="model in visibleModels" :key="`${model.platform}:${model.name}`" class="min-w-0 rounded-xl border border-slate-200 bg-white px-4 py-4 dark:border-white/10 dark:bg-[#151b2e]"><div class="truncate text-sm font-semibold" :title="model.name">{{ model.name }}</div><div class="mt-1 text-xs text-slate-500 dark:text-slate-400">{{ platformLabel(model.platform) }}</div></div>
        </div>
        <p v-if="!loading && !error && !filteredModels.length" class="mt-8 text-sm text-slate-500">{{ t('showcase.noModels') }}</p>
        <button v-if="!showAllModels && filteredModels.length > 12" type="button" class="mt-6 rounded-xl border border-slate-200 bg-white px-5 py-2.5 text-sm font-semibold text-slate-700 transition hover:border-indigo-300 dark:border-white/15 dark:bg-white/5 dark:text-slate-200" @click="showAllModels = true">{{ t('showcase.showAllModels', { count: filteredModels.length }) }}</button>
      </section>

      <section class="bg-[#111936] px-5 py-14 text-white sm:px-8"><div class="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-6"><div><h2 class="text-2xl font-bold sm:text-3xl">{{ t('showcase.ctaTitle') }}</h2><p class="mt-2 text-sm text-slate-300">{{ t('showcase.ctaDescription') }}</p></div><router-link :to="entryPath" class="rounded-xl bg-white px-5 py-3 text-sm font-semibold text-[#111936] transition hover:bg-indigo-100">{{ isAuthenticated ? t('home.goToDashboard') : t('showcase.start') }} ↗</router-link></div></section>
    </main>
    <footer class="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-3 px-5 py-7 text-xs text-slate-500 sm:px-8"><span>© {{ new Date().getFullYear() }} {{ siteName }}</span><span>{{ t('showcase.footer') }}</span></footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import { getHomeCatalog, type HomeCatalogGroup, type HomeCatalogModel } from '@/api/homeCatalog'
import { sanitizeUrl } from '@/utils/url'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const groups = ref<HomeCatalogGroup[]>([])
const loading = ref(true)
const error = ref(false)
const search = ref('')
const selectedPlatform = ref('all')
const showAllModels = ref(false)
const isDark = ref(document.documentElement.classList.contains('dark'))
let requestController: AbortController | undefined

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || t('showcase.defaultSubtitle'))
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const entryPath = computed(() => isAuthenticated.value ? (authStore.isAdmin ? '/admin/dashboard' : '/dashboard') : '/login')
const activePlatforms = computed(() => [...new Set(groups.value.flatMap(group => group.models.map(model => model.platform)).concat(groups.value.filter(group => group.platform !== 'composite').map(group => group.platform)))].sort())
const uniqueModels = computed<HomeCatalogModel[]>(() => {
  const byKey = new Map<string, HomeCatalogModel>()
  for (const group of groups.value) for (const model of group.models) byKey.set(`${model.platform}\x00${model.name.toLowerCase()}`, model)
  return [...byKey.values()].sort((a, b) => a.name.localeCompare(b.name))
})
const filteredGroups = computed(() => {
  const query = search.value.toLowerCase()
  return groups.value.filter(group => !query || [group.name, group.description, group.platform, ...group.models.map(model => model.name)].some(value => value.toLowerCase().includes(query)))
})
const filteredModels = computed(() => uniqueModels.value.filter(model => selectedPlatform.value === 'all' || model.platform === selectedPlatform.value))
const modelPlatforms = computed(() => [...new Set(uniqueModels.value.map(model => model.platform))].sort())
const visibleModels = computed(() => showAllModels.value ? filteredModels.value : filteredModels.value.slice(0, 12))
const activeFilterClass = 'bg-indigo-600 text-white'
const inactiveFilterClass = 'border border-slate-200 bg-white text-slate-600 hover:border-indigo-300 dark:border-white/10 dark:bg-white/5 dark:text-slate-300'
const clients = [
  { name: 'Codex', initial: 'C', descriptionKey: 'showcase.clients.codex', badgeClass: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-400/15 dark:text-emerald-300' },
  { name: 'Claude Code', initial: '✳', descriptionKey: 'showcase.clients.claude', badgeClass: 'bg-orange-100 text-orange-700 dark:bg-orange-400/15 dark:text-orange-300' },
  { name: 'OpenCode', initial: 'O', descriptionKey: 'showcase.clients.opencode', badgeClass: 'bg-blue-100 text-blue-700 dark:bg-blue-400/15 dark:text-blue-300' },
  { name: 'WorkBuddy', initial: 'W', descriptionKey: 'showcase.clients.workbuddy', badgeClass: 'bg-violet-100 text-violet-700 dark:bg-violet-400/15 dark:text-violet-300' },
  { name: 'CodeBuddy IDE', initial: '⌘', descriptionKey: 'showcase.clients.codebuddy', badgeClass: 'bg-cyan-100 text-cyan-700 dark:bg-cyan-400/15 dark:text-cyan-300' },
]

function platformLabel(platform: string) {
  const key = `showcase.platforms.${platform}`
  const label = t(key)
  return label === key ? platform : label
}
function billingLabel(type: string) {
  if (type === 'subscription_balance') return t('showcase.billingHybrid')
  if (type === 'subscription') return t('showcase.billingSubscription')
  return t('showcase.billingBalance')
}
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}
async function loadCatalog() {
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller
  loading.value = true
  error.value = false
  try {
    groups.value = await getHomeCatalog({ signal: controller.signal })
  } catch {
    if (!controller.signal.aborted) error.value = true
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}
onMounted(loadCatalog)
onUnmounted(() => requestController?.abort())
</script>
