<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950" :class="{ 'dashboard-layout': variant === 'dashboard' }">
    <!-- Background Decoration -->
    <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>

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
      <main class="p-4 md:p-6 lg:p-8">
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
.dashboard-layout { background: #fff; }
.dashboard-layout :deep(.sidebar) { border-right-color: #ececf2; }
.dashboard-layout :deep(.sidebar-link-active) { background: #f0efff; color: #595bd6; }
.dashboard-layout :deep(.sidebar-link-active:hover) { background: #e8e7ff; color: #4b4dc9; }
.dashboard-layout :deep(.sidebar-logo) { box-shadow: 0 0 18px rgba(100, 102, 233, .18); }
.dashboard-layout :deep(header.glass) { background: rgba(255, 255, 255, .9); border-bottom-color: #ececf2; }
.dashboard-layout :deep(.header-balance) { background: #f2f1ff; }
.dashboard-layout :deep(.header-balance svg), .dashboard-layout :deep(.header-balance > span) { color: #595bd6; }
:global(.dark) .dashboard-layout { background: #0b1120; }
:global(.dark) .dashboard-layout :deep(.sidebar) { border-right-color: #273148; }
:global(.dark) .dashboard-layout :deep(.sidebar-link-active) { background: rgba(100, 102, 233, .19); color: #b9baff; }
:global(.dark) .dashboard-layout :deep(header.glass) { background: rgba(14, 22, 38, .9); border-bottom-color: #273148; }
:global(.dark) .dashboard-layout :deep(.header-balance) { background: rgba(100, 102, 233, .18); }
:global(.dark) .dashboard-layout :deep(.header-balance svg), :global(.dark) .dashboard-layout :deep(.header-balance > span) { color: #b9baff; }
</style>
