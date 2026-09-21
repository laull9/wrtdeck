<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '../api'
import type { Entry } from '../types'
import EntryEditorDialog from '../components/entry/EntryEditorDialog.vue'

// 注册表视图：查看、编辑、启停、删除注册项
const emit = defineEmits<{
  notify: [string, 'ok' | 'error']
  reload: []
}>()

// 注册项列表与加载状态
const entries = ref<Entry[]>([])
const loading = ref(true)
const error = ref('')

// 编辑器状态
const editor_open = ref(false)
const editor_mode = ref<'new' | 'edit'>('new')
const editing = ref<Entry | null>(null)

// 正在切换启用状态的注册项 ID，避免重复点击
const toggling = ref('')

// 按类型拆成两组展示
const sources = computed(() => entries.value.filter((item) => item.kind === 'source'))
const actions = computed(() => entries.value.filter((item) => item.kind === 'action'))

// 已有分组名，交给编辑器做输入联想
const groups = computed(() =>
  Array.from(new Set(entries.value.map((item) => item.group).filter((name): name is string => !!name))),
)

// 拉取注册表
async function load(): Promise<void> {
  loading.value = true
  try {
    const payload = await api.registry()
    entries.value = payload.entries ?? []
    error.value = ''
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

// 打开新增编辑器
function open_new(): void {
  editor_mode.value = 'new'
  editing.value = null
  editor_open.value = true
}

// 打开编辑现有注册项
function open_edit(entry: Entry): void {
  editor_mode.value = 'edit'
  editing.value = entry
  editor_open.value = true
}

// 保存成功后的收尾：刷新列表并让面板同步
function on_saved(entry: Entry): void {
  emit('notify', `已保存 ${entry.id}`, 'ok')
  void load()
  emit('reload')
}

// 就地切换启用状态，停用会同时停止调度
async function toggle_enabled(entry: Entry): Promise<void> {
  toggling.value = entry.id
  try {
    await api.put_entry({ ...entry, enabled: !entry.enabled })
    emit('notify', `${entry.name} 已${entry.enabled ? '停用' : '启用'}`, 'ok')
    await load()
    emit('reload')
  } catch (err) {
    emit('notify', err instanceof Error ? err.message : String(err), 'error')
  } finally {
    toggling.value = ''
  }
}

// 手动触发一次信息源采集
async function refresh_source(entry: Entry): Promise<void> {
  try {
    await api.refresh_source(entry.id)
    emit('notify', `已触发 ${entry.name} 采集`, 'ok')
  } catch (err) {
    emit('notify', err instanceof Error ? err.message : String(err), 'error')
  }
}

// 删除一条注册项，删除前需要用户确认
async function remove(entry: Entry): Promise<void> {
  if (!window.confirm(`确认删除 ${entry.name}（${entry.id}）？此操作会同步停止其调度。`)) {
    return
  }
  try {
    await api.delete_entry(entry.id)
    emit('notify', `已删除 ${entry.id}`, 'ok')
    await load()
    emit('reload')
  } catch (err) {
    emit('notify', err instanceof Error ? err.message : String(err), 'error')
  }
}

// 把传输配置压成一行摘要
function transport_summary(entry: Entry): string {
  const t = entry.transport
  if (t.http) {
    return `${t.type} ${t.http.method ?? 'GET'} ${t.http.url}`
  }
  if (t.udp) {
    return `${t.type} ${t.udp.address}`
  }
  if (t.tcp) {
    return `${t.type} ${t.tcp.address}`
  }
  if (t.mqtt) {
    return `${t.type} ${t.mqtt.broker}/${t.mqtt.topic}`
  }
  if (t.exec) {
    return `${t.type} ${t.exec.executable}`
  }
  return t.type
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-5">
    <div class="flex items-center gap-3">
      <div class="min-w-0">
        <h1 class="text-base font-semibold text-ink-strong">注册表</h1>
      </div>
      <div class="ml-auto flex items-center gap-2">
        <button type="button" class="btn btn-outline" @click="load">刷新</button>
        <button type="button" class="btn btn-primary" @click="open_new">新增注册项</button>
      </div>
    </div>

    <p
      v-if="error"
      class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-base text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300"
    >
      {{ error }}
    </p>

    <div v-if="loading" class="panel p-5 text-base text-ink-muted">正在读取注册表…</div>

    <template v-else>
      <section
        v-for="group in [
          { title: '信息源', rows: sources },
          { title: '动作', rows: actions },
        ]"
        :key="group.title"
        class="flex flex-col gap-2"
      >
        <div class="flex items-baseline gap-2">
          <h2 class="text-base font-medium text-ink-body">{{ group.title }}</h2>
          <span class="readout text-sm text-ink-ghost">{{ group.rows.length }} 项</span>
        </div>

        <div class="overflow-hidden rounded-xl border border-line">
          <table class="w-full border-collapse text-base">
            <thead>
              <tr class="bg-surface-2 text-left text-[13px] uppercase tracking-wide text-ink-faint">
                <th class="px-3 py-2 font-medium">ID</th>
                <th class="px-3 py-2 font-medium">名称</th>
                <th class="px-3 py-2 font-medium">分组</th>
                <th class="px-3 py-2 font-medium">传输</th>
                <th class="px-3 py-2 font-medium">状态</th>
                <th class="px-3 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="group.rows.length === 0">
                <td colspan="6" class="px-3 py-4 text-center text-sm text-ink-ghost">暂无数据</td>
              </tr>
              <tr
                v-for="entry in group.rows"
                :key="entry.id"
                class="border-t border-line transition-colors hover:bg-surface-2"
              >
                <td class="readout px-3 py-2 text-ink-body">{{ entry.id }}</td>
                <td class="px-3 py-2 text-ink-strong">{{ entry.name }}</td>
                <td class="px-3 py-2 text-ink-faint">{{ entry.group || '—' }}</td>
                <td
                  class="readout max-w-[22rem] truncate px-3 py-2 text-ink-faint"
                  :title="transport_summary(entry)"
                >
                  {{ transport_summary(entry) }}
                </td>
                <td class="px-3 py-2">
                  <button
                    type="button"
                    class="rounded-md px-2 py-0.5 text-[13px] transition-colors disabled:opacity-50"
                    :class="
                      entry.enabled
                        ? 'bg-emerald-500/10 text-emerald-700 hover:bg-emerald-500/20 dark:text-emerald-300'
                        : 'bg-raised text-ink-faint hover:bg-raised-strong'
                    "
                    :disabled="toggling === entry.id"
                    :title="entry.enabled ? '点击停用' : '点击启用'"
                    @click="toggle_enabled(entry)"
                  >
                    {{ entry.enabled ? '已启用' : '已停用' }}
                  </button>
                </td>
                <td class="whitespace-nowrap px-3 py-2 text-right">
                  <button
                    v-if="entry.kind === 'source'"
                    type="button"
                    class="btn btn-sm btn-ghost"
                    @click="refresh_source(entry)"
                  >
                    采集
                  </button>
                  <button type="button" class="btn btn-sm btn-ghost" @click="open_edit(entry)">编辑</button>
                  <button type="button" class="btn btn-sm btn-danger" @click="remove(entry)">删除</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>

    <EntryEditorDialog
      v-model:open="editor_open"
      :mode="editor_mode"
      :entry="editing"
      :groups="groups"
      @saved="on_saved"
    />
  </div>
</template>
