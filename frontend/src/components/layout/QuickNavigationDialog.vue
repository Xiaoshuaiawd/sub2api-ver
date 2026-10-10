<template>
  <Teleport to="body">
    <Transition name="quick-navigation">
      <div v-if="open" class="fixed inset-0 z-[100] flex items-start justify-center bg-slate-950/45 px-3 pt-[12vh] backdrop-blur-[3px] sm:pt-[16vh]" @click.self="close">
        <section ref="dialogRef" role="dialog" aria-modal="true" :aria-label="t('nav.quickNavigation')" class="w-full max-w-[540px] overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-[0_28px_90px_rgba(20,25,50,.24)] dark:border-dark-600 dark:bg-dark-800">
          <div class="flex items-center gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
            <Icon name="search" size="md" class="shrink-0 text-gray-400" />
            <input ref="searchInput" v-model="query" type="search" :placeholder="t('nav.searchPages')" :aria-label="t('nav.searchPages')" class="min-w-0 flex-1 bg-transparent text-sm text-gray-900 outline-none placeholder:text-gray-400 dark:text-white dark:placeholder:text-gray-500" />
            <button type="button" class="rounded-md border border-gray-200 px-1.5 py-0.5 text-[11px] text-gray-500 hover:bg-gray-50 dark:border-dark-600 dark:text-gray-400 dark:hover:bg-dark-700" :aria-label="t('common.close')" @click="close">Esc</button>
          </div>
          <div class="max-h-[min(55vh,420px)] overflow-y-auto p-2" role="listbox" :aria-label="t('nav.quickNavigation')">
            <p v-if="filteredItems.length === 0" class="px-3 py-10 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('nav.noSearchResults') }}</p>
            <button v-for="(item, index) in filteredItems" :key="item.path" type="button" role="option" :aria-selected="index === selectedIndex" class="flex w-full items-center justify-between rounded-xl px-3 py-2.5 text-left text-sm transition-colors" :class="index === selectedIndex ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/25 dark:text-primary-200' : 'text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-dark-700'" @mouseenter="selectedIndex = index" @click="navigate(item.path)">
              <span class="min-w-0 truncate font-medium">{{ item.label }}</span>
              <span class="ml-4 hidden shrink-0 text-xs text-gray-400 sm:inline">{{ item.path }}</span>
            </button>
          </div>
          <footer class="flex items-center gap-4 border-t border-gray-100 bg-gray-50/70 px-4 py-2.5 text-[11px] text-gray-500 dark:border-dark-700 dark:bg-dark-900/50 dark:text-gray-400">
            <span>↑ ↓ {{ t('nav.searchSelect') }}</span>
            <span>↵ {{ t('nav.searchOpen') }}</span>
            <span class="ml-auto">Esc {{ t('common.close') }}</span>
          </footer>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'

interface NavigationItem { path: string; label: string }

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()
const { t } = useI18n()
const router = useRouter()
const query = ref('')
const items = ref<NavigationItem[]>([])
const selectedIndex = ref(0)
const searchInput = ref<HTMLInputElement | null>(null)
const dialogRef = ref<HTMLElement | null>(null)
const filteredItems = computed(() => {
  const term = query.value.trim().toLocaleLowerCase()
  return term ? items.value.filter((item) => `${item.label} ${item.path}`.toLocaleLowerCase().includes(term)) : items.value
})

function collectNavigationItems() {
  const seen = new Set<string>()
  items.value = Array.from(document.querySelectorAll<HTMLAnchorElement>('.sidebar-nav a[href]'))
    .map((link) => ({ path: link.getAttribute('href') || '', label: (link.querySelector('.sidebar-label')?.textContent || link.textContent || '').trim() }))
    .filter((item) => {
      if (!item.path.startsWith('/') || !item.label || seen.has(item.path)) return false
      seen.add(item.path)
      return true
    })
}

function close() { emit('update:open', false) }

function navigate(path: string) {
  close()
  void router.push(path)
}

function isEditing(target: EventTarget | null) {
  return target instanceof HTMLElement && Boolean(target.closest('input, textarea, select, [contenteditable="true"]'))
}

function onKeydown(event: KeyboardEvent) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault()
    emit('update:open', !props.open)
    return
  }
  if (!props.open) {
    if (event.key === '/' && !event.metaKey && !event.ctrlKey && !event.altKey && !isEditing(event.target)) {
      event.preventDefault()
      emit('update:open', true)
    }
    return
  }
  if (event.key === 'Escape') { event.preventDefault(); close(); return }
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    const step = event.key === 'ArrowDown' ? 1 : -1
    selectedIndex.value = (selectedIndex.value + step + filteredItems.value.length) % Math.max(filteredItems.value.length, 1)
  }
  if (event.key === 'Enter' && filteredItems.value[selectedIndex.value]) {
    event.preventDefault()
    navigate(filteredItems.value[selectedIndex.value].path)
  }
  if (event.key === 'Tab' && dialogRef.value) {
    const focusable = Array.from(dialogRef.value.querySelectorAll<HTMLElement>('input, button:not([disabled])'))
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
  }
}

watch(() => props.open, async (open) => {
  if (!open) return
  collectNavigationItems()
  query.value = ''
  selectedIndex.value = 0
  await nextTick()
  searchInput.value?.focus()
})
watch(query, () => { selectedIndex.value = 0 })
onMounted(() => document.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => document.removeEventListener('keydown', onKeydown))
</script>

<style scoped>
.quick-navigation-enter-active, .quick-navigation-leave-active { transition: opacity .22s ease; }
.quick-navigation-enter-active section { transition: transform .34s cubic-bezier(.2, 1.28, .36, 1), opacity .22s ease; }
.quick-navigation-leave-active section { transition: transform .16s ease, opacity .16s ease; }
.quick-navigation-enter-from, .quick-navigation-leave-to { opacity: 0; }
.quick-navigation-enter-from section { transform: translateY(-12px) scale(.965); opacity: 0; }
.quick-navigation-leave-to section { transform: translateY(-5px) scale(.985); opacity: 0; }
@media (prefers-reduced-motion: reduce) { .quick-navigation-enter-active, .quick-navigation-leave-active, .quick-navigation-enter-active section, .quick-navigation-leave-active section { transition: none; } }
</style>
