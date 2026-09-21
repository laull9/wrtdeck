<script setup lang="ts">
import { computed } from 'vue'
import ActionCard from '../components/ActionCard.vue'
import SourceCard from '../components/SourceCard.vue'
import type { LiveMode } from '../lib/live'
import type { Entry, SourceItem } from '../types'
// 品牌标识，由 scripts/make-icons.py 从 assets/WrtDeck.png 生成
import logo_url from '../assets/logo.png'

// Dashboard 视图：按分组渲染信息源卡片与动作按钮
const props = defineProps<{
  sources: SourceItem[]
  actions: Entry[]
  live: LiveMode
}>()
const emit = defineEmits<{
  refresh: [string]
  notify: [string, 'ok' | 'error']
  reload: []
}>()

// 通道名称：面板既可能直连服务端走推送，也可能被设备自带 Web 服务器转发后走拉取
const live_label = computed(() => {
  switch (props.live) {
    case 'sse':
      return '实时推送'
    case 'poll':
      return '定时刷新'
    default:
      return '已断开'
  }
})

// 通道说明：把「浏览器到底怎么拿到状态」讲清楚，排障时能少问一轮
const live_detail = computed(() => {
  switch (props.live) {
    case 'sse':
      return '状态由服务端调度器主动下发，浏览器不直接访问设备'
    case 'poll':
      return '面板被 LuCI 内嵌或长连接送不出事件时走的通道，页面切到后台会暂停'
    default:
      return '可以点右侧按钮手动重新拉取'
  }
})

// 按 group 分组，保持后端给出的顺序
const groups = computed(() => {
  const map = new Map<string, SourceItem[]>()
  for (const item of props.sources) {
    const key = item.entry.group || '未分组'
    const list = map.get(key)
    if (list) {
      list.push(item)
    } else {
      map.set(key, [item])
    }
  }
  return Array.from(map, ([name, items]) => ({ name, items }))
})

// 启用中的动作数量
const enabled_actions = computed(() => props.actions.filter((item) => item.enabled).length)
</script>

<template>
  <div class="flex flex-col gap-7">
    <section v-if="sources.length === 0" class="panel flex flex-col items-center gap-3 px-6 py-10 text-center">
      <img :src="logo_url" alt="WrtDeck" class="h-16 w-16" />
      <p class="text-base text-ink-body">注册表里还没有信息源。</p>
      <p class="text-sm text-ink-faint">
        切换到「注册表」页签新增一条 source，或者重启服务并加上 <code class="code-chip">-seed-demo</code> 写入自检示例。
      </p>
    </section>

    <section v-for="group in groups" :key="group.name" class="flex flex-col gap-3">
      <div class="flex items-baseline gap-2">
        <h2 class="text-base font-medium tracking-wide text-ink-body">{{ group.name }}</h2>
        <span class="readout text-[13px] text-ink-ghost">{{ group.items.length }} 项</span>
      </div>
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <SourceCard
          v-for="item in group.items"
          :key="item.entry.id"
          :item="item"
          @refresh="emit('refresh', item.entry.id)"
        />
      </div>
    </section>

    <section v-if="actions.length" class="flex flex-col gap-3">
      <div class="flex items-baseline gap-2">
        <h2 class="text-base font-medium tracking-wide text-ink-body">动作</h2>
        <span class="readout text-[13px] text-ink-ghost">{{ enabled_actions }}/{{ actions.length }} 已启用</span>
      </div>
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <ActionCard
          v-for="action in actions"
          :key="action.id"
          :entry="action"
          @notify="(text, tone) => emit('notify', text, tone)"
        />
      </div>
    </section>

    <section class="panel flex items-center justify-between gap-4 px-4 py-3 text-sm text-ink-faint">
      <span>
        实时通道
        <span :class="props.live === 'off' ? 'text-rose-600 dark:text-rose-400' : 'text-emerald-600 dark:text-emerald-400'">
          {{ live_label }}
        </span>
        ，{{ live_detail }}
      </span>
      <button type="button" class="btn btn-outline btn-sm" @click="emit('reload')">重新拉取</button>
    </section>
  </div>
</template>
