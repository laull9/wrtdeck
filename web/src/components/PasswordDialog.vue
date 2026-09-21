<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { ApiError } from '../api'
import { auth, change_password } from '../lib/auth'

// 对话框显隐由父组件控制；forced 为真时表示服务端要求先改口令，界面不许关闭
const props = defineProps<{ open: boolean; forced: boolean }>()
// 关闭与改完的通知
const emit = defineEmits<{ 'update:open': [boolean]; done: [] }>()

// 表单草稿
const current = ref('')
const next = ref('')
const confirm = ref('')
const busy = ref(false)
const error = ref('')

// 每次打开时清空上一次的输入，避免口令残留在界面上
watch(
  () => props.open,
  (open) => {
    if (open) {
      current.value = ''
      next.value = ''
      confirm.value = ''
      error.value = ''
    }
  },
)

// 关闭对话框；强制模式下不允许关闭
function close(): void {
  if (props.forced) {
    return
  }
  emit('update:open', false)
}

// 提交改口令。客户端只做能立刻说清的基本校验，强度策略以服务端为准。
async function submit(): Promise<void> {
  if (busy.value) {
    return
  }
  error.value = ''
  if (next.value !== confirm.value) {
    error.value = '两次输入的新口令不一致'
    return
  }
  if (next.value === current.value) {
    error.value = '新口令与当前口令相同'
    return
  }
  busy.value = true
  try {
    await change_password(current.value, next.value)
    emit('done')
    emit('update:open', false)
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : String(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <DialogRoot :open="props.open" @update:open="close">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm dark:bg-black/70" />
      <DialogContent
        class="fixed left-1/2 top-1/2 z-50 w-[min(26rem,92vw)] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-line bg-floating p-5 shadow-2xl"
        @escape-key-down.prevent="close"
        @pointer-down-outside.prevent="close"
      >
        <DialogTitle class="text-base font-semibold text-ink">
          {{ props.forced ? '请先修改初始口令' : '修改登录口令' }}
        </DialogTitle>
        <DialogDescription class="mt-1 text-xs text-ink-muted">
          {{ props.forced ? '首次使用必须修改初始口令' : '修改后需重新登录' }}
        </DialogDescription>

        <label class="field-label mt-4 block" for="pw-current">当前口令</label>
        <input
          id="pw-current"
          v-model="current"
          type="password"
          autocomplete="current-password"
          class="field mt-1.5"
          :disabled="busy"
        />

        <label class="field-label mt-3 block" for="pw-next">新口令</label>
        <input
          id="pw-next"
          v-model="next"
          type="password"
          autocomplete="new-password"
          class="field mt-1.5"
          :disabled="busy"
        />
        <p class="field-hint mt-1.5">至少 8 个字符，且字母、数字、符号里至少含两类。</p>

        <label class="field-label mt-3 block" for="pw-confirm">确认新口令</label>
        <input
          id="pw-confirm"
          v-model="confirm"
          type="password"
          autocomplete="new-password"
          class="field mt-1.5"
          :disabled="busy"
          @keydown.enter="submit"
        />

        <p v-if="error" class="field-error mt-3">{{ error }}</p>

        <div class="mt-5 flex items-center justify-end gap-2">
          <button v-if="!props.forced" type="button" class="btn btn-outline" @click="close">取消</button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="busy || !current || !next || !confirm"
            @click="submit"
          >
            {{ busy ? '提交中…' : '修改口令' }}
          </button>
        </div>

        <p v-if="props.forced && auth.token" class="field-hint mt-3">
          也可以在设备上执行 <code class="code-chip">/etc/init.d/wrtdeck password</code> 重置。
        </p>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
