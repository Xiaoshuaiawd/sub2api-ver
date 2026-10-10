<template>
  <div ref="rootEl" data-testid="showcase-home" class="credit-home min-h-screen overflow-x-hidden">
    <div ref="scrollProgressEl" class="scroll-progress fixed inset-x-0 top-0 z-50 h-0.5 origin-left" aria-hidden="true"></div>
    <header class="site-header sticky top-0 z-30 border-b border-slate-100/80 bg-white/80 backdrop-blur-xl">
      <nav class="mx-auto flex max-w-7xl items-center justify-between gap-3 px-5 py-3.5 sm:px-8">
        <router-link to="/home" class="flex min-w-0 flex-1 items-center gap-2.5" :aria-label="siteName">
          <img :src="siteLogo || '/logo.svg'" :alt="siteName" class="h-9 w-9 shrink-0 rounded-xl object-contain" />
          <span class="truncate text-base font-bold tracking-tight text-slate-950 sm:text-lg">{{ siteName }}</span>
        </router-link>
        <div class="flex shrink-0 items-center gap-2 sm:gap-6">
          <a href="#clients" class="nav-link hidden text-sm font-medium text-slate-600 sm:inline">{{ t('showcase.navClients') }}</a>
          <a href="#groups" class="nav-link hidden text-sm font-medium text-slate-600 sm:inline">{{ t('showcase.navGroups') }}</a>
          <a href="#models" class="nav-link hidden text-sm font-medium text-slate-600 sm:inline">{{ t('showcase.navModels') }}</a>
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="nav-link hidden text-sm font-medium text-slate-600 md:inline">{{ t('home.docs') }}</a>
          <LocaleSwitcher />
          <router-link :to="entryPath" class="pressable primary-button inline-flex min-h-10 items-center gap-2 rounded-full px-4 py-2 text-sm font-semibold text-white">
            {{ isAuthenticated ? t('home.dashboard') : t('showcase.start') }} <span aria-hidden="true">↗</span>
          </router-link>
        </div>
      </nav>
    </header>

    <main>
      <section class="hero-section relative isolate flex min-h-[min(820px,calc(100svh-68px))] items-center overflow-hidden px-5 py-20 sm:px-8 lg:py-24">
        <div aria-hidden="true" class="ambient ambient-blue"></div>
        <div aria-hidden="true" class="ambient ambient-violet"></div>
        <div class="relative z-10 mx-auto grid w-full max-w-7xl items-center gap-14 lg:grid-cols-2 lg:gap-20">
          <div class="hero-copy" data-reveal>
            <p class="mb-5 text-sm font-semibold tracking-wide text-indigo-600">{{ t('showcase.eyebrow') }}</p>
            <h1 class="max-w-2xl text-[2.7rem] font-extrabold leading-[1.12] tracking-tight text-slate-950 sm:text-5xl lg:text-6xl">
              {{ t('showcase.heroLead') }}<span class="block text-indigo-600">{{ t('showcase.heroAccent') }}</span>
            </h1>
            <p class="mt-7 max-w-xl text-base leading-8 text-slate-600 sm:text-lg">{{ siteSubtitle }}</p>
            <p class="mt-2 max-w-xl text-sm leading-7 text-slate-500">{{ t('showcase.heroDescription') }}</p>
            <div class="mt-9 flex flex-wrap gap-3">
              <router-link :to="entryPath" class="pressable primary-button inline-flex min-h-12 items-center gap-2 rounded-full px-6 py-3 text-sm font-semibold text-white shadow-sm">
                {{ isAuthenticated ? t('home.goToDashboard') : t('showcase.start') }} <span aria-hidden="true">→</span>
              </router-link>
              <a href="#groups" class="pressable secondary-button inline-flex min-h-12 items-center rounded-full px-6 py-3 text-sm font-semibold text-slate-800">{{ t('showcase.exploreGroups') }}</a>
            </div>
            <div class="mt-14 flex flex-wrap gap-x-8 gap-y-4 border-t border-slate-200/80 pt-7 text-sm font-medium text-slate-600">
              <span class="flex items-center gap-2"><i class="benefit-dot bg-indigo-100 text-indigo-600">✓</i>{{ t('showcase.liveCatalog') }}</span>
              <span class="flex items-center gap-2"><i class="benefit-dot bg-sky-100 text-sky-600">✓</i>{{ t('showcase.permissionAware') }}</span>
              <span class="flex items-center gap-2"><i class="benefit-dot bg-emerald-100 text-emerald-600">✓</i>{{ t('showcase.clientFriendly') }}</span>
            </div>
          </div>

          <div class="preview-scene relative mx-auto w-full max-w-lg lg:ml-auto" data-reveal>
            <div aria-hidden="true" class="preview-halo"></div>
            <div class="preview-card relative overflow-hidden rounded-[1.75rem] border border-white/80 bg-white/75 p-6 shadow-[0_28px_65px_rgba(83,81,174,0.13)] backdrop-blur-xl sm:p-8">
              <div class="flex items-center justify-between gap-4">
                <div class="flex items-center gap-3"><span class="flex h-10 w-10 items-center justify-center rounded-xl bg-indigo-50 text-indigo-600" aria-hidden="true">⌘</span><div><p class="text-sm font-bold text-slate-900">{{ siteName }}</p><p class="text-xs text-slate-500">{{ t('showcase.catalogTitle') }}</p></div></div>
                <span class="rounded-full bg-emerald-50 px-2.5 py-1 text-[11px] font-semibold text-emerald-700">{{ t('showcase.liveCatalog') }}</span>
              </div>
              <div class="mt-14 grid grid-cols-2 gap-4 border-b border-slate-200/80 pb-7">
                <div><p class="text-sm text-slate-500">{{ t('showcase.groupsCount') }}</p><p class="mt-1 text-4xl font-bold tracking-tight text-slate-950 tabular-nums">{{ loading ? '—' : groups.length }}</p></div>
                <div><p class="text-sm text-slate-500">{{ t('showcase.modelsCount') }}</p><p class="mt-1 text-4xl font-bold tracking-tight text-slate-950 tabular-nums">{{ loading ? '—' : uniqueModels.length }}</p></div>
              </div>
              <div class="mt-5 flex items-center justify-between gap-4"><div class="min-w-0"><p class="text-xs text-slate-500">{{ t('showcase.featuredModel') }}</p><p class="mt-1 truncate text-sm font-semibold text-slate-900" :title="featuredModel">{{ featuredModel }}</p></div><span class="h-8 w-8 shrink-0 rounded-full bg-indigo-600"></span></div>
            </div>
            <div class="floating-count absolute -right-2 -top-5 z-10 rounded-2xl border border-white bg-white/95 px-4 py-3 shadow-[0_14px_35px_rgba(74,72,149,0.13)] sm:-right-5"><span class="text-xs text-slate-500">{{ t('showcase.activePlatforms') }}</span><strong class="mt-0.5 block text-base text-indigo-600 tabular-nums">{{ activePlatforms.length }}</strong></div>
          </div>
        </div>
        <a href="#clients" class="scroll-cue absolute bottom-6 left-1/2 hidden -translate-x-1/2 items-center gap-2 text-xs font-medium text-slate-500 transition-colors hover:text-indigo-600 lg:inline-flex">{{ t('showcase.scrollToExplore') }} <span aria-hidden="true">↓</span></a>
      </section>

      <section id="clients" class="developer-section relative scroll-mt-20 overflow-hidden px-5 py-20 sm:px-8 lg:py-28">
        <div aria-hidden="true" class="developer-glow"></div>
        <div class="relative mx-auto grid max-w-7xl items-center gap-12 lg:grid-cols-2 lg:gap-20">
          <div class="code-window overflow-hidden rounded-2xl bg-[#111216] shadow-[0_24px_55px_rgba(37,32,72,0.2)]" data-reveal>
            <div class="flex items-center justify-between border-b border-white/10 px-5 py-4"><span class="flex items-center gap-2" aria-hidden="true"><i class="h-2.5 w-2.5 rounded-full bg-rose-400"></i><i class="h-2.5 w-2.5 rounded-full bg-amber-300"></i><i class="h-2.5 w-2.5 rounded-full bg-emerald-400"></i></span><span class="font-mono text-xs text-slate-400">api-request.sh</span><button type="button" class="copy-button min-h-10 rounded-lg px-3 py-2 text-xs text-slate-300 transition hover:bg-white/10 hover:text-white" :aria-label="t('showcase.copyCode')" @click="copySnippet">{{ copyState === 'copied' ? t('showcase.copied') : copyState === 'error' ? t('showcase.copyFailed') : t('showcase.copyCode') }}</button></div>
            <pre class="overflow-x-auto p-5 font-mono text-xs leading-7 text-slate-300 sm:p-7 sm:text-sm"><code><span class="text-violet-300">curl</span> -X POST "<span class="text-emerald-300">API_BASE_URL/v1/responses</span>" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL","input":"Hello, world"}'</code></pre>
          </div>
          <div data-reveal>
            <p class="section-eyebrow">01 / {{ t('showcase.navClients') }}</p>
            <h2 class="mt-3 text-3xl font-bold tracking-tight text-slate-950 sm:text-4xl">{{ t('showcase.clientsTitle') }}</h2>
            <p class="mt-5 max-w-xl text-base leading-8 text-slate-600">{{ t('showcase.clientsDescription') }}</p>
            <div class="mt-8 grid gap-3 sm:grid-cols-2">
              <div v-for="(client, index) in clients" :key="client.name" data-reveal class="client-item rounded-2xl border border-slate-200/80 bg-white/75 p-4 transition" :style="{ transitionDelay: `${index * 65}ms` }"><div class="flex items-center gap-3"><span class="client-symbol flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-indigo-50"><img :src="client.icon" alt="" aria-hidden="true" :class="client.iconClass" class="object-contain" width="28" height="28" loading="lazy" /></span><div class="min-w-0"><h3 class="text-sm font-semibold text-slate-900">{{ client.name }}</h3><p class="mt-0.5 text-xs leading-5 text-slate-500">{{ t(client.descriptionKey) }}</p></div></div></div>
            </div>
            <p class="mt-5 text-xs leading-6 text-slate-500">{{ t('showcase.clientNote') }}</p>
          </div>
        </div>
      </section>

      <section id="groups" class="scroll-mt-20 px-5 py-20 sm:px-8 lg:py-28">
        <div class="mx-auto max-w-7xl">
          <div class="flex flex-wrap items-end justify-between gap-5" data-reveal><div><p class="section-eyebrow">02 / {{ t('showcase.navGroups') }}</p><h2 class="mt-3 text-3xl font-bold tracking-tight text-slate-950 sm:text-4xl">{{ t('showcase.groupsTitle') }}</h2><p class="mt-4 text-sm leading-7 text-slate-600">{{ t('showcase.groupsDescription') }}</p></div><div class="relative w-full sm:w-72"><label for="showcase-search" class="sr-only">{{ t('showcase.searchPlaceholder') }}</label><input id="showcase-search" v-model.trim="search" type="search" class="min-h-12 w-full rounded-xl border border-slate-200 bg-white px-4 py-3 text-sm text-slate-900 outline-none transition placeholder:text-slate-400 focus:border-indigo-400 focus:ring-4 focus:ring-indigo-100" :placeholder="t('showcase.searchPlaceholder')" /></div></div>
          <div v-if="loading" class="mt-9 grid gap-4 md:grid-cols-2 lg:grid-cols-3" aria-live="polite"><div v-for="n in 3" :key="n" class="h-56 animate-pulse rounded-2xl bg-slate-100"></div></div>
          <div v-else-if="error" class="mt-9 rounded-2xl border border-rose-200 bg-rose-50 p-8 text-center" role="alert"><p class="text-sm text-rose-700">{{ t('showcase.loadError') }}</p><button type="button" class="pressable primary-button mt-4 min-h-11 rounded-full px-5 py-2 text-sm font-semibold text-white" @click="loadCatalog">{{ t('showcase.retry') }}</button></div>
          <div v-else-if="filteredGroups.length === 0" class="mt-9 rounded-2xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500">{{ search ? t('showcase.noSearchResults') : t('showcase.noGroups') }}</div>
          <div v-else class="mt-9 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            <article v-for="(group, index) in filteredGroups" :key="group.id" data-reveal class="catalog-card flex min-w-0 flex-col rounded-2xl border border-slate-200/80 bg-white p-6" :style="{ transitionDelay: `${Math.min(index, 6) * 65}ms` }"><div class="flex items-start justify-between gap-3"><span class="rounded-full bg-indigo-50 px-3 py-1.5 text-xs font-semibold text-indigo-700">{{ platformLabel(group.platform) }}</span><span class="rounded-full bg-slate-100 px-2.5 py-1.5 text-xs text-slate-600">{{ billingLabel(group.subscription_type) }}</span></div><h3 class="mt-6 break-words text-lg font-bold text-slate-950">{{ group.name }}</h3><p class="mt-2 min-h-12 text-sm leading-6 text-slate-600">{{ group.description || t('showcase.groupFallback') }}</p><div class="mt-6 flex flex-wrap gap-2"><span v-for="model in group.models.slice(0, 4)" :key="`${model.platform}:${model.name}`" class="max-w-full truncate rounded-md bg-slate-50 px-2.5 py-1 text-xs text-slate-600" :title="model.name">{{ model.name }}</span><span v-if="group.models.length > 4" class="px-1 py-1 text-xs text-indigo-600">+{{ group.models.length - 4 }}</span><span v-if="group.models.length === 0" class="text-xs text-slate-400">{{ t('showcase.modelsPending') }}</span></div><div class="mt-auto flex items-center justify-between border-t border-slate-100 pt-5 text-xs text-slate-500"><span>{{ t('showcase.groupModelCount', { count: group.models.length }) }}</span><span v-if="group.is_exclusive" class="text-amber-700">{{ t('showcase.exclusive') }}</span></div></article>
          </div>
        </div>
      </section>

      <section id="models" class="models-section scroll-mt-20 px-5 py-20 sm:px-8 lg:py-28">
        <div class="mx-auto max-w-7xl">
          <div class="flex flex-wrap items-end justify-between gap-4" data-reveal><div><p class="section-eyebrow">03 / {{ t('showcase.navModels') }}</p><h2 class="mt-3 text-3xl font-bold tracking-tight text-slate-950 sm:text-4xl">{{ t('showcase.modelsTitle') }}</h2><p class="mt-4 text-sm leading-7 text-slate-600">{{ t('showcase.modelsDescription') }}</p></div><span class="rounded-full border border-indigo-100 bg-white px-3 py-1.5 text-xs font-semibold text-indigo-700">{{ uniqueModels.length }} {{ t('showcase.modelsCount') }}</span></div>
          <div v-if="!loading && !error && uniqueModels.length" class="mt-8 flex flex-wrap gap-2"><button type="button" class="pressable min-h-11 rounded-full px-4 py-2.5 text-xs font-medium transition" :class="selectedPlatform === 'all' ? activeFilterClass : inactiveFilterClass" :aria-pressed="selectedPlatform === 'all'" @click="selectedPlatform = 'all'; showAllModels = false">{{ t('showcase.allPlatforms') }}</button><button v-for="platform in modelPlatforms" :key="platform" type="button" class="pressable min-h-11 rounded-full px-4 py-2.5 text-xs font-medium transition" :class="selectedPlatform === platform ? activeFilterClass : inactiveFilterClass" :aria-pressed="selectedPlatform === platform" @click="selectedPlatform = platform; showAllModels = false">{{ platformLabel(platform) }}</button></div>
          <div v-if="!loading && !error && filteredModels.length" class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4"><div v-for="(model, index) in visibleModels" :key="`${model.platform}:${model.name}`" data-reveal class="model-card min-w-0 rounded-xl border border-white bg-white p-4" :style="{ transitionDelay: `${Math.min(index, 7) * 45}ms` }"><div class="truncate text-sm font-semibold text-slate-900" :title="model.name">{{ model.name }}</div><div class="mt-1 text-xs text-slate-500">{{ platformLabel(model.platform) }}</div></div></div>
          <p v-if="!loading && !error && !filteredModels.length" class="mt-8 text-sm text-slate-500">{{ t('showcase.noModels') }}</p>
          <button v-if="!showAllModels && filteredModels.length > 12" type="button" class="pressable secondary-button mt-6 min-h-11 rounded-full px-5 py-2.5 text-sm font-semibold text-indigo-700" @click="showAllModels = true">{{ t('showcase.showAllModels', { count: filteredModels.length }) }}</button>
        </div>
      </section>

      <section class="cta-section relative overflow-hidden px-5 py-20 sm:px-8"><div class="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-6" data-reveal><div><p class="section-eyebrow">04 / API</p><h2 class="mt-3 text-3xl font-bold tracking-tight text-slate-950">{{ t('showcase.ctaTitle') }}</h2><p class="mt-3 text-sm text-slate-600">{{ t('showcase.ctaDescription') }}</p></div><router-link :to="entryPath" class="pressable primary-button inline-flex min-h-12 items-center rounded-full px-6 py-3 text-sm font-semibold text-white">{{ isAuthenticated ? t('home.goToDashboard') : t('showcase.start') }} <span aria-hidden="true" class="ml-2">→</span></router-link></div></section>
    </main>
    <footer class="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-3 px-5 py-7 text-xs text-slate-500 sm:px-8"><span>© {{ new Date().getFullYear() }} {{ siteName }}</span><span>{{ t('showcase.footer') }}</span></footer>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import { getHomeCatalog, type HomeCatalogGroup, type HomeCatalogModel } from '@/api/homeCatalog'
import { sanitizeUrl } from '@/utils/url'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import codexIcon from '@/assets/client-icons/codex.svg'
import claudeCodeIcon from '@/assets/client-icons/claude-code.svg'
import openCodeIcon from '@/assets/client-icons/opencode.svg'
import workBuddyIcon from '@/assets/client-icons/workbuddy.svg'
import codeBuddyIcon from '@/assets/client-icons/codebuddy.svg'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const rootEl = ref<HTMLElement | null>(null)
const scrollProgressEl = ref<HTMLElement | null>(null)
const groups = ref<HomeCatalogGroup[]>([])
const loading = ref(true)
const error = ref(false)
const search = ref('')
const selectedPlatform = ref('all')
const showAllModels = ref(false)
const copyState = ref<'idle' | 'copied' | 'error'>('idle')
let requestController: AbortController | undefined
let revealObserver: IntersectionObserver | undefined
let copyTimer: number | undefined
let scrollFrame = 0

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
const featuredModel = computed(() => uniqueModels.value[0]?.name || 'YOUR_MODEL')
const filteredGroups = computed(() => {
  const query = search.value.toLowerCase()
  return groups.value.filter(group => !query || [group.name, group.description, group.platform, ...group.models.map(model => model.name)].some(value => value.toLowerCase().includes(query)))
})
const filteredModels = computed(() => uniqueModels.value.filter(model => selectedPlatform.value === 'all' || model.platform === selectedPlatform.value))
const modelPlatforms = computed(() => [...new Set(uniqueModels.value.map(model => model.platform))].sort())
const visibleModels = computed(() => showAllModels.value ? filteredModels.value : filteredModels.value.slice(0, 12))
const activeFilterClass = 'bg-indigo-600 text-white shadow-sm'
const inactiveFilterClass = 'border border-slate-200 bg-white text-slate-600 hover:border-indigo-200 hover:text-indigo-700'
const clients = [
  { name: 'Codex', icon: codexIcon, iconClass: 'h-7 w-7', descriptionKey: 'showcase.clients.codex' },
  { name: 'Claude Code', icon: claudeCodeIcon, iconClass: 'h-7 w-7', descriptionKey: 'showcase.clients.claude' },
  { name: 'OpenCode', icon: openCodeIcon, iconClass: 'h-7 w-7', descriptionKey: 'showcase.clients.opencode' },
  { name: 'WorkBuddy', icon: workBuddyIcon, iconClass: 'h-9 w-9', descriptionKey: 'showcase.clients.workbuddy' },
  { name: 'CodeBuddy IDE', icon: codeBuddyIcon, iconClass: 'h-7 w-7', descriptionKey: 'showcase.clients.codebuddy' },
]

const codeSnippet = [
  'curl -X POST "API_BASE_URL/v1/responses" \\',
  '  -H "Authorization: Bearer YOUR_API_KEY" \\',
  '  -H "Content-Type: application/json" \\',
  "  -d '{\"model\":\"YOUR_MODEL\",\"input\":\"Hello, world\"}'",
].join('\n')

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
async function copySnippet() {
  try {
    await navigator.clipboard.writeText(codeSnippet)
    copyState.value = 'copied'
  } catch {
    copyState.value = 'error'
  }
  if (copyTimer) window.clearTimeout(copyTimer)
  copyTimer = window.setTimeout(() => { copyState.value = 'idle' }, 2000)
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
function observeRevealTargets() {
  if (!rootEl.value || !revealObserver) return
  rootEl.value.querySelectorAll<HTMLElement>('[data-reveal]:not(.reveal-observed)').forEach(target => {
    target.classList.add('reveal-observed')
    revealObserver?.observe(target)
  })
}
function initReveal() {
  if (!rootEl.value || typeof IntersectionObserver === 'undefined' || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return
  rootEl.value.classList.add('reveal-enabled')
  revealObserver = new IntersectionObserver(entries => {
    for (const entry of entries) {
      if (entry.isIntersecting) {
        entry.target.classList.add('is-revealed')
        revealObserver?.unobserve(entry.target)
      }
    }
  }, { threshold: 0.08, rootMargin: '0px 0px -40px 0px' })
  observeRevealTargets()
}
function updateScrollProgress() {
  if (!rootEl.value || !scrollProgressEl.value) return
  const scrolled = Math.max(0, window.scrollY - rootEl.value.offsetTop)
  const maxScroll = Math.max(1, rootEl.value.scrollHeight - window.innerHeight)
  scrollProgressEl.value.style.transform = `scaleX(${Math.min(1, scrolled / maxScroll)})`
  rootEl.value.style.setProperty('--hero-parallax', `${-Math.min(scrolled * 0.08, 64)}px`)
}
function queueScrollProgress() {
  if (scrollFrame) return
  scrollFrame = window.requestAnimationFrame(() => {
    scrollFrame = 0
    updateScrollProgress()
  })
}
watch([filteredGroups, visibleModels], async () => {
  await nextTick()
  observeRevealTargets()
  queueScrollProgress()
})
onMounted(() => {
  loadCatalog()
  initReveal()
  updateScrollProgress()
  window.addEventListener('scroll', queueScrollProgress, { passive: true })
  window.addEventListener('resize', queueScrollProgress)
})
onUnmounted(() => {
  requestController?.abort()
  revealObserver?.disconnect()
  window.removeEventListener('scroll', queueScrollProgress)
  window.removeEventListener('resize', queueScrollProgress)
  if (scrollFrame) window.cancelAnimationFrame(scrollFrame)
  if (copyTimer) window.clearTimeout(copyTimer)
})
</script>

<style scoped>
.credit-home { --primary: #6466e9; --muted: #667085; --line: #e9eaf2; background: #fff; color: #14151d; }
.scroll-progress { background: linear-gradient(90deg, #6466e9, #a49bff); transform: scaleX(0); box-shadow: 0 0 10px rgba(100, 102, 233, .22); }
.scroll-cue span { display: inline-block; animation: cue-float 2.8s ease-in-out infinite; }
.nav-link:hover { color: var(--primary); }
.primary-button { background: var(--primary); box-shadow: 0 8px 20px rgba(100, 102, 233, .18); transition: transform .2s ease, background .2s ease, box-shadow .2s ease; }
.primary-button:hover { background: #5658dc; box-shadow: 0 12px 28px rgba(100, 102, 233, .24); transform: translateY(-2px); }
.secondary-button { background: #f2f3f7; transition: transform .2s ease, background .2s ease; }
.secondary-button:hover { background: #e9eaf2; transform: translateY(-2px); }
.pressable:active { transform: scale(.96); }
.hero-section { background: radial-gradient(ellipse at 82% 60%, #f4f1ff 0%, rgba(255,255,255,0) 52%); }
.ambient { position: absolute; z-index: -1; width: 28rem; height: 28rem; border-radius: 50%; filter: blur(85px); opacity: .32; animation: ambient-drift 12s ease-in-out infinite alternate; pointer-events: none; }
.ambient-blue { right: 13%; bottom: 1%; background: #a6c9ff; }
.ambient-violet { right: -8%; top: 0; background: #d6b8ff; animation-delay: -5s; }
.preview-halo { position: absolute; inset: -2rem; z-index: -1; border-radius: 50%; background: radial-gradient(ellipse, rgba(119, 130, 245, .22), transparent 66%); filter: blur(18px); transform: translateY(var(--hero-parallax, 0px)); }
.preview-card { min-height: 18rem; transition: transform .5s ease, box-shadow .5s ease; }
.preview-card:hover { transform: scale(1.02); box-shadow: 0 36px 75px rgba(83, 81, 174, .18); }
.floating-count { animation: float-slow 4.5s ease-in-out infinite; }
.benefit-dot { display: inline-flex; width: 1.35rem; height: 1.35rem; flex: none; align-items: center; justify-content: center; border-radius: 50%; font-size: 11px; font-style: normal; }
.developer-section { background: linear-gradient(120deg, #fbfaff, #f7f4ff 55%, #fff); }
.developer-glow { position: absolute; width: 40rem; height: 40rem; left: 15%; top: 0; border-radius: 50%; background: rgba(164, 138, 245, .08); filter: blur(90px); pointer-events: none; }
.code-window { transition: transform .5s ease, box-shadow .5s ease; }
.code-window:hover { transform: translateY(-4px); box-shadow: 0 28px 65px rgba(37, 32, 72, .26); }
.copy-button:active { transform: scale(.94); }
.section-eyebrow { color: var(--primary); font-size: .75rem; font-weight: 700; letter-spacing: .15em; text-transform: uppercase; }
.client-item { transition: transform .25s ease, border-color .25s ease, box-shadow .25s ease; }
.client-item:hover { transform: translateY(-3px); border-color: #c9c9fb; box-shadow: 0 10px 22px rgba(100, 102, 233, .08); }
.client-item:hover .client-symbol { background: #e7e8ff; }
.catalog-card { box-shadow: 0 4px 20px rgba(31, 36, 76, .04); transition: transform .25s ease, border-color .25s ease, box-shadow .25s ease; }
.catalog-card:hover { transform: translateY(-4px); border-color: #cccdf9; box-shadow: 0 16px 35px rgba(54, 55, 117, .1); }
.models-section { background: #f8f8fc; }
.model-card { box-shadow: 0 4px 18px rgba(31, 36, 76, .035); transition: transform .25s ease, box-shadow .25s ease; }
.model-card:hover { transform: translateY(-3px); box-shadow: 0 12px 26px rgba(54, 55, 117, .1); }
.cta-section { background: linear-gradient(110deg, #f7f5ff, #eef2ff); }
.reveal-enabled [data-reveal] { opacity: 0; transform: translateY(28px); transition: opacity .75s ease, transform .75s cubic-bezier(.16,1,.3,1); }
.reveal-enabled [data-reveal].is-revealed { opacity: 1; transform: translateY(0); }
.reveal-enabled .catalog-card.is-revealed:hover, .reveal-enabled .client-item.is-revealed:hover, .reveal-enabled .model-card.is-revealed:hover { transform: translateY(-4px); }
@keyframes float-slow { 50% { transform: translateY(-12px); } }
@keyframes ambient-drift { to { transform: translate(30px, 20px) scale(1.1); } }
@keyframes cue-float { 50% { transform: translateY(5px); } }
@media (max-width: 640px) { .ambient { width: 18rem; height: 18rem; opacity: .22; } .hero-section { min-height: auto; } }
@media (prefers-reduced-motion: reduce) { .credit-home *, .credit-home *::before, .credit-home *::after { animation: none !important; transition-duration: .01ms !important; scroll-behavior: auto !important; } .reveal-enabled [data-reveal] { opacity: 1; transform: none; } }
</style>
