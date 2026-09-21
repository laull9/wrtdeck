<script setup lang="ts">
// 表单项外壳：统一标签、必填标记、说明与错误提示
defineProps<{
  label: string
  hint?: string
  error?: string
  required?: boolean
}>()
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <span class="field-label">
      {{ label }}
      <span v-if="required" class="text-rose-400">*</span>
    </span>
    <slot />
    <p v-if="error" class="field-error">{{ error }}</p>
    <p v-else-if="hint" class="field-hint">{{ hint }}</p>
  </div>
</template>
