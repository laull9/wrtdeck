<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, ApiError, get_token, open_events } from './api'
import type { DashboardResponse, SourceState } from './types'
import DashboardView from './views/DashboardView.vue'
import RegistryView from './views/RegistryView.vue'
import TokenDialog from './components/TokenDialog.vue'
import ThemeToggle from './components/ThemeToggle.vue'
import ToastStack from './components/ToastStack.vue'
// 品牌标识，由 scripts/make-icons.py 从 assets/WrtDeck.png 生成
import logo_url from './assets/logo.png'

// 当前视图，只有两个页面因此不引入 Vue Router
const page = ref<'dashboard' | 'registry'>('dashboard')
// Dashboard 全量数据，SSE 只做增量覆盖
const data = ref<DashboardResponse | null>(null)
// 首次加载与刷新状态
const loading = ref(true)
const error = ref('')
// SSE 连接状态，用于右上角指示灯
const connected = ref(false)
// 提示条列表
const toasts = ref<{ id: number; text: string; tone: 'ok' | 'error' }[]>([])
// 是否需要显示 Token 输入框
const token_open = ref(false)
// 是否有本地 Token，决定是否显示鉴权入口
const has_token = ref(get_token() !== '')
// 顶部页签定义
const tabs = [
  { key: 'dashboard', label: '面板' },
  { key: 'registry', label: '注册表' },
] as const

// 关闭 SSE 的回调，组件卸载时调用
let close_events: (() => void) | null = null
// 提示条自增序号
let toast_seq = 0

// 是否处于"未鉴权"状态，用于引导填写 Token
const need_token = computed(() => error.value.includes('Token'))

// 拉取 Dashboard 全量数据
async function load(): Promise<void> {
  try {
    data.value = await api.dashboard()
    error.value = ''
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : String(err)
    if (err instanceof ApiError && err.status === 401) {
      has_token.value = false
      token_open.value = true
    }
  } finally {
    loading.value = false
  }
}

// 弹出提示条，3 秒后自动消失
function notify(text: string, tone: 'ok' | 'error' = 'ok'): void {
  toast_seq += 1
  const id = toast_seq
  toasts.value = [...toasts.value, { id, text, tone }]
  window.setTimeout(() => {
    toasts.value = toasts.value.filter((item) => item.id !== id)
  }, 3000)
}

// 用 SSE 推来的新状态覆盖对应信息源
function apply_source(next: SourceState): void {
  const current = data.value
  if (!current) {
    return
  }
  const index = current.sources.findIndex((item) => item.entry.id === next.id)
  if (index >= 0) {
    const item = current.sources[index]
    if (item) {
      item.state = next
    }
    return
  }
  // 新注册的信息源，补一次全量拉取
  void load()
}

// 建立 SSE 连接并注册事件处理
function connect(): void {
  close_events?.()
  close_events = open_events({
    on_state: (state) => {
      connected.value = state
    },
    on_source: apply_source,
    on_action: (payload) => {
      notify(payload.ok ? `${payload.name} 执行成功` : `${payload.name} 执行失败：${payload.message}`, payload.ok ? 'ok' : 'error')
    },
    on_registry: (payload) => {
      notify(payload.action === 'delete' ? `已删除 ${payload.id}` : `已更新 ${payload.id}`)
    },
  })
}

// Token 保存后重连并刷新数据
function on_token_saved(): void {
  has_token.value = get_token() !== ''
  connect()
  void load()
}

// 手动刷新信息源
async function refresh_source(id: string): Promise<void> {
  try {
    await api.refresh_source(id)
  } catch (err) {
    notify(err instanceof Error ? err.message : String(err), 'error')
  }
}

onMounted(async () => {
  await load()
  connect()
})

onBeforeUnmount(() => {
  close_events?.()
})
</script>

<template>
  <div class="flex min-h-full flex-col">
    <header class="sticky top-0 z-20 border-b border-line bg-canvas/90 backdrop-blur">
      <div class="mx-auto flex max-w-6xl items-center gap-4 px-5 py-3">
        <div class="flex items-center gap-2.5">
          <img :src="logo_url" alt="WrtDeck" class="h-8 w-8 shrink-0" />
          <div class="flex items-baseline gap-2">
            <span class="text-lg font-semibold tracking-tight text-ink">WrtDeck</span>
            <span class="readout text-sm text-ink-faint">{{ data?.server.version ?? '—' }}</span>
          </div>
        </div>

        <nav class="flex items-center gap-1 rounded-lg bg-track p-0.5">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            class="rounded-md px-3 py-1.5 text-base transition-colors"
            :class="
              page === tab.key
                ? 'bg-raised-strong text-ink'
                : 'text-ink-muted hover:text-ink-strong'
            "
            @click="page = tab.key"
          >
            {{ tab.label }}
          </button>
        </nav>

        <div class="ml-auto flex items-center gap-3 text-sm">
          <span class="flex items-center gap-1.5 text-ink-muted">
            <span
              class="h-1.5 w-1.5 rounded-full"
              :class="connected ? 'bg-emerald-500 dark:bg-emerald-400' : 'bg-rose-500'"
            ></span>
            {{ connected ? '实时已连接' : '实时断开' }}
          </span>
          <span class="readout hidden text-ink-faint sm:inline">
            {{ data?.server.sources ?? 0 }} 源 / {{ data?.server.actions ?? 0 }} 动作
          </span>
          <button
            v-if="!data?.server.auth_disabled"
            type="button"
            class="btn btn-outline btn-sm"
            :class="{
              'border-amber-500 text-amber-600 dark:border-amber-600 dark:text-amber-400':
                !has_token,
            }"
            @click="token_open = true"
          >
            {{ has_token ? 'Token' : '设置 Token' }}
          </button>
          <ThemeToggle />
        </div>
      </div>
    </header>

    <main class="mx-auto w-full max-w-6xl flex-1 px-5 py-6">
      <div v-if="loading" class="panel p-6 text-base text-ink-muted">正在连接服务…</div>

      <div
        v-else-if="error && !data"
        class="rounded-xl border border-rose-200 bg-rose-50 p-6 text-base text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300"
      >
        <p class="font-medium">{{ error }}</p>
        <p v-if="need_token" class="mt-2 text-rose-600/80 dark:text-rose-400/80">
          服务已开启 Bearer Token 鉴权，请点击右上角填入 Token（可在服务端执行
          <code class="code-chip">owdash -print-token</code> 获取）。
        </p>
        <button
          type="button"
          class="mt-4 rounded-md border border-rose-300 px-3 py-1.5 text-base text-rose-700 hover:bg-rose-100 dark:border-rose-800 dark:text-rose-200 dark:hover:bg-rose-900/40"
          @click="load"
        >
          重试
        </button>
      </div>

      <DashboardView
        v-else-if="page === 'dashboard' && data"
        :sources="data.sources"
        :actions="data.actions"
        :connected="connected"
        @refresh="refresh_source"
        @notify="notify"
        @reload="load"
      />

      <RegistryView v-else-if="page === 'registry'" @notify="notify" @reload="load" />
    </main>

    <ToastStack :items="toasts" />
    <TokenDialog v-model:open="token_open" @saved="on_token_saved" />
  </div>
</template>
