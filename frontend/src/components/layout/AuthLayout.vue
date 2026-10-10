<template>
  <div class="auth-stage relative min-h-dvh" :class="{ 'auth-split': isEntryPage }">
    <aside v-if="isEntryPage" class="auth-showcase relative hidden min-h-dvh overflow-hidden lg:flex">
      <div class="showcase-orb showcase-orb-top" aria-hidden="true"></div>
      <div class="showcase-orb showcase-orb-bottom" aria-hidden="true"></div>
      <div class="relative z-10 flex w-full flex-col justify-between px-10 py-10 xl:px-16 xl:py-12">
        <router-link to="/home" class="showcase-brand flex w-fit items-center gap-3">
          <img :src="siteLogo || '/logo.svg'" :alt="siteName" class="h-11 w-11 rounded-xl object-contain" />
          <span class="truncate text-xl font-bold tracking-tight">{{ siteName }}</span>
        </router-link>

        <div class="showcase-content max-w-[580px]">
          <p class="mb-5 text-xs font-semibold uppercase tracking-[.2em] text-indigo-600">AI API FOR BUILDERS</p>
          <h1 class="whitespace-pre-line text-[2.6rem] font-bold leading-[1.18] tracking-tight text-slate-950 xl:text-[3.15rem]">{{ showcaseTitle }}</h1>
          <p class="mt-6 max-w-lg text-base leading-8 text-slate-600">{{ showcaseDescription }}</p>

          <div class="showcase-code relative mt-11 overflow-hidden rounded-[1.4rem] border border-white/10 bg-[#151827] shadow-[0_28px_60px_rgba(30,30,80,.23)]">
            <div class="flex items-center justify-between border-b border-white/10 px-5 py-4">
              <span class="flex gap-1.5" aria-hidden="true"><i class="h-2.5 w-2.5 rounded-full bg-rose-400"></i><i class="h-2.5 w-2.5 rounded-full bg-amber-300"></i><i class="h-2.5 w-2.5 rounded-full bg-emerald-400"></i></span>
              <span class="font-mono text-[11px] text-slate-400">request.json · {{ t('auth.showcaseExample') }}</span>
            </div>
            <div class="showcase-code-body px-6 py-6 font-mono text-[13px] leading-7">
              <p><span class="text-violet-300">POST</span> <span class="text-slate-200">/v1/responses</span></p>
              <p class="mt-3 text-slate-500">{</p>
              <p class="pl-5"><span class="text-sky-300">"model"</span><span class="text-slate-300">: </span><span class="text-emerald-300">"your-model"</span><span class="text-slate-300">,</span></p>
              <p class="pl-5"><span class="text-sky-300">"input"</span><span class="text-slate-300">: </span><span class="text-emerald-300">"Build something new"</span></p>
              <p class="text-slate-500">}</p>
            </div>
          </div>
        </div>

        <div class="showcase-clients flex flex-wrap items-center gap-2 text-xs text-slate-500">
          <span class="mr-2 font-medium text-slate-600">{{ t('auth.showcaseTools') }}</span>
          <span v-for="client in ['Codex', 'Claude Code', 'OpenCode']" :key="client" class="rounded-full border border-indigo-200/70 bg-white/60 px-3 py-1.5">{{ client }}</span>
        </div>
      </div>
    </aside>

    <div class="auth-main relative isolate min-h-dvh min-w-0 px-4 py-5 sm:px-6 sm:py-7">
      <div class="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
        <div class="auth-glow auth-glow-blue"></div>
        <div class="auth-glow auth-glow-violet"></div>
      </div>

      <div class="relative z-10 mx-auto flex max-w-6xl items-center justify-between">
        <router-link to="/home" class="auth-back inline-flex min-h-11 items-center gap-2 rounded-full px-3 text-sm font-medium">
          <span aria-hidden="true">←</span>{{ t('auth.backHome') }}
        </router-link>
        <LocaleSwitcher />
      </div>

      <div class="auth-inner relative z-10 mx-auto flex min-h-[calc(100dvh-100px)] w-full max-w-6xl items-center justify-center py-8 sm:py-12">
        <div class="auth-content w-full max-w-[490px]">
          <div class="auth-brand mb-8 text-center">
            <template v-if="settingsLoaded">
              <router-link to="/home" class="inline-flex items-center justify-center gap-3">
                <img :src="siteLogo || '/logo.svg'" :alt="siteName" class="h-12 w-12 rounded-xl object-contain" />
                <span class="auth-brand-name text-2xl font-bold tracking-tight sm:text-[1.7rem]">{{ siteName }}</span>
              </router-link>
              <p class="auth-subtitle mt-3 text-sm leading-6">{{ siteSubtitle }}</p>
            </template>
          </div>

          <div class="auth-card rounded-[1.75rem] border p-6 backdrop-blur-xl sm:p-9">
            <slot />
          </div>

          <div class="auth-footer mt-6 text-center text-sm"><slot name="footer" /></div>
          <p class="auth-copyright mt-10 text-center text-xs">&copy; {{ currentYear }} {{ siteName }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'

const props = withDefaults(defineProps<{ page?: 'default' | 'login' | 'register' }>(), { page: 'default' })
const { t } = useI18n()
const appStore = useAppStore()
const isEntryPage = computed(() => props.page === 'login' || props.page === 'register')
const showcaseTitle = computed(() => t(props.page === 'register' ? 'auth.showcaseRegisterTitle' : 'auth.showcaseLoginTitle'))
const showcaseDescription = computed(() => t(props.page === 'register' ? 'auth.showcaseRegisterDescription' : 'auth.showcaseLoginDescription'))
const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)
const currentYear = computed(() => new Date().getFullYear())

onMounted(() => { if (!appStore.publicSettingsLoaded) appStore.fetchPublicSettings() })
</script>

<style scoped>
.auth-stage {
  --auth-primary: #6466e9;
  --auth-fg: #171825;
  --auth-muted: #687080;
  --auth-surface: rgba(255, 255, 255, .82);
  --auth-field: #fff;
  --auth-line: rgba(213, 217, 231, .85);
  background: radial-gradient(ellipse at 70% 20%, #f6f4ff, #fff 55%);
  color: var(--auth-fg);
}
.auth-showcase { background: linear-gradient(145deg, #f8f9ff 0%, #eef0ff 58%, #e9eafe 100%); border-right: 1px solid rgba(151, 153, 221, .18); }
.showcase-orb { position: absolute; width: 28rem; height: 28rem; border-radius: 50%; filter: blur(95px); opacity: .34; pointer-events: none; animation: auth-float 14s ease-in-out infinite alternate; }
.showcase-orb-top { right: -8rem; top: -9rem; background: #b9c9ff; }
.showcase-orb-bottom { left: -10rem; bottom: -8rem; background: #d5beff; animation-delay: -7s; }
.showcase-brand { color: #181a2e; }
.showcase-content { animation: auth-enter .8s .08s ease-out both; }
.showcase-code { transition: transform .4s ease, box-shadow .4s ease; }
.showcase-code:hover { transform: translateY(-4px); box-shadow: 0 33px 65px rgba(30, 30, 80, .28); }
.showcase-clients { animation: auth-enter .8s .18s ease-out both; }
.auth-glow { position: absolute; width: 32rem; height: 32rem; border-radius: 50%; filter: blur(95px); opacity: .32; animation: auth-float 12s ease-in-out infinite alternate; }
.auth-glow-blue { right: -7rem; top: -9rem; background: #bbd1ff; }
.auth-glow-violet { left: -10rem; bottom: -10rem; background: #d7c4ff; animation-delay: -6s; }
.auth-back { color: var(--auth-muted); transition: color .2s ease, background .2s ease, transform .2s ease; }
.auth-back:hover { color: var(--auth-primary); background: rgba(100, 102, 233, .07); transform: translateX(-2px); }
.auth-brand { animation: auth-enter .65s ease-out both; }
.auth-brand-name { color: var(--auth-fg); }
.auth-subtitle, .auth-copyright { color: var(--auth-muted); }
.auth-card {
  border-color: rgba(255, 255, 255, .88);
  background: var(--auth-surface);
  box-shadow: 0 25px 65px rgba(67, 64, 130, .1), 0 2px 12px rgba(67, 64, 130, .035);
  animation: auth-enter .75s .08s ease-out both;
}
.auth-footer { color: var(--auth-muted); animation: auth-enter .75s .16s ease-out both; }
.auth-card :deep(.input) { min-height: 46px; border-color: var(--auth-line); background: var(--auth-field); color: var(--auth-fg); }
.auth-card :deep(.input:focus) { border-color: var(--auth-primary); box-shadow: 0 0 0 3px rgba(100, 102, 233, .15); }
.auth-card :deep(.input.input-error) { border-color: #ef4444; box-shadow: 0 0 0 3px rgba(239, 68, 68, .1); }
.auth-card :deep(.input-label) { color: var(--auth-fg); }
.auth-card :deep(.btn) { min-height: 46px; border-radius: 999px; }
.auth-card :deep(.btn-primary) { background: var(--auth-primary); background-image: none; box-shadow: 0 10px 22px rgba(100, 102, 233, .2); }
.auth-card :deep(.btn-primary:hover) { background: #5658db; box-shadow: 0 14px 28px rgba(100, 102, 233, .25); transform: translateY(-1px); }
.auth-card :deep(.btn-primary:active) { transform: scale(.98); }
.auth-card :deep(.btn-secondary) { border-color: var(--auth-line); background: var(--auth-field); color: var(--auth-fg); }
.auth-card :deep(.btn-secondary:hover) { border-color: #b8b9f3; background: rgba(100, 102, 233, .05); }
.auth-card :deep(a.text-primary-600), .auth-footer :deep(a.text-primary-600) { color: var(--auth-primary); }
.auth-card :deep(a.text-primary-600:hover), .auth-footer :deep(a.text-primary-600:hover) { color: #4f51cd; }
.auth-card :deep(.input:focus-visible), .auth-card :deep(.btn:focus-visible), .auth-back:focus-visible { outline: 2px solid var(--auth-primary); outline-offset: 2px; }

:global(.dark) .auth-stage { --auth-fg: #f7f7fb; --auth-muted: #a9afc0; --auth-surface: rgba(24, 29, 48, .82); --auth-field: #1a2238; --auth-line: rgba(101, 111, 140, .5); background: radial-gradient(ellipse at 70% 20%, #1b2446, #0b1120 65%); }
:global(.dark) .auth-showcase { background: linear-gradient(145deg, #151d36, #1c2347 65%, #28234d); border-color: rgba(161, 167, 222, .12); }
:global(.dark) .showcase-brand, :global(.dark) .auth-showcase h1 { color: #f8f9ff; }
:global(.dark) .auth-showcase p.text-slate-600, :global(.dark) .showcase-clients { color: #b1b8d0; }
:global(.dark) .showcase-clients span.rounded-full { background: rgba(255, 255, 255, .06); border-color: rgba(167, 175, 237, .25); }
:global(.dark) .auth-card { border-color: rgba(116, 126, 162, .18); }
:global(.dark) .auth-glow { opacity: .12; }
:global(.dark) .auth-card :deep(.btn-secondary) { background: #202a43; }

@keyframes auth-enter { from { opacity: 0; transform: translateY(16px); } to { opacity: 1; transform: translateY(0); } }
@keyframes auth-float { to { transform: translate(35px, 25px) scale(1.08); } }
@media (min-width: 1024px) {
  .auth-stage.auth-split { display: grid; grid-template-columns: minmax(0, 1.04fr) minmax(0, .96fr); }
  .auth-split .auth-showcase { position: sticky; top: 0; height: 100dvh; }
  .auth-split .auth-main { background: var(--auth-surface); }
  .auth-split .auth-content { max-width: 440px; }
  .auth-split .auth-brand { display: none; }
  .auth-split .auth-card { border: 0; border-radius: 0; padding: 0; background: transparent; box-shadow: none; backdrop-filter: none; }
  .auth-split .auth-card :deep(.auth-form-heading) { text-align: left; }
  .auth-split .auth-card :deep(.auth-form-heading h2) { font-size: 2rem; line-height: 1.2; }
}
@media (min-width: 1024px) and (max-height: 760px) {
  .auth-showcase > div.relative { padding-top: 1.75rem; padding-bottom: 1.75rem; }
  .auth-showcase h1 { font-size: 2.3rem; }
  .showcase-code { margin-top: 1.5rem; }
  .showcase-code-body { padding-top: 1rem; padding-bottom: 1rem; }
}
@media (max-width: 640px) { .auth-glow { width: 18rem; height: 18rem; opacity: .2; } .auth-card { border-radius: 1.4rem; } }
@media (prefers-reduced-motion: reduce) { .auth-stage *, .auth-stage *::before, .auth-stage *::after { animation: none !important; transition-duration: .01ms !important; } }
</style>
