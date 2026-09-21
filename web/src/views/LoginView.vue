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
        <h1 class="text-base font-semibold text-ink">登录面板</h1>
        <p class="mt-1 text-sm leading-relaxed text-ink-muted">
          输入设备上设置的口令。<span v-if="need_setup">当前还是初始口令，登录后必须立即更换。</span>
        </p>

        <div
          v-if="need_setup"
          class="mt-4 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm leading-relaxed text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300"
        >
          初始口令是 <code class="code-chip">{{ default_password }}</code>，
          改口令之前面板只处理改口令请求。
        </div>

        <label class="field-label mt-4 block" for="login-password">登录口令</label>
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
          在本浏览器保持登录（有效期 {{ auth.session_ttl_minutes }} 分钟）
        </label>

        <p v-if="error" class="mt-3 text-sm leading-relaxed text-rose-600 dark:text-rose-400">
          {{ error }}
        </p>

        <button type="submit" class="btn btn-primary mt-4 w-full" :disabled="busy || !password">
          {{ busy ? '登录中…' : '登录' }}
        </button>

        <div class="mt-4 flex items-center justify-between gap-2 border-t border-line pt-3 text-sm">
          <button type="button" class="btn btn-ghost btn-sm" @click="token_open = true">
            改用 API Token 登录
          </button>
          <span class="text-ink-ghost">忘记口令？</span>
        </div>
        <p class="mt-1.5 text-sm leading-relaxed text-ink-faint">
          在设备上执行 <code class="code-chip">/etc/init.d/wrtdeck password</code> 可重置口令。
        </p>
      </form>

      <p
        v-if="!auth.tls"
        class="mt-4 text-center text-sm leading-relaxed text-amber-700 dark:text-amber-500"
      >
        当前连接未启用 HTTPS，口令会明文传输。局域网内可用，直接暴露到公网前请先配置
        <code class="code-chip">tls</code> 或套一层反向代理。
      </p>
    </div>

    <TokenDialog v-model:open="token_open" />
  </div>
</template>
