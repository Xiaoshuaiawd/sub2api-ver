<template>
  <div class="app-shell min-h-screen" :class="{ 'dashboard-layout': variant === 'dashboard', 'layout-wide': fullWidth }">
    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader :full-width="fullWidth" @toggle-full-width="toggleFullWidth" />

      <!-- Main Content -->
      <main class="page-content mx-auto w-full p-4 transition-[max-width] duration-300 md:p-6 lg:p-8" :class="fullWidth ? 'max-w-none' : 'max-w-[1320px]'">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import '@/styles/workspace-motion.css'
import { computed, onMounted, ref } from 'vue'
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
const fullWidth = ref(localStorage.getItem('workspace_full_width') === 'true')

function toggleFullWidth() {
  fullWidth.value = !fullWidth.value
  localStorage.setItem('workspace_full_width', String(fullWidth.value))
}

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
.app-shell { background: #fff; }
.app-shell :deep(.sidebar) { background: #fafafa; border-right-color: #e9eaf2; }
.app-shell :deep(.sidebar-link-active) { background: #f1f1f5; color: #6366e9; }
.app-shell :deep(.sidebar-link-active:hover) { background: #ebebf0; color: #5053c9; }
.app-shell :deep(.sidebar-logo) { box-shadow: none; }
.app-shell :deep(header.glass) { background: rgba(255, 255, 255, .96); border-bottom-color: #ececf2; }
.app-shell :deep(.header-balance) { background: #f2f1ff; }
.app-shell :deep(.header-balance svg), .app-shell :deep(.header-balance > span.font-semibold) { color: #595bd6; }
.page-content > * { animation: page-enter .38s cubic-bezier(.16, 1, .3, 1) both; }
.dashboard-layout { background: #fff; }
.layout-wide :deep(.dashboard-page > div) { max-width: none; }
@keyframes page-enter { from { opacity: 0; transform: translateY(10px) scale(.994); } to { opacity: 1; transform: translateY(0) scale(1); } }
@media (prefers-reduced-motion: reduce) { .page-content > * { animation: none; } }
</style>

<style>
.dark .app-shell { background: #0b1120; }
.dark .app-shell .sidebar { background: #111827; border-right-color: #273148; }
.dark .app-shell .sidebar-link-active { background: #253044; color: #b9baff; }
.dark .app-shell header.glass { background: rgba(14, 22, 38, .9); border-bottom-color: #273148; }
.dark .app-shell .header-balance { background: rgba(100, 102, 233, .18); }
.dark .app-shell .header-balance svg, .dark .app-shell .header-balance > span.font-semibold { color: #b9baff; }
</style>
