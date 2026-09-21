<script setup lang="ts">
import { computed, ref } from 'vue'
import { ApiError } from '../api'
import { auth, login } from '../lib/auth'
import TokenDialog from '../components/TokenDialog.vue'
// 品牌标识，由 scripts/make-icons.py 从 assets/WrtDeck.png 生成
import logo_url from '../assets/logo.png'

// 初始口令写在文档与安装提示里，登录页把它点明，免得用户对着空表单去猜
const default_password = 'admin'

// 口令草稿
const password = ref('')
// 是否在本浏览器保持登录
const remember = ref(false)
// 提交中，避免重复点击
const busy = ref(false)
// 登录失败原因，直接取服务端返回的中文提示
const error = ref('')
// 是否展开「用 API Token 登录」入口
const token_open = ref(false)
// 口令是否明文显示
const revealed = ref(false)

// 服务端仍是初始口令时给出提示
const need_setup = computed(() => auth.must_change)

// 提交登录
async function submit(): Promise<void> {
  if (busy.value) {
    return
  }
  busy.value = true
  error.value = ''
  try {
    await login(password.value, remember.value)
    password.value = ''
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : String(err)
    password.value = ''
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex min-h-full items-center justify-center px-5 py-10">
    <div class="w-full max-w-sm">
      <div class="mb-6 flex items-center justify-center gap-3">
        <img :src="logo_url" alt="WrtDeck" class="h-10 w-10 shrink-0" />
        <div class="flex items-baseline gap-2">
          <span class="text-xl font-semibold tracking-tight text-ink">WrtDeck</span>
          <span class="readout text-sm text-ink-faint">{{ auth.version || '—' }}</span>
        </div>
      </div>

      <form class="panel p-6" @submit.prevent="submit">
        <h1 class="text-base font-semibold text-ink">登录</h1>

        <div
          v-if="need_setup"
          class="mt-3 rounded-lg border border-amber-300 bg-amber-50 p-2.5 text-xs text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300"
        >
          初始口令：<code class="code-chip">{{ default_password }}</code>，首次登录需修改
        </div>

        <label class="field-label mt-4 block" for="login-password">口令</label>
        <div class="relative mt-1.5">
          <input
            id="login-password"
            v-model="password"
            :type="revealed ? 'text' : 'password'"
            autocomplete="current-password"
            class="field pr-16"
            :disabled="busy"
          />
          <button
            type="button"
            class="btn btn-ghost btn-sm absolute right-1 top-1/2 -translate-y-1/2"
            @click="revealed = !revealed"
          >
            {{ revealed ? '隐藏' : '显示' }}
          </button>
        </div>

        <label class="mt-3 flex items-center gap-2 text-sm text-ink-muted">
          <input v-model="remember" type="checkbox" class="field-check" :disabled="busy" />
          保持登录（{{ auth.session_ttl_minutes }} 分钟）
        </label>

        <p v-if="error" class="mt-3 text-sm leading-relaxed text-rose-600 dark:text-rose-400">
          {{ error }}
        </p>

        <button type="submit" class="btn btn-primary mt-4 w-full" :disabled="busy || !password">
          {{ busy ? '登录中…' : '登录' }}
        </button>

        <div class="mt-4 flex items-center justify-between gap-2 border-t border-line pt-3 text-sm">
          <button type="button" class="btn btn-ghost btn-sm" @click="token_open = true">
            API Token 登录
          </button>
        </div>
        <p class="mt-2 text-xs text-ink-faint">
          重置口令：<code class="code-chip">/etc/init.d/wrtdeck password</code>
        </p>
      </form>

      <p
        v-if="!auth.tls"
        class="mt-3 text-center text-xs text-amber-600 dark:text-amber-400"
      >
        未启用 HTTPS，请勿在非信任网络直接暴露
      </p>
    </div>

    <TokenDialog v-model:open="token_open" />
  </div>
</template>
