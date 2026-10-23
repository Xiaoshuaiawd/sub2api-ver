<template>
  <main class="flex min-h-screen items-center justify-center bg-gray-50 px-4 dark:bg-dark-900">
    <section class="w-full max-w-md rounded-2xl bg-white p-8 shadow-sm dark:bg-dark-800" aria-live="polite">
      <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ isChinese ? '连接 Vcode' : 'Connect Vcode' }}</h1>
      <p v-if="!error" role="status" class="mt-3 text-sm text-gray-600 dark:text-dark-300">
        {{ isChinese ? '正在验证账号并返回 Vcode…' : 'Verifying your account and returning to Vcode…' }}
      </p>
      <template v-else>
        <p role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
        <button type="button" class="btn btn-primary mt-5" @click="authorize">
          {{ isChinese ? '重试' : 'Try again' }}
        </button>
      </template>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { apiClient } from '@/api/client'
import { buildVcodeCallbackUrl } from '@/utils/vcodeAuthorize'

const route = useRoute()
const error = ref('')
const isChinese = computed(() => navigator.language.toLowerCase().startsWith('zh'))
const challengePattern = /^[A-Za-z0-9_-]{43}$/

async function authorize() {
  error.value = ''
  const state = typeof route.query.state === 'string' ? route.query.state : ''
  const challenge = typeof route.query.code_challenge === 'string' ? route.query.code_challenge : ''
  if (!challengePattern.test(state) || !challengePattern.test(challenge)) {
    error.value = isChinese.value ? '授权链接无效，请从 Vcode 重新登录。' : 'Invalid authorization link. Start again from Vcode.'
    return
  }
  try {
    const { data } = await apiClient.post<{ code: string }>('/vcode/authorize', { code_challenge: challenge })
    const controlOrigin = import.meta.env.VITE_VCODE_CONTROL_ORIGIN || 'https://agent.albkw.cn'
    window.location.replace(buildVcodeCallbackUrl(controlOrigin, state, data.code))
  } catch {
    error.value = isChinese.value ? '连接失败，请重试。' : 'Connection failed. Try again.'
  }
}

onMounted(() => { void authorize() })
</script>
