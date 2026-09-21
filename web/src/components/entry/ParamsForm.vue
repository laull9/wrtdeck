<script setup lang="ts">
import FormField from './FormField.vue'
import { PARAM_TYPE_CHOICES, type ParamDraft } from '../../lib/entry_model'
import type { ParamType } from '../../types'

// 运行参数编辑器：为动作声明触发时需要在界面上填写的参数
// params 是父级草稿上的响应式数组，这里直接增删改它的元素
const props = defineProps<{
  params: ParamDraft[]
  errors: Record<string, string>
}>()

// 追加一个字符串参数，参数名留空待填
function add_param(): void {
  props.params.push({
    name: '',
    type: 'string',
    label: '',
    required: true,
    default_text: '',
    min_text: '',
    max_text: '',
    options_text: '',
  })
}

// 删除指定参数
function remove_param(index: number): void {
  props.params.splice(index, 1)
}

// 切换类型时清掉不再适用的约束，避免提交出互相矛盾的声明
function on_type_change(row: ParamDraft, type: ParamType): void {
  row.type = type
  if (type !== 'number') {
    row.min_text = ''
    row.max_text = ''
  }
  if (type !== 'select') {
    row.options_text = ''
  }
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <p v-if="params.length === 0" class="field-hint">
      尚未声明参数，点击下方按钮添加。参数会出现在执行确认弹窗里，并可通过
      <code class="code-chip">${params.参数名}</code> 注入到地址、载荷或请求头中。
    </p>

    <div
      v-for="(row, index) in params"
      :key="index"
      class="flex flex-col gap-3 rounded-xl border border-line bg-surface-2 p-3"
    >
      <div class="grid gap-3 sm:grid-cols-[1fr_9rem_1fr_auto]">
        <FormField label="参数名" required :error="errors[`params.${index}.name`]">
          <input v-model="row.name" class="field readout" placeholder="angle" />
        </FormField>
        <FormField label="类型">
          <select
            :value="row.type"
            class="field"
            @change="on_type_change(row, ($event.target as HTMLSelectElement).value as ParamType)"
          >
            <option v-for="choice in PARAM_TYPE_CHOICES" :key="choice.value" :value="choice.value">
              {{ choice.label }}
            </option>
          </select>
        </FormField>
        <FormField label="界面标签" hint="留空则直接显示参数名">
          <input v-model="row.label" class="field" placeholder="舵机角度" />
        </FormField>
        <div class="flex items-end pb-1.5">
          <button type="button" class="btn btn-outline btn-sm" @click="remove_param(index)">删除</button>
        </div>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <!-- 布尔参数直接用下拉表达"不设默认 / 默认开 / 默认关" -->
        <FormField v-if="row.type === 'boolean'" label="默认值">
          <select v-model="row.default_text" class="field">
            <option value="">不设默认</option>
            <option value="true">默认开启</option>
            <option value="false">默认关闭</option>
          </select>
        </FormField>
        <FormField v-else label="默认值" hint="留空表示不预填">
          <input
            v-model="row.default_text"
            :type="row.type === 'number' ? 'number' : row.type === 'secret' ? 'password' : 'text'"
            autocomplete="off"
            class="field readout"
          />
        </FormField>

        <FormField label="是否必填" hint="必填参数在执行弹窗里会标记星号">
          <label class="flex items-center gap-2 text-base text-ink-body">
            <input v-model="row.required" type="checkbox" class="field-check" />
            {{ row.required ? '必填' : '可留空' }}
          </label>
        </FormField>
      </div>

      <div v-if="row.type === 'number'" class="grid gap-3 sm:grid-cols-2">
        <FormField label="最小值" :error="errors[`params.${index}.min_text`]">
          <input v-model="row.min_text" type="number" class="field readout" />
        </FormField>
        <FormField label="最大值" :error="errors[`params.${index}.max_text`]">
          <input v-model="row.max_text" type="number" class="field readout" />
        </FormField>
      </div>

      <FormField
        v-if="row.type === 'select'"
        label="可选值"
        required
        :error="errors[`params.${index}.options_text`]"
        hint="用逗号或换行分隔，例如 on, off, auto"
      >
        <input v-model="row.options_text" class="field" placeholder="on, off" />
      </FormField>
    </div>

    <button type="button" class="btn btn-outline btn-sm self-start" @click="add_param">
      + 添加参数
    </button>
  </div>
</template>
