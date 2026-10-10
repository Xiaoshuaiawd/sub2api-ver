<template>
  <div data-testid="showcase-home" class="showcase-shell min-h-screen overflow-hidden text-slate-100">
    <div class="hero-stage relative isolate overflow-hidden text-white">
      <div aria-hidden="true" class="tech-grid pointer-events-none absolute inset-0"></div>
      <div aria-hidden="true" class="hero-beam pointer-events-none absolute inset-0"></div>
      <div aria-hidden="true" class="hero-aurora hero-aurora-cyan pointer-events-none"></div>
      <div aria-hidden="true" class="hero-aurora hero-aurora-violet pointer-events-none"></div>
      <div aria-hidden="true" class="hero-scan pointer-events-none absolute inset-0"></div>

      <header class="relative z-20 border-b border-cyan-300/10 bg-[#050c1b]/55 backdrop-blur-xl">
        <nav class="mx-auto flex max-w-7xl items-center justify-between gap-2 px-5 py-4 sm:gap-4 sm:px-8">
          <router-link to="/home" class="flex min-w-0 flex-1 items-center gap-2 sm:gap-3" :aria-label="siteName">
            <span class="brand-mark"><img :src="siteLogo || '/logo.svg'" :alt="siteName" class="h-9 w-9 rounded-lg object-contain" /></span>
            <span class="min-w-0 truncate text-base font-bold tracking-tight sm:text-lg">{{ siteName }}<span class="ml-2 hidden font-mono text-[10px] font-normal tracking-[0.22em] text-cyan-300/70 md:inline">// API GATEWAY</span></span>
          </router-link>
          <div class="flex shrink-0 items-center gap-2 sm:gap-4">
            <a href="#clients" class="hidden text-sm text-slate-300 transition hover:text-cyan-300 sm:inline">{{ t('showcase.navClients') }}</a>
            <a href="#groups" class="hidden text-sm text-slate-300 transition hover:text-cyan-300 sm:inline">{{ t('showcase.navGroups') }}</a>
            <a href="#models" class="hidden text-sm text-slate-300 transition hover:text-cyan-300 sm:inline">{{ t('showcase.navModels') }}</a>
            <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="hidden text-sm text-slate-300 transition hover:text-cyan-300 md:inline">{{ t('home.docs') }}</a>
            <LocaleSwitcher />
            <router-link :to="entryPath" class="nav-cta inline-flex min-h-10 items-center rounded-lg px-3 py-2 text-sm font-semibold text-[#03111d] sm:px-4">
              {{ isAuthenticated ? t('home.dashboard') : t('showcase.start') }} <span aria-hidden="true" class="ml-2">↗</span>
            </router-link>
          </div>
        </nav>
      </header>

      <section class="relative z-10 mx-auto grid max-w-7xl items-center gap-12 px-5 pb-20 pt-16 sm:px-8 lg:grid-cols-[1.02fr_0.98fr] lg:gap-16 lg:pb-32 lg:pt-24">
        <div class="hero-copy">
          <div class="mb-7 inline-flex items-center gap-2 rounded-full border border-cyan-300/30 bg-cyan-300/[0.07] px-3 py-2 font-mono text-[11px] font-semibold tracking-[0.16em] text-cyan-200">
            <span class="status-pulse h-2 w-2 rounded-full bg-emerald-300"></span>{{ t('showcase.eyebrow') }}<span class="cursor-block" aria-hidden="true">_</span>
          </div>
          <h1 class="max-w-3xl text-4xl font-bold leading-[1.14] tracking-tight sm:text-5xl lg:text-[4.25rem]">
            {{ t('showcase.heroLead') }}<span class="hero-gradient block">{{ t('showcase.heroAccent') }}</span>
          </h1>
          <p class="mt-7 max-w-2xl text-base leading-8 text-slate-200 sm:text-lg">{{ siteSubtitle }}</p>
          <p class="mt-3 max-w-2xl text-sm leading-7 text-slate-400">{{ t('showcase.heroDescription') }}</p>
          <div class="mt-9 flex flex-wrap gap-3">
            <router-link :to="entryPath" class="primary-cta inline-flex min-h-12 items-center gap-2 rounded-xl px-6 py-3 text-sm font-bold text-[#03111d]">
              {{ isAuthenticated ? t('home.goToDashboard') : t('showcase.start') }} <span aria-hidden="true">↗</span>
            </router-link>
            <a href="#groups" class="inline-flex min-h-12 items-center rounded-xl border border-cyan-300/30 bg-cyan-300/[0.04] px-6 py-3 text-sm font-semibold text-cyan-100 transition hover:border-cyan-300/70 hover:bg-cyan-300/10">{{ t('showcase.exploreGroups') }}</a>
          </div>
          <div class="mt-11 flex flex-wrap gap-x-6 gap-y-3 border-t border-cyan-200/10 pt-6 font-mono text-[11px] tracking-wide text-slate-400">
            <span><span class="mr-2 text-cyan-400">//</span>{{ t('showcase.liveCatalog') }}</span>
            <span><span class="mr-2 text-cyan-400">//</span>{{ t('showcase.permissionAware') }}</span>
            <span><span class="mr-2 text-cyan-400">//</span>{{ t('showcase.clientFriendly') }}</span>
          </div>
        </div>

        <div class="terminal-scene relative mx-auto w-full max-w-xl lg:ml-auto">
          <div aria-hidden="true" class="orbit orbit-one"></div>
          <div aria-hidden="true" class="orbit orbit-two"></div>
          <div aria-hidden="true" class="floating-signal signal-one">API / PREVIEW</div>
          <div aria-hidden="true" class="floating-signal signal-two">01 → 0∞</div>
          <div class="console-frame relative overflow-hidden rounded-2xl border border-cyan-300/25 bg-[#071328]/90 shadow-[0_25px_100px_rgba(0,0,0,0.5)] backdrop-blur-xl">
            <div aria-hidden="true" class="console-tracer"></div>
            <div class="flex items-center justify-between border-b border-cyan-200/10 bg-white/[0.03] px-5 py-4 font-mono text-[11px] text-slate-400">
              <span class="flex items-center gap-2"><i class="h-2 w-2 rounded-full bg-rose-400/80"></i><i class="h-2 w-2 rounded-full bg-amber-300/80"></i><i class="h-2 w-2 rounded-full bg-emerald-300/80"></i><span class="ml-3 text-cyan-200">gateway.request</span></span>
              <span>DEMO / JSON</span>
            </div>
            <div class="px-5 pb-5 pt-6 font-mono text-xs leading-7 sm:px-7 sm:text-sm">
              <div class="code-line"><span class="code-number">01</span><span class="text-violet-300">POST</span><span class="ml-3 text-slate-200">/v1/responses</span></div>
              <div class="code-line"><span class="code-number">02</span><span class="text-slate-500">Authorization:</span><span class="ml-2 text-emerald-300">Bearer ••••••••</span></div>
              <div class="code-line mt-4"><span class="code-number">03</span><span class="text-slate-500">{</span></div>
              <div class="code-line"><span class="code-number">04</span><span class="pl-4 text-cyan-300">"model"</span><span class="text-slate-300">: </span><span class="break-all text-amber-200">"{{ featuredModel }}"</span><span class="text-slate-300">,</span></div>
              <div class="code-line"><span class="code-number">05</span><span class="pl-4 text-cyan-300">"input"</span><span class="text-slate-300">: </span><span class="text-emerald-200">"Build something brilliant"</span></div>
              <div class="code-line"><span class="code-number">06</span><span class="text-slate-500">}</span><span class="cursor-block ml-1 text-cyan-300" aria-hidden="true">▍</span></div>
            </div>
            <div class="mx-5 mb-5 rounded-xl border border-cyan-300/15 bg-cyan-300/[0.04] px-4 py-4 sm:mx-7">
              <div class="mb-3 flex items-center justify-between font-mono text-[10px] uppercase tracking-[0.18em] text-cyan-200"><span>{{ t('showcase.catalogTitle') }}</span><span class="flex items-center gap-1.5 text-emerald-300"><span class="status-pulse h-1.5 w-1.5 rounded-full bg-emerald-300"></span>SYNC</span></div>
              <div class="grid grid-cols-3 gap-3 font-mono"><div><div class="text-xl font-bold text-white">{{ loading ? '—' : groups.length }}</div><div class="text-[10px] text-slate-400">{{ t('showcase.groupsCount') }}</div></div><div><div class="text-xl font-bold text-white">{{ loading ? '—' : uniqueModels.length }}</div><div class="text-[10px] text-slate-400">{{ t('showcase.modelsCount') }}</div></div><div><div class="text-xl font-bold text-white">{{ loading ? '—' : activePlatforms.length }}</div><div class="text-[10px] text-slate-400">{{ t('showcase.activePlatforms') }}</div></div></div>
            </div>
            <div class="flex flex-wrap items-center gap-2 border-t border-cyan-200/10 px-5 py-4 font-mono text-[10px] text-slate-400 sm:px-7">
              <span class="text-cyan-300">&gt;_</span>
              <span v-for="platform in activePlatforms.slice(0, 4)" :key="platform" class="rounded border border-cyan-200/15 px-2 py-1 text-cyan-100">{{ platformLabel(platform) }}</span>
              <span v-if="!loading && activePlatforms.length === 0">{{ t('showcase.noPlatforms') }}</span>
              <span v-if="activePlatforms.length > 4">+{{ activePlatforms.length - 4 }}</span>
            </div>
          </div>
        </div>
      </section>
    </div>

    <div aria-hidden="true" class="client-ticker overflow-hidden border-y border-cyan-300/10 py-4 font-mono text-[11px] uppercase tracking-[0.24em] text-cyan-200/60">
      <div class="ticker-track"><span v-for="n in 2" :key="n" class="ticker-set"><span>CODEX</span><b>✦</b><span>CLAUDE CODE</span><b>✦</b><span>OPENCODE</span><b>✦</b><span>WORKBUDDY</span><b>✦</b><span>CODEBUDDY IDE</span><b>✦</b><span>ONE API / MANY MODELS</span><b>✦</b></span></div>
    </div>

    <main>
      <section id="clients" class="relative mx-auto max-w-7xl scroll-mt-8 px-5 py-20 sm:px-8 lg:py-24">
        <div class="mb-9 flex flex-wrap items-end justify-between gap-4">
          <div><p class="section-kicker">01 // CLIENT INTERFACE</p><h2 class="mt-3 text-3xl font-bold tracking-tight text-white sm:text-4xl">{{ t('showcase.clientsTitle') }}</h2><p class="mt-3 max-w-2xl text-sm leading-7 text-slate-400">{{ t('showcase.clientsDescription') }}</p></div>
        </div>
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
          <div v-for="client in clients" :key="client.name" class="client-card group relative overflow-hidden rounded-2xl border border-cyan-200/10 bg-[#0c1830]/85 p-5 transition duration-300 hover:-translate-y-2 hover:border-cyan-300/45">
            <div aria-hidden="true" class="client-card-glow"></div>
            <div class="relative flex h-12 w-12 items-center justify-center rounded-xl border border-cyan-300/20 bg-cyan-300/[0.08] font-mono text-sm font-bold tracking-tight text-cyan-200">{{ client.initial }}</div>
            <h3 class="relative mt-6 text-base font-bold text-white">{{ client.name }}</h3>
            <p class="relative mt-2 min-h-12 text-sm leading-6 text-slate-400">{{ t(client.descriptionKey) }}</p>
            <div class="relative mt-5 border-t border-cyan-200/10 pt-4 font-mono text-[10px] font-medium tracking-wide text-cyan-300">{{ t('showcase.customApi') }} <span aria-hidden="true" class="ml-1">↗</span></div>
          </div>
        </div>
        <p class="mt-6 border-l-2 border-cyan-300/40 pl-3 text-xs leading-6 text-slate-400">{{ t('showcase.clientNote') }}</p>
      </section>

      <section id="groups" class="group-section relative scroll-mt-8 border-y border-cyan-300/10 py-20 lg:py-24">
        <div aria-hidden="true" class="section-grid pointer-events-none absolute inset-0"></div>
        <div class="mx-auto max-w-7xl px-5 sm:px-8">
          <div class="relative flex flex-wrap items-end justify-between gap-5">
            <div><p class="section-kicker">02 // GROUP ROUTING</p><h2 class="mt-3 text-3xl font-bold tracking-tight text-white sm:text-4xl">{{ t('showcase.groupsTitle') }}</h2><p class="mt-3 text-sm text-slate-400">{{ t('showcase.groupsDescription') }}</p></div>
            <div class="relative w-full sm:w-72"><label for="showcase-search" class="sr-only">{{ t('showcase.searchPlaceholder') }}</label><span aria-hidden="true" class="absolute left-4 top-1/2 -translate-y-1/2 font-mono text-cyan-400">&gt;_</span><input id="showcase-search" v-model.trim="search" type="search" class="w-full rounded-xl border border-cyan-300/20 bg-[#071629] py-3 pl-12 pr-4 text-sm text-white outline-none transition placeholder:text-slate-500 focus:border-cyan-300/70 focus:ring-2 focus:ring-cyan-300/15" :placeholder="t('showcase.searchPlaceholder')" /></div>
          </div>
          <div v-if="loading" class="relative mt-8 grid gap-4 md:grid-cols-2 lg:grid-cols-3" aria-live="polite"><div v-for="n in 3" :key="n" class="h-56 animate-pulse rounded-2xl bg-cyan-300/[0.06]"></div></div>
          <div v-else-if="error" class="relative mt-8 rounded-2xl border border-rose-400/30 bg-rose-400/10 p-8 text-center" role="alert"><p class="text-sm text-rose-200">{{ t('showcase.loadError') }}</p><button type="button" class="mt-4 rounded-lg bg-rose-500 px-4 py-2 text-sm font-semibold text-white" @click="loadCatalog">{{ t('showcase.retry') }}</button></div>
          <div v-else-if="filteredGroups.length === 0" class="relative mt-8 rounded-2xl border border-dashed border-cyan-300/20 p-10 text-center text-sm text-slate-400">{{ search ? t('showcase.noSearchResults') : t('showcase.noGroups') }}</div>
          <div v-else class="relative mt-8 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            <article v-for="group in filteredGroups" :key="group.id" class="group-card flex min-w-0 flex-col rounded-2xl border border-cyan-300/15 bg-[#0a1830]/95 p-6 transition duration-300 hover:-translate-y-1 hover:border-cyan-300/45">
              <div class="flex items-start justify-between gap-3"><span class="rounded-md border border-cyan-300/20 bg-cyan-300/[0.08] px-3 py-1.5 font-mono text-[11px] font-semibold text-cyan-200">{{ platformLabel(group.platform) }}</span><span class="rounded-full border border-violet-300/20 px-2.5 py-1 text-[11px] font-medium text-violet-200">{{ billingLabel(group.subscription_type) }}</span></div>
              <h3 class="mt-5 break-words text-lg font-bold text-white">{{ group.name }}</h3>
              <p class="mt-2 min-h-12 text-sm leading-6 text-slate-400">{{ group.description || t('showcase.groupFallback') }}</p>
              <div class="mt-5 flex flex-wrap gap-2"><span v-for="model in group.models.slice(0, 4)" :key="`${model.platform}:${model.name}`" class="max-w-full truncate rounded-md bg-cyan-100/[0.06] px-2.5 py-1 font-mono text-[11px] font-medium text-slate-300" :title="model.name">{{ model.name }}</span><span v-if="group.models.length > 4" class="px-1 py-1 text-[11px] text-cyan-300">+{{ group.models.length - 4 }}</span><span v-if="group.models.length === 0" class="text-xs text-slate-400">{{ t('showcase.modelsPending') }}</span></div>
              <div class="mt-auto flex items-center justify-between border-t border-cyan-200/10 pt-5 font-mono text-[11px] text-slate-400"><span>{{ t('showcase.groupModelCount', { count: group.models.length }) }}</span><span v-if="group.is_exclusive" class="text-amber-300">{{ t('showcase.exclusive') }}</span></div>
            </article>
          </div>
        </div>
      </section>

      <section id="models" class="mx-auto max-w-7xl scroll-mt-8 px-5 py-20 sm:px-8 lg:py-24">
        <p class="section-kicker">03 // MODEL INDEX</p>
        <div class="mt-3 flex flex-wrap items-end justify-between gap-4"><div><h2 class="text-3xl font-bold tracking-tight text-white sm:text-4xl">{{ t('showcase.modelsTitle') }}</h2><p class="mt-3 text-sm text-slate-400">{{ t('showcase.modelsDescription') }}</p></div><span class="rounded-full border border-cyan-300/20 bg-cyan-300/[0.07] px-3 py-1.5 font-mono text-xs font-semibold text-cyan-200">{{ uniqueModels.length }} {{ t('showcase.modelsCount') }}</span></div>
        <div v-if="!loading && !error && uniqueModels.length" class="mt-8 flex flex-wrap gap-2">
          <button type="button" class="min-h-11 rounded-full px-4 py-2.5 text-xs font-medium transition" :class="selectedPlatform === 'all' ? activeFilterClass : inactiveFilterClass" @click="selectedPlatform = 'all'; showAllModels = false">{{ t('showcase.allPlatforms') }}</button>
          <button v-for="platform in modelPlatforms" :key="platform" type="button" class="min-h-11 rounded-full px-4 py-2.5 text-xs font-medium transition" :class="selectedPlatform === platform ? activeFilterClass : inactiveFilterClass" @click="selectedPlatform = platform; showAllModels = false">{{ platformLabel(platform) }}</button>
        </div>
        <div v-if="!loading && !error && filteredModels.length" class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div v-for="model in visibleModels" :key="`${model.platform}:${model.name}`" class="model-card min-w-0 rounded-xl border border-cyan-300/15 bg-[#0b1930] px-4 py-4"><div class="mb-3 font-mono text-xs text-cyan-300">&lt;/&gt;</div><div class="truncate text-sm font-semibold text-white" :title="model.name">{{ model.name }}</div><div class="mt-1 font-mono text-[11px] text-slate-400">{{ platformLabel(model.platform) }}</div></div>
        </div>
        <p v-if="!loading && !error && !filteredModels.length" class="mt-8 text-sm text-slate-400">{{ t('showcase.noModels') }}</p>
        <button v-if="!showAllModels && filteredModels.length > 12" type="button" class="mt-6 rounded-xl border border-cyan-300/25 bg-cyan-300/[0.07] px-5 py-2.5 text-sm font-semibold text-cyan-200 transition hover:border-cyan-300/70" @click="showAllModels = true">{{ t('showcase.showAllModels', { count: filteredModels.length }) }}</button>
      </section>

      <section class="cta-stage relative overflow-hidden border-y border-cyan-300/15 px-5 py-16 text-white sm:px-8"><div class="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-6"><div class="relative z-10"><p class="section-kicker">04 // INITIALIZE</p><h2 class="mt-3 text-2xl font-bold sm:text-3xl">{{ t('showcase.ctaTitle') }}</h2><p class="mt-2 text-sm text-slate-300">{{ t('showcase.ctaDescription') }}</p></div><router-link :to="entryPath" class="primary-cta relative z-10 rounded-xl px-6 py-3 text-sm font-bold text-[#03111d]">{{ isAuthenticated ? t('home.goToDashboard') : t('showcase.start') }} ↗</router-link></div></section>
    </main>
    <footer class="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-3 px-5 py-7 font-mono text-[11px] text-slate-500 sm:px-8"><span>© {{ new Date().getFullYear() }} {{ siteName }}</span><span>{{ t('showcase.footer') }}</span></footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import { getHomeCatalog, type HomeCatalogGroup, type HomeCatalogModel } from '@/api/homeCatalog'
import { sanitizeUrl } from '@/utils/url'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const groups = ref<HomeCatalogGroup[]>([])
const loading = ref(true)
const error = ref(false)
const search = ref('')
const selectedPlatform = ref('all')
const showAllModels = ref(false)
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
const featuredModel = computed(() => uniqueModels.value.find(model => model.platform === 'openai')?.name || 'your-model')
const filteredGroups = computed(() => {
  const query = search.value.toLowerCase()
  return groups.value.filter(group => !query || [group.name, group.description, group.platform, ...group.models.map(model => model.name)].some(value => value.toLowerCase().includes(query)))
})
const filteredModels = computed(() => uniqueModels.value.filter(model => selectedPlatform.value === 'all' || model.platform === selectedPlatform.value))
const modelPlatforms = computed(() => [...new Set(uniqueModels.value.map(model => model.platform))].sort())
const visibleModels = computed(() => showAllModels.value ? filteredModels.value : filteredModels.value.slice(0, 12))
const activeFilterClass = 'border border-cyan-300/70 bg-cyan-300/15 text-cyan-200'
const inactiveFilterClass = 'border border-cyan-300/15 bg-[#0b1930] text-slate-400 hover:border-cyan-300/50 hover:text-cyan-200'
const clients = [
  { name: 'Codex', initial: 'CX', descriptionKey: 'showcase.clients.codex' },
  { name: 'Claude Code', initial: 'CC', descriptionKey: 'showcase.clients.claude' },
  { name: 'OpenCode', initial: 'OC', descriptionKey: 'showcase.clients.opencode' },
  { name: 'WorkBuddy', initial: 'WB', descriptionKey: 'showcase.clients.workbuddy' },
  { name: 'CodeBuddy IDE', initial: 'IDE', descriptionKey: 'showcase.clients.codebuddy' },
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

<style scoped>
.showcase-shell {
  background: radial-gradient(ellipse at 82% 42%, #0c1a35 0%, #050b18 52%);
}

.hero-stage {
  background: radial-gradient(ellipse at 66% 42%, #12284b 0%, #071329 39%, #040a17 78%);
}

.tech-grid {
  background-image: linear-gradient(rgba(73, 209, 232, .13) 1px, transparent 1px), linear-gradient(90deg, rgba(73, 209, 232, .13) 1px, transparent 1px);
  background-size: 54px 54px;
  opacity: .38;
  transform: perspective(600px) rotateX(62deg) scale(1.8) translateY(18%);
  transform-origin: 50% 100%;
  mask-image: linear-gradient(to bottom, transparent 4%, black 84%);
  animation: grid-drift 18s linear infinite;
}

.hero-beam {
  background: linear-gradient(115deg, transparent 38%, rgba(70, 212, 240, .08) 46%, rgba(144, 102, 255, .13) 52%, transparent 61%);
  background-size: 220% 100%;
  animation: beam-sweep 13s ease-in-out infinite;
}

.hero-aurora {
  position: absolute;
  width: 36rem;
  height: 36rem;
  border-radius: 50%;
  filter: blur(95px);
  opacity: .25;
  animation: aurora-float 11s ease-in-out infinite alternate;
}

.hero-aurora-cyan { right: -8rem; top: -12rem; background: #18d5e9; }
.hero-aurora-violet { left: 24%; bottom: -24rem; background: #804bdf; animation-delay: -5s; }

.hero-scan {
  background: linear-gradient(transparent 49%, rgba(89, 221, 239, .12) 50%, transparent 51%);
  background-size: 100% 250%;
  opacity: .5;
  animation: scan-pass 10s linear infinite;
}

.brand-mark {
  display: inline-flex;
  padding: 3px;
  border: 1px solid rgba(103, 232, 249, .38);
  border-radius: .75rem;
  background: rgba(103, 232, 249, .08);
  box-shadow: 0 0 22px rgba(44, 199, 226, .16);
}

.nav-cta, .primary-cta {
  background: linear-gradient(105deg, #67e8f9, #9ff6ff 46%, #b6a2ff);
  box-shadow: 0 0 28px rgba(73, 209, 232, .26), inset 0 1px rgba(255, 255, 255, .6);
  transition: transform .25s ease, box-shadow .25s ease, filter .25s ease;
}

.nav-cta:hover, .primary-cta:hover {
  transform: translateY(-2px);
  box-shadow: 0 0 40px rgba(73, 209, 232, .44);
  filter: saturate(1.15);
}

.hero-copy { animation: hero-enter .75s ease-out both; }
.hero-gradient {
  width: fit-content;
  background: linear-gradient(95deg, #67e8f9, #a5b4fc 42%, #e0a8ff 70%, #67e8f9);
  background-size: 230% 100%;
  color: transparent;
  background-clip: text;
  text-shadow: 0 0 48px rgba(80, 199, 245, .15);
  animation: gradient-flow 7s ease-in-out infinite;
}

.status-pulse { box-shadow: 0 0 0 0 rgba(110, 231, 183, .55); animation: status-ping 2.4s ease-out infinite; }
.cursor-block { animation: cursor-blink 1s steps(2, start) infinite; }

.terminal-scene { animation: terminal-enter .9s .1s ease-out both; }
.orbit {
  position: absolute;
  inset: -2.7rem;
  border: 1px dashed rgba(103, 232, 249, .22);
  border-radius: 50%;
  pointer-events: none;
  animation: orbit-spin 38s linear infinite;
}
.orbit::after {
  content: '';
  position: absolute;
  top: 13%; left: 13%;
  width: 7px; height: 7px;
  border-radius: 50%;
  background: #67e8f9;
  box-shadow: 0 0 16px #67e8f9;
}
.orbit-two { inset: -1rem; border-color: rgba(167, 139, 250, .22); animation-direction: reverse; animation-duration: 29s; }
.orbit-two::after { top: 80%; left: 82%; background: #c4b5fd; box-shadow: 0 0 16px #c4b5fd; }

.floating-signal {
  position: absolute;
  z-index: 2;
  border: 1px solid rgba(103, 232, 249, .28);
  border-radius: .5rem;
  background: rgba(8, 27, 46, .92);
  padding: .4rem .7rem;
  color: #a5f3fc;
  font: 10px ui-monospace, SFMono-Regular, monospace;
  letter-spacing: .12em;
  box-shadow: 0 0 24px rgba(73, 209, 232, .2);
  animation: signal-float 5s ease-in-out infinite;
}
.signal-one { right: -1rem; top: -1rem; }
.signal-two { left: -2rem; bottom: 2.5rem; animation-delay: -2.5s; }

.console-frame { box-shadow: 0 24px 90px rgba(0, 0, 0, .46), 0 0 55px rgba(43, 197, 223, .13), inset 0 0 40px rgba(43, 197, 223, .03); }
.console-tracer {
  position: absolute;
  z-index: 2;
  top: 0; left: -40%;
  width: 40%; height: 1px;
  background: linear-gradient(90deg, transparent, #67e8f9, transparent);
  box-shadow: 0 0 14px #67e8f9;
  animation: tracer 5s linear infinite;
}
.code-number { display: inline-block; width: 2.1rem; color: rgba(148, 163, 184, .45); font-size: 10px; user-select: none; }
.code-line { opacity: 0; animation: code-appear .45s ease-out forwards; }
.code-line:nth-child(2) { animation-delay: .15s; }
.code-line:nth-child(3) { animation-delay: .3s; }
.code-line:nth-child(4) { animation-delay: .45s; }
.code-line:nth-child(5) { animation-delay: .6s; }
.code-line:nth-child(6) { animation-delay: .75s; }

.client-ticker { background: #07142a; mask-image: linear-gradient(90deg, transparent, black 8%, black 92%, transparent); }
.ticker-track { display: flex; width: max-content; animation: ticker-scroll 28s linear infinite; }
.ticker-set { display: flex; flex: none; align-items: center; gap: 3rem; padding-right: 3rem; white-space: nowrap; }
.ticker-set b { color: #67e8f9; font-weight: 400; }
.section-kicker { color: #67e8f9; font: 700 11px ui-monospace, SFMono-Regular, monospace; letter-spacing: .22em; }
.client-card { box-shadow: inset 0 1px rgba(255, 255, 255, .035); }
.client-card-glow { position: absolute; inset: -1px; opacity: 0; background: radial-gradient(circle at 20% 10%, rgba(75, 210, 238, .2), transparent 55%); transition: opacity .3s ease; pointer-events: none; }
.client-card:hover .client-card-glow { opacity: 1; }
.client-card:nth-child(2) .client-card-glow { background: radial-gradient(circle at 20% 10%, rgba(251, 146, 60, .15), transparent 55%); }
.client-card:nth-child(4) .client-card-glow { background: radial-gradient(circle at 20% 10%, rgba(167, 139, 250, .2), transparent 55%); }
.group-section { background: linear-gradient(155deg, #09182f, #071223 58%, #0d1732); }
.section-grid { background-image: linear-gradient(rgba(103, 232, 249, .035) 1px, transparent 1px), linear-gradient(90deg, rgba(103, 232, 249, .035) 1px, transparent 1px); background-size: 44px 44px; mask-image: linear-gradient(to bottom, transparent, black 28%, black 78%, transparent); }
.group-card { box-shadow: inset 0 1px rgba(255, 255, 255, .035); }
.group-card:hover { box-shadow: 0 0 35px rgba(53, 201, 223, .09), inset 0 1px rgba(255, 255, 255, .05); }
.model-card { transition: transform .25s ease, border-color .25s ease, box-shadow .25s ease; }
.model-card:hover { transform: translateY(-3px); border-color: rgba(103, 232, 249, .5); box-shadow: 0 0 25px rgba(53, 201, 223, .09); }
.cta-stage { background: radial-gradient(ellipse at 78% 52%, #173f65, #102346 42%, #07142a 76%); }
.cta-stage::before { content: ''; position: absolute; inset: 0; background: linear-gradient(105deg, transparent 35%, rgba(103, 232, 249, .1) 50%, transparent 65%); background-size: 220% 100%; animation: beam-sweep 11s linear infinite; }

@keyframes grid-drift { to { background-position: 0 54px, 54px 0; } }
@keyframes beam-sweep { 0%, 15% { background-position: 100% 0; } 70%, 100% { background-position: -100% 0; } }
@keyframes aurora-float { to { transform: translate(70px, 40px) scale(1.18); } }
@keyframes scan-pass { to { background-position: 0 250%; } }
@keyframes gradient-flow { 50% { background-position: 100% 0; } }
@keyframes status-ping { 70%, 100% { box-shadow: 0 0 0 8px rgba(110, 231, 183, 0); } }
@keyframes cursor-blink { to { opacity: 0; } }
@keyframes terminal-enter { from { opacity: 0; transform: translateY(25px) scale(.97); } to { opacity: 1; transform: translateY(0) scale(1); } }
@keyframes hero-enter { from { opacity: 0; transform: translateY(20px); } to { opacity: 1; transform: translateY(0); } }
@keyframes orbit-spin { to { transform: rotate(360deg); } }
@keyframes signal-float { 50% { transform: translateY(-8px); } }
@keyframes tracer { to { left: 110%; } }
@keyframes code-appear { to { opacity: 1; } }
@keyframes ticker-scroll { to { transform: translateX(-50%); } }

@media (max-width: 640px) {
  .orbit, .floating-signal { display: none; }
  .tech-grid { opacity: .2; }
  .hero-aurora { width: 22rem; height: 22rem; }
}

@media (prefers-reduced-motion: reduce) {
  .showcase-shell *, .showcase-shell *::before, .showcase-shell *::after { animation: none !important; transition-duration: .01ms !important; scroll-behavior: auto !important; }
  .code-line { opacity: 1; }
  .client-card:hover, .group-card:hover, .model-card:hover, .nav-cta:hover, .primary-cta:hover { transform: none; }
}
</style>
