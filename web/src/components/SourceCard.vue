<script setup lang="ts">
import { computed } from 'vue'
import type { SourceItem } from '../types'

// 一个信息源卡片，按 ui.type 决定渲染成读数、状态徽标或文本
const props = defineProps<{ item: SourceItem }>()
const emit = defineEmits<{ refresh: [] }>()

// 状态到颜色与文案的映射。圆点用饱和度较高的色块，文字在浅色主题下需要更深一档才够对比度
const status_map: Record<string, { label: string; dot: string; text: string }> = {
  ok: { label: '正常', dot: 'bg-emerald-500 dark:bg-emerald-400', text: 'text-emerald-700 dark:text-emerald-300' },
  running: { label: '采集中', dot: 'bg-sky-500 animate-pulse dark:bg-sky-400', text: 'text-sky-700 dark:text-sky-300' },
  waiting: { label: '等待推送', dot: 'bg-violet-500 animate-pulse dark:bg-violet-400', text: 'text-violet-700 dark:text-violet-300' },
  error: { label: '异常', dot: 'bg-rose-500', text: 'text-rose-700 dark:text-rose-300' },
  stale: { label: '已过期', dot: 'bg-amber-500 dark:bg-amber-400', text: 'text-amber-700 dark:text-amber-300' },
  unknown: { label: '未采集', dot: 'bg-ink-ghost', text: 'text-ink-muted' },
}

// 未知状态的兜底展示配置
const fallback_status = status_map.unknown as { label: string; dot: string; text: string }

// 当前状态对应的展示配置
const status = computed(() => status_map[props.item.state.status] ?? fallback_status)

// 订阅型信息源由 broker 推送驱动，没有可手动触发的采集动作
const is_push = computed(
  () =>
    props.item.entry.transport.type === 'mqtt' &&
    props.item.entry.transport.mqtt?.mode === 'subscribe',
)

// 按 ui.precision 格式化数值读数
const display_value = computed(() => {
  const raw = props.item.state.value
  if (raw === null || raw === undefined || raw === '') {
    return '—'
  }
  const precision = props.item.entry.ui.precision ?? 0
  const num = typeof raw === 'number' ? raw : Number(raw)
  if (!Number.isFinite(num)) {
    return String(raw)
  }
  return precision > 0 ? num.toFixed(precision) : String(Math.round(num))
})

// 最近一次更新时间，展示为相对时间
const updated_text = computed(() => {
  const at = props.item.state.updated_at
  if (!at || props.item.state.status === 'unknown') {
    return '尚未采集'
  }
  const diff = Date.now() - new Date(at).getTime()
  if (diff < 1000) {
    return '刚刚'
  }
  if (diff < 60_000) {
    return `${Math.floor(diff / 1000)} 秒前`
  }
  return `${Math.floor(diff / 60_000)} 分钟前`
})
</script>

<template>
  <div
    class="group flex flex-col rounded-xl border border-line bg-surface p-4 transition-colors hover:border-line-strong"
  >
    <div class="flex items-start gap-2">
      <span class="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full" :class="status.dot"></span>
      <div class="min-w-0 flex-1">
        <p class="truncate text-base font-medium text-ink-strong">{{ item.entry.name }}</p>
        <p class="readout truncate text-[13px] text-ink-faint">{{ item.entry.id }}</p>
      </div>
      <button
        v-if="!is_push"
        type="button"
        title="立即刷新"
        class="rounded-md px-1.5 py-0.5 text-sm text-ink-ghost opacity-0 transition-all hover:bg-raised hover:text-ink-body group-hover:opacity-100"
        @click="emit('refresh')"
      >
        ↻
      </button>
    </div>

    <div class="mt-3 flex-1">
      <!-- 数值型读数 -->
      <div v-if="item.entry.ui.type === 'metric'" class="flex items-baseline gap-1">
        <span class="readout text-4xl font-semibold text-ink">{{ display_value }}</span>
        <span v-if="item.entry.ui.unit" class="text-base text-ink-faint">{{
          item.entry.ui.unit
        }}</span>
      </div>

      <!-- 状态型徽标 -->
      <div v-else-if="item.entry.ui.type === 'status'" class="flex items-center gap-2">
        <span
          class="rounded-md border px-2 py-0.5 text-base font-medium"
          :class="[
            'border-current/30',
            status.text,
            item.state.status === 'ok' ? 'bg-emerald-500/10' : 'bg-surface-2',
          ]"
        >
          {{ item.state.text || display_value }}
        </span>
      </div>

      <!-- 文本型 -->
      <p v-else class="readout line-clamp-3 text-base text-ink-body">
        {{ item.state.text || display_value }}
      </p>
    </div>

    <div class="mt-3 flex items-center justify-between text-[13px] text-ink-ghost">
      <span :class="status.text">{{ status.label }}</span>
      <span class="readout">
        <template v-if="item.state.latency_ms">{{ item.state.latency_ms }}ms · </template>
        {{ updated_text }}
      </span>
    </div>

    <p
      v-if="item.state.error"
      class="readout mt-2 truncate rounded-md bg-rose-50 px-2 py-1 text-[13px] text-rose-700 dark:bg-rose-950/40 dark:text-rose-300"
      :title="item.state.error"
    >
      {{ item.state.error }}
    </p>
  </div>
</template>
