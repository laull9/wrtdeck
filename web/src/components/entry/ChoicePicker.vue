<script setup lang="ts">
import { computed } from 'vue'
import type { Choice } from '../../lib/entry_form'

// 选项卡片组：代替普通下拉框，让每个选项的行为在界面上直接可读
const props = defineProps<{
  options: Choice[]
  modelValue: string
}>()
const emit = defineEmits<{ 'update:modelValue': [string] }>()

// 当前选中项，用于在按钮下方展示它的行为说明
const selected = computed(() => props.options.find((item) => item.value === props.modelValue))
</script>

<template>
  <div class="flex flex-col gap-2">
    <div class="flex flex-wrap gap-2">
      <button
        v-for="option in options"
        :key="option.value"
        type="button"
        class="rounded-lg border px-3 py-1.5 text-sm transition-colors"
        :class="
          option.value === modelValue
            ? 'border-zinc-500 bg-zinc-700/60 text-zinc-100'
            : 'border-zinc-700 text-zinc-400 hover:border-zinc-600 hover:text-zinc-200'
        "
        @click="emit('update:modelValue', option.value)"
      >
        {{ option.label }}
      </button>
    </div>
    <p v-if="selected?.hint" class="field-hint">{{ selected.hint }}</p>
  </div>
</template>
