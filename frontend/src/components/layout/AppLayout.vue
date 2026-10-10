<template>
  <div class="app-shell min-h-screen" :class="{ 'dashboard-layout': variant === 'dashboard' }">
    <!-- Background Decoration -->
    <div class="layout-ambient pointer-events-none fixed inset-0"></div>

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main class="page-content p-4 md:p-6 lg:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'

const appStore = useAppStore()
const authStore = useAuthStore()
const { variant = 'default' } = defineProps<{ variant?: 'default' | 'dashboard' }>()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>

<style scoped>
.app-shell { background: #fbfbfe; }
.layout-ambient { background: radial-gradient(ellipse at 87% 3%, rgba(100, 102, 233, .055), transparent 42%); }
.app-shell :deep(.sidebar) { border-right-color: #e9eaf2; }
.app-shell :deep(.sidebar-link-active) { background: #f0efff; color: #595bd6; }
.app-shell :deep(.sidebar-link-active:hover) { background: #e8e7ff; color: #4b4dc9; }
.app-shell :deep(.sidebar-logo) { box-shadow: 0 0 18px rgba(100, 102, 233, .18); }
.app-shell :deep(header.glass) { background: rgba(255, 255, 255, .9); border-bottom-color: #ececf2; }
.app-shell :deep(.header-balance) { background: #f2f1ff; }
.app-shell :deep(.header-balance svg), .app-shell :deep(.header-balance > span.font-semibold) { color: #595bd6; }
.page-content > * { animation: page-enter .36s ease-out both; }
.dashboard-layout { background: #fff; }
@keyframes page-enter { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: translateY(0); } }
@media (prefers-reduced-motion: reduce) { .page-content > * { animation: none; } }
</style>

<style>
.dark .app-shell { background: #0b1120; }
.dark .app-shell .sidebar { border-right-color: #273148; }
.dark .app-shell .sidebar-link-active { background: rgba(100, 102, 233, .19); color: #b9baff; }
.dark .app-shell header.glass { background: rgba(14, 22, 38, .9); border-bottom-color: #273148; }
.dark .app-shell .header-balance { background: rgba(100, 102, 233, .18); }
.dark .app-shell .header-balance svg, .dark .app-shell .header-balance > span.font-semibold { color: #b9baff; }
</style>
