<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, ApiError, open_events } from './api'
import type { DashboardResponse, SourceState } from './types'
import { auth, forget, logout, require_password_change } from './lib/auth'
import DashboardView from './views/DashboardView.vue'
import LoginView from './views/LoginView.vue'
import RegistryView from './views/RegistryView.vue'
import PasswordDialog from './components/PasswordDialog.vue'
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
// 主动打开改口令对话框
const password_open = ref(false)
// 顶部页签定义
const tabs = [
  { key: 'dashboard', label: '面板' },
  { key: 'registry', label: '注册表' },
] as const

// 关闭 SSE 的回调，组件卸载时调用
let close_events: (() => void) | null = null
// 提示条自增序号
let toast_seq = 0

// 是否需要渲染业务界面：引导结束、且要么有凭据、要么服务端根本没开鉴权
const signed_in = computed(() => auth.ready && (auth.token !== '' || auth.open))
// 强制改口令只在已登录之后生效：改口令本身需要凭据，
// 未登录时把框弹出来，用户既改不了也点不掉，等于把登录页堵死
const password_forced = computed(() => auth.must_change && signed_in.value)
// 是否显示改口令与登出按钮
const show_session_actions = computed(() => !auth.open && auth.token !== '')

// 拉取 Dashboard 全量数据
async function load(): Promise<void> {
  try {
    data.value = await api.dashboard()
    error.value = ''
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : String(err)
    if (err instanceof ApiError && err.status === 401) {
      // 凭据过期或已在别处改过口令，退回登录页
      forget()
      return
    }
    // 服务端要求先改口令时，界面只保留改密框
    if (err instanceof ApiError && err.code === 'password_change_required') {
      require_password_change()
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

// 登录成功后接管数据加载；仍在初始口令状态时先不拉数据，交给强制改密框
function on_signed_in(): void {
  if (auth.must_change) {
    loading.value = false
    error.value = ''
    password_open.value = true
    return
  }
  loading.value = true
  error.value = ''
  void load().then(connect)
}

// 改完口令：服务端已换发新会话，凭据仍是「已登录」，因此这里要显式把数据接上
function on_password_changed(): void {
  password_open.value = false
  notify('口令已更新，其它设备上的登录已失效')
  on_signed_in()
}

// 主动登出
async function sign_out(): Promise<void> {
  await logout()
  close_events?.()
  close_events = null
  data.value = null
  connected.value = false
  page.value = 'dashboard'
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
  // 还没登录就先看登录页：初始口令的提示由登录页给出，
  // 改密框要等拿到凭据之后才能弹（见 password_forced）
  if (!signed_in.value) {
    loading.value = false
    return
  }
  // 已登录但仍是初始口令：除改口令外的请求都会被拒，不必先拉数据
  if (auth.must_change) {
    loading.value = false
    password_open.value = true
    return
  }
  await load()
  connect()
})

// 从「未登录」变成「已登录」时接管数据加载。
// 这里监听的是状态本身而不是 token：改口令会换发新凭据，
// 按 token 变化判断的话「旧凭据非空、新凭据也非空」会被漏掉。
watch(signed_in, (now, before) => {
  if (now && !before) {
    on_signed_in()
  }
})

onBeforeUnmount(() => {
  close_events?.()
})
</script>

<template>
  <LoginView v-if="auth.ready && !signed_in" />

  <!-- 初始口令期间不渲染面板外壳：此时除改口令外的请求都会被服务端拒绝，
       渲染出来只会是一片报错，徒增困惑 -->
  <div
    v-else-if="auth.ready && password_forced"
    class="flex min-h-full items-center justify-center px-5 text-base text-ink-muted"
  >
    面板仍在使用初始口令，请先完成口令修改。
  </div>

  <div v-else-if="auth.ready" class="flex min-h-full flex-col">
    <header class="sticky top-0 z-20 border-b border-line bg-canvas/90 backdrop-blur">
      <div class="mx-auto flex max-w-6xl items-center gap-4 px-5 py-3">
        <div class="flex items-center gap-2.5">
          <img :src="logo_url" alt="WrtDeck" class="h-8 w-8 shrink-0" />
          <div class="flex items-baseline gap-2">
            <span class="text-lg font-semibold tracking-tight text-ink">WrtDeck</span>
            <span class="readout text-sm text-ink-faint">{{ data?.server.version ?? auth.version }}</span>
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
            v-if="show_session_actions"
            type="button"
            class="btn btn-outline btn-sm"
            :class="{
              'border-amber-500 text-amber-600 dark:border-amber-600 dark:text-amber-400':
                auth.must_change,
            }"
            @click="password_open = true"
          >
            {{ auth.must_change ? '改初始口令' : '改口令' }}
          </button>
          <button v-if="show_session_actions" type="button" class="btn btn-ghost btn-sm" @click="sign_out">
            退出
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

      <!-- 兜底：既不加载、也没数据、也没报错时给一句话，避免出现整片空白 -->
      <div v-else class="panel p-6 text-base text-ink-muted">正在准备面板…</div>
    </main>

    <ToastStack :items="toasts" />
  </div>

  <div v-else class="flex min-h-full items-center justify-center text-base text-ink-muted">
    正在连接服务…
  </div>

  <!-- 改口令对话框放在分支之外：登录页与面板里都能打开它 -->
  <PasswordDialog
    :open="password_open || password_forced"
    :forced="password_forced"
    @update:open="password_open = $event"
    @done="on_password_changed"
  />
</template>
