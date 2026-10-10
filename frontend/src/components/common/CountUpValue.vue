<template>
  <span :aria-label="formattedTarget" aria-live="off">{{ formattedValue }}</span>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{ value: number; formatter?: (value: number) => string; duration?: number }>(), { duration: 600 })
const displayed = ref(0)
let frame = 0
let mounted = false
const format = (value: number) => props.formatter ? props.formatter(value) : value.toLocaleString()
const formattedValue = computed(() => format(displayed.value))
const formattedTarget = computed(() => format(props.value))

function animateTo(target: number) {
  if (frame && typeof cancelAnimationFrame === 'function') cancelAnimationFrame(frame)
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches || props.duration <= 0 || typeof requestAnimationFrame !== 'function') {
    displayed.value = target
    return
  }
  const start = performance.now()
  const from = displayed.value
  const tick = (now: number) => {
    const progress = Math.min((now - start) / props.duration, 1)
    displayed.value = from + (target - from) * (1 - Math.pow(1 - progress, 3))
    if (progress < 1) frame = requestAnimationFrame(tick)
    else displayed.value = target
  }
  frame = requestAnimationFrame(tick)
}

onMounted(() => { mounted = true; animateTo(props.value) })
watch(() => props.value, (value) => { if (mounted) animateTo(value) })
onBeforeUnmount(() => { if (frame && typeof cancelAnimationFrame === 'function') cancelAnimationFrame(frame) })
</script>
