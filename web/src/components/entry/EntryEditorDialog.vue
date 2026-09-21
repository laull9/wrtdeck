<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { api } from '../../api'
import type { Entry, EntryKind } from '../../types'
import {
  DEFAULT_INTERVAL_MS,
  EXTRACT_CHOICES,
  KIND_CHOICES,
  MIN_INTERVAL_MS,
  SOURCE_UI_CHOICES,
  VALUE_TYPE_CHOICES,
  VARIANT_CHOICES,
  draft_from_entry,
  empty_draft,
  entry_from_draft,
  issue_map,
  validate_draft,
} from '../../lib/entry_form'
import ChoicePicker from './ChoicePicker.vue'
import FormField from './FormField.vue'
import FormSection from './FormSection.vue'
import ParamsForm from './ParamsForm.vue'
import TransportForm from './TransportForm.vue'

// 注册项编辑器：默认是结构化表单，「高级」页签直接编辑 JSON 原文
const props = defineProps<{
  open: boolean
  mode: 'new' | 'edit'
  entry: Entry | null
  groups: string[]
}>()
const emit = defineEmits<{
  'update:open': [boolean]
  saved: [Entry]
}>()

// 表单草稿
const draft = ref(empty_draft())
// 当前页签
const tab = ref<'form' | 'json'>('form')
// JSON 原文与解析状态
const json_text = ref('')
const json_entry = ref<Entry | null>(null)
const json_error = ref('')
// 提交中与接口错误
const saving = ref(false)
const submit_error = ref('')

// 编辑已有注册项时 ID 不允许改动，改 ID 等于新建另一条
const id_editable = computed(() => props.mode === 'new')

// 生效条目：表单模式由草稿生成，JSON 模式直接取解析结果
const effective = computed<Entry | null>(() =>
  tab.value === 'form' ? entry_from_draft(draft.value) : json_entry.value,
)

// 校验结果：表单模式直接校验草稿本身，JSON 模式校验解析出来的条目
// 注意不能先经过 entry_from_draft 再校验，那一步会丢掉还没填名字的参数行
const issues = computed(() => {
  if (tab.value === 'form') {
    return validate_draft(draft.value)
  }
  const entry = json_entry.value
  return entry ? validate_draft(draft_from_entry(entry)) : []
})
const errors = computed(() => issue_map(issues.value))

// 有错误或 JSON 不合法时禁止提交
const can_save = computed(() => !saving.value && issues.value.length === 0 && effective.value !== null)

// 每次打开时按模式重置编辑器状态
watch(
  () => props.open,
  (open) => {
    if (!open) {
      return
    }
    draft.value = props.mode === 'edit' && props.entry ? draft_from_entry(props.entry) : empty_draft('source')
    tab.value = 'form'
    json_text.value = ''
    json_entry.value = null
    json_error.value = ''
    submit_error.value = ''
    saving.value = false
  },
)

// 切换注册项类型时同步展示形态，避免留下与类型不匹配的 ui.type
function on_kind_change(value: string): void {
  const kind = value as EntryKind
  draft.value.kind = kind
  const current = draft.value.ui.type
  if (kind === 'action' && !['button'].includes(current)) {
    draft.value.ui.type = 'button'
  }
  if (kind === 'source' && !['metric', 'status', 'text'].includes(current)) {
    draft.value.ui.type = 'metric'
  }
}

// 解析 JSON 原文，成功时返回条目，失败时写入错误信息
function parse_json(): Entry | null {
  try {
    const parsed = JSON.parse(json_text.value) as Entry
    if (!parsed || typeof parsed !== 'object') {
      json_error.value = 'JSON 顶层必须是一个对象'
      json_entry.value = null
      return null
    }
    json_error.value = ''
    json_entry.value = parsed
    return parsed
  } catch (err) {
    json_error.value = err instanceof Error ? err.message : String(err)
    json_entry.value = null
    return null
  }
}

// 切换页签：表单转 JSON 直接序列化，JSON 转表单需要先解析成功
function switch_tab(next: 'form' | 'json'): void {
  if (next === tab.value) {
    return
  }
  if (next === 'json') {
    const payload = entry_from_draft(draft.value)
    json_text.value = JSON.stringify(payload, null, 2)
    json_entry.value = payload
    json_error.value = ''
    tab.value = 'json'
    return
  }
  const parsed = parse_json()
  if (!parsed) {
    return
  }
  draft.value = draft_from_entry(parsed)
  tab.value = 'form'
}

// 提交：把生效条目写入注册表
async function save(): Promise<void> {
  const entry = effective.value
  if (!entry) {
    return
  }
  saving.value = true
  submit_error.value = ''
  try {
    const saved = await api.put_entry(entry)
    emit('saved', saved ?? entry)
    emit('update:open', false)
  } catch (err) {
    submit_error.value = err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <DialogRoot :open="open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-black/70 backdrop-blur-sm" />
      <DialogContent
        class="fixed left-1/2 top-1/2 z-50 flex max-h-[88vh] w-[min(56rem,95vw)] -translate-x-1/2 -translate-y-1/2 flex-col rounded-xl border border-zinc-800 bg-zinc-900 shadow-2xl"
      >
        <header class="flex items-center gap-3 border-b border-zinc-800 px-5 py-3.5">
          <DialogTitle class="text-base font-semibold text-zinc-100">
            {{ mode === 'new' ? '新增注册项' : '编辑注册项' }}
          </DialogTitle>
          <DialogDescription class="sr-only">编辑注册项的表单与 JSON 配置</DialogDescription>
          <div class="ml-auto flex items-center gap-1 rounded-lg bg-black/40 p-0.5">
            <button
              type="button"
              class="rounded-md px-3 py-1 text-sm transition-colors"
              :class="tab === 'form' ? 'bg-zinc-700/70 text-zinc-100' : 'text-zinc-400 hover:text-zinc-200'"
              @click="switch_tab('form')"
            >
              表单编辑
            </button>
            <button
              type="button"
              class="rounded-md px-3 py-1 text-sm transition-colors"
              :class="tab === 'json' ? 'bg-zinc-700/70 text-zinc-100' : 'text-zinc-400 hover:text-zinc-200'"
              @click="switch_tab('json')"
            >
              JSON 高级
            </button>
          </div>
        </header>

        <div class="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          <!-- 结构化表单 -->
          <div v-if="tab === 'form'" class="flex flex-col gap-4">
            <FormSection title="基础信息" desc="ID 是注册项的唯一标识，也是接口路径的一部分。">
              <div class="grid gap-3 sm:grid-cols-2">
                <FormField
                  label="ID"
                  required
                  :error="errors.id"
                  :hint="id_editable ? '只允许小写字母、数字、- 和 _' : '编辑时不可修改，如需改 ID 请新建后删除旧条目'"
                >
                  <input
                    v-model="draft.id"
                    class="field readout"
                    :readonly="!id_editable"
                    :class="[errors.id ? 'border-rose-700' : '', id_editable ? '' : 'opacity-60']"
                    placeholder="livingroom-temp"
                  />
                </FormField>
                <FormField label="名称" required :error="errors.name" hint="显示在面板卡片或按钮上">
                  <input v-model="draft.name" class="field" placeholder="客厅温度" />
                </FormField>
              </div>

              <FormField label="类型">
                <ChoicePicker
                  :model-value="draft.kind"
                  :options="KIND_CHOICES"
                  @update:model-value="on_kind_change($event)"
                />
              </FormField>

              <div class="grid gap-3 sm:grid-cols-2">
                <FormField label="分组" hint="同组的信息源会归到面板上的同一个区块">
                  <input v-model="draft.group" class="field" list="owdash-group-options" placeholder="未分组" />
                  <datalist id="owdash-group-options">
                    <option v-for="name in groups" :key="name" :value="name"></option>
                  </datalist>
                </FormField>
                <FormField label="启用状态" hint="停用后不再调度，动作按钮也会被禁用">
                  <label class="flex items-center gap-2 text-base text-zinc-300">
                    <input v-model="draft.enabled" type="checkbox" class="field-check" />
                    {{ draft.enabled ? '启用' : '停用' }}
                  </label>
                </FormField>
              </div>
            </FormSection>

            <!-- 动作先声明参数，再在传输字段里引用 -->
            <FormSection
              v-if="draft.kind === 'action'"
              title="运行参数"
              desc="触发动作时由界面收集的入参，可在下面的传输字段里用 ${params.名称} 引用。"
            >
              <ParamsForm :params="draft.params" :errors="errors" />
            </FormSection>

            <FormSection
              title="传输方式"
              :desc="
                draft.kind === 'source'
                  ? '决定每次采集如何从设备取值。'
                  : '决定点击按钮后如何把指令发给设备。'
              "
            >
              <TransportForm :transport="draft.transport" :errors="errors" />
            </FormSection>

            <FormSection
              v-if="draft.kind === 'source'"
              title="采集与取值"
              desc="调度周期决定采集频率，取值规则决定从响应里挑出哪个值。"
            >
              <FormField
                label="轮询间隔（ms）"
                required
                :error="errors.interval_ms"
                :hint="`服务端最小间隔 ${MIN_INTERVAL_MS}ms，默认 ${DEFAULT_INTERVAL_MS}ms`"
              >
                <input
                  v-model.number="draft.interval_ms"
                  type="number"
                  :min="MIN_INTERVAL_MS"
                  step="500"
                  class="field readout"
                />
              </FormField>

              <FormField label="取值方式">
                <ChoicePicker v-model="draft.extract.type" :options="EXTRACT_CHOICES" />
              </FormField>

              <FormField
                v-if="draft.extract.type === 'json'"
                label="JSON 路径"
                required
                :error="errors['extract.path']"
                hint="点号分层，数组用下标，例如 data.items.0.value"
              >
                <input v-model="draft.extract.path" class="field readout" placeholder="server.uptime_s" />
              </FormField>

              <div v-if="draft.extract.type === 'regex'" class="grid gap-3 sm:grid-cols-[1fr_7rem]">
                <FormField label="正则表达式" required :error="errors['extract.pattern']">
                  <input v-model="draft.extract.pattern" class="field field-code" placeholder="temp=(\d+\.\d+)" />
                </FormField>
                <FormField label="捕获分组" hint="0 表示整个匹配">
                  <input v-model.number="draft.extract.group" type="number" min="0" class="field readout" />
                </FormField>
              </div>

              <FormField label="取值类型" hint="决定前端如何格式化，自动推断时按原样展示">
                <ChoicePicker v-model="draft.extract.value_type" :options="VALUE_TYPE_CHOICES" />
              </FormField>
            </FormSection>

            <FormSection
              title="展示方式"
              :desc="
                draft.kind === 'source' ? '决定这张卡片在面板上长什么样。' : '决定这个按钮在面板上的外观。'
              "
            >
              <FormField v-if="draft.kind === 'source'" label="卡片形态">
                <ChoicePicker v-model="draft.ui.type" :options="SOURCE_UI_CHOICES" />
              </FormField>

              <div v-if="draft.kind === 'source' && draft.ui.type === 'metric'" class="grid gap-3 sm:grid-cols-2">
                <FormField label="单位" hint="显示在读数右侧，例如 °C、s、Mbps">
                  <input v-model="draft.ui.unit" class="field" placeholder="°C" />
                </FormField>
                <FormField label="小数位" hint="0 表示取整">
                  <input v-model.number="draft.ui.precision" type="number" min="0" max="6" class="field readout" />
                </FormField>
              </div>

              <FormField v-if="draft.kind === 'action'" label="按钮配色">
                <ChoicePicker v-model="draft.ui.variant" :options="VARIANT_CHOICES" />
              </FormField>

              <FormField label="排序权重" hint="数值越小越靠前，同组内按此排序">
                <input v-model.number="draft.ui.order" type="number" class="field readout" />
              </FormField>
            </FormSection>

            <FormSection
              v-if="draft.kind === 'action'"
              title="二次确认"
              desc="开启后点击按钮会先弹窗确认，可用来拦住误触的不可逆操作。"
            >
              <FormField label="启用确认">
                <label class="flex items-center gap-2 text-base text-zinc-300">
                  <input v-model="draft.ui.confirm_enabled" type="checkbox" class="field-check" />
                  {{ draft.ui.confirm_enabled ? '执行前需要确认' : '点击即执行' }}
                </label>
              </FormField>
              <div v-if="draft.ui.confirm_enabled" class="grid gap-3 sm:grid-cols-2">
                <FormField label="弹窗标题" hint="留空则用注册项名称">
                  <input v-model="draft.ui.confirm_title" class="field" placeholder="确认下发角度？" />
                </FormField>
                <FormField label="弹窗说明">
                  <input v-model="draft.ui.confirm_message" class="field" placeholder="将向舵机控制器发送角度指令。" />
                </FormField>
              </div>
            </FormSection>
          </div>

          <!-- JSON 高级编辑 -->
          <div v-else class="flex flex-col gap-3">
            <p class="field-hint">
              这里就是提交给 <code class="rounded bg-black/50 px-1">PUT /api/v1/registry/{id}</code> 的原始报文，适合批量调整或复制配置。切回表单时会被重新解析。
            </p>
            <textarea
              v-model="json_text"
              spellcheck="false"
              class="field field-code min-h-[26rem] w-full resize-y leading-relaxed"
              @input="parse_json"
            ></textarea>
            <p v-if="json_error" class="field-error">JSON 语法错误：{{ json_error }}</p>
          </div>
        </div>

        <footer class="flex items-center gap-3 border-t border-zinc-800 px-5 py-3.5">
          <div class="min-w-0 flex-1 text-sm">
            <p v-if="submit_error" class="truncate text-rose-400" :title="submit_error">
              保存失败：{{ submit_error }}
            </p>
            <p v-else-if="issues.length" class="truncate text-amber-400" :title="issues[0]?.message">
              还有 {{ issues.length }} 处需要修正：{{ issues[0]?.message }}
            </p>
            <p v-else class="text-zinc-500">校验通过，可以保存</p>
          </div>
          <button type="button" class="btn btn-ghost" @click="emit('update:open', false)">取消</button>
          <button type="button" class="btn btn-primary" :disabled="!can_save" @click="save">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </footer>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
