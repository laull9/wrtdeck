<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { api } from '../api'
import type { Entry } from '../types'

// 一个动作卡片，带参或需要确认时先弹窗收集输入
const props = defineProps<{ entry: Entry }>()
const emit = defineEmits<{ notify: [string, 'ok' | 'error'] }>()

// 弹窗显隐与执行中状态
const open = ref(false)
const running = ref(false)
// 参数取值，键为参数名
const values = ref<Record<string, unknown>>({})

// 参数声明列表，保持声明顺序
const param_list = computed(() => Object.entries(props.entry.params ?? {}))

// 是否需要先弹窗：有参数或要求二次确认
const need_dialog = computed(() => param_list.value.length > 0 || !!props.entry.ui.confirm?.enabled)

// 按钮配色
const button_class = computed(() => {
  const variant = props.entry.ui.variant ?? 'primary'
  if (variant === 'danger') {
    return 'bg-rose-600/90 text-white hover:bg-rose-500'
  }
  if (variant === 'ghost') {
    return 'border border-zinc-700 text-zinc-300 hover:border-zinc-500 hover:text-zinc-100'
  }
  return 'bg-zinc-100 text-zinc-900 hover:bg-white'
})

// 按参数声明生成初始值
function initial_values(): Record<string, unknown> {
  const next: Record<string, unknown> = {}
  for (const [name, spec] of param_list.value) {
    next[name] = spec.default ?? (spec.type === 'boolean' ? false : '')
  }
  return next
}

// 点击按钮：需要弹窗则打开，否则直接执行
function on_click(): void {
  if (!props.entry.enabled) {
    return
  }
  if (need_dialog.value) {
    values.value = initial_values()
    open.value = true
    return
  }
  void run()
}

// 调用后端执行动作
async function run(): Promise<void> {
  running.value = true
  try {
    const result = await api.run_action(props.entry.id, values.value)
    emit('notify', result.message || `${props.entry.name} 已执行`, 'ok')
    open.value = false
  } catch (err) {
    emit('notify', err instanceof Error ? err.message : String(err), 'error')
  } finally {
    running.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-2 rounded-xl border border-zinc-800 bg-zinc-900/40 p-4">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <p class="truncate text-base font-medium" :class="entry.enabled ? 'text-zinc-200' : 'text-zinc-500'">
          {{ entry.name }}
        </p>
        <p class="readout truncate text-[13px] text-zinc-500">{{ entry.transport.type }} · {{ entry.id }}</p>
      </div>
      <button
        type="button"
        class="btn"
        :class="button_class"
        :disabled="!entry.enabled || running"
        @click="on_click"
      >
        {{ entry.enabled ? (running ? '执行中…' : '执行') : '已禁用' }}
      </button>
    </div>

    <p v-if="param_list.length" class="readout text-[13px] text-zinc-600">
      参数：{{ param_list.map(([name]) => name).join('、') }}
    </p>

    <DialogRoot :open="open" @update:open="open = $event">
      <DialogPortal>
        <DialogOverlay class="fixed inset-0 z-40 bg-black/70 backdrop-blur-sm" />
        <DialogContent
          class="fixed left-1/2 top-1/2 z-50 w-[min(30rem,92vw)] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-zinc-800 bg-zinc-900 p-5 shadow-2xl"
        >
          <DialogTitle class="text-base font-semibold text-zinc-100">
            {{ entry.ui.confirm?.title || entry.name }}
          </DialogTitle>
          <DialogDescription class="mt-1 text-sm leading-relaxed text-zinc-400">
            {{ entry.ui.confirm?.message || `将执行动作 ${entry.name}。` }}
          </DialogDescription>

          <div v-if="param_list.length" class="mt-4 flex flex-col gap-3">
            <label v-for="[name, spec] in param_list" :key="name" class="flex flex-col gap-1.5">
              <span class="field-label">
                {{ spec.label || name }}
                <span v-if="spec.required" class="text-rose-400">*</span>
              </span>

              <select v-if="spec.type === 'select'" v-model="values[name]" class="field">
                <option v-for="option in spec.options ?? []" :key="option" :value="option">
                  {{ option }}
                </option>
              </select>

              <input
                v-else-if="spec.type === 'boolean'"
                v-model="values[name]"
                type="checkbox"
                class="field-check"
              />

              <input
                v-else
                v-model="values[name]"
                :type="spec.type === 'number' ? 'number' : spec.type === 'secret' ? 'password' : 'text'"
                :min="spec.min"
                :max="spec.max"
                class="field readout"
              />

              <span v-if="spec.min !== undefined || spec.max !== undefined" class="text-[13px] text-zinc-600">
                取值范围 {{ spec.min ?? '不限' }} ~ {{ spec.max ?? '不限' }}
              </span>
            </label>
          </div>

          <div class="mt-5 flex items-center justify-end gap-2">
            <button type="button" class="btn btn-ghost" @click="open = false">取消</button>
            <button type="button" class="btn btn-primary" :disabled="running" @click="run">
              {{ running ? '执行中…' : '执行' }}
            </button>
          </div>
        </DialogContent>
      </DialogPortal>
    </DialogRoot>
  </div>
</template>
