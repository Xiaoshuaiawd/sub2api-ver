<template>
  <div class="auth-stage relative isolate min-h-dvh px-4 py-5 sm:px-6 sm:py-7">
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

    <div class="relative z-10 mx-auto flex min-h-[calc(100dvh-100px)] w-full max-w-6xl items-center justify-center py-8 sm:py-12">
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
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'

const { t } = useI18n()
const appStore = useAppStore()
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
:global(.dark) .auth-card { border-color: rgba(116, 126, 162, .18); }
:global(.dark) .auth-glow { opacity: .12; }
:global(.dark) .auth-card :deep(.btn-secondary) { background: #202a43; }

@keyframes auth-enter { from { opacity: 0; transform: translateY(16px); } to { opacity: 1; transform: translateY(0); } }
@keyframes auth-float { to { transform: translate(35px, 25px) scale(1.08); } }
@media (max-width: 640px) { .auth-glow { width: 18rem; height: 18rem; opacity: .2; } .auth-card { border-radius: 1.4rem; } }
@media (prefers-reduced-motion: reduce) { .auth-stage *, .auth-stage *::before, .auth-stage *::after { animation: none !important; transition-duration: .01ms !important; } }
</style>
