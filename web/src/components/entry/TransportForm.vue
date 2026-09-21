<script setup lang="ts">
import { computed } from 'vue'
import ChoicePicker from './ChoicePicker.vue'
import FormField from './FormField.vue'
import type { EntryKind } from '../../types'
import {
  ENCODING_CHOICES,
  HTTP_METHODS,
  MQTT_MODE_CHOICES,
  TRANSPORT_CHOICES,
  type TransportDraft,
} from '../../lib/entry_model'

// 传输配置表单：只渲染当前传输类型需要的字段，切换类型时其他分支的填写内容会保留
// transport 是父级草稿上的响应式对象，这里直接改它的字段来触发父级更新
const props = defineProps<{
  transport: TransportDraft
  kind: EntryKind
  errors: Record<string, string>
}>()

// MQTT 订阅模式只对信息源开放，动作侧直接过滤掉该选项
const mqtt_modes = computed(() =>
  props.kind === 'source'
    ? MQTT_MODE_CHOICES
    : MQTT_MODE_CHOICES.filter((item) => item.value === 'publish'),
)

// 当前是否处于 MQTT 订阅模式，订阅不需要载荷与保留标记
const mqtt_subscribe = computed(() => props.transport.mqtt_mode === 'subscribe')

// 追加一行请求头
function add_header(): void {
  props.transport.http_headers.push({ key: '', value: '' })
}

// 删除指定行请求头
function remove_header(index: number): void {
  props.transport.http_headers.splice(index, 1)
}

// 追加一条命令参数
function add_arg(): void {
  props.transport.exec_args.push('')
}

// 删除指定命令参数
function remove_arg(index: number): void {
  props.transport.exec_args.splice(index, 1)
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <FormField label="传输方式">
      <ChoicePicker v-model="transport.type" :options="TRANSPORT_CHOICES" />
    </FormField>

    <!-- HTTP -->
    <template v-if="transport.type === 'http'">
      <div class="grid gap-3 sm:grid-cols-[9rem_1fr]">
        <FormField label="方法">
          <select v-model="transport.http_method" class="field">
            <option v-for="method in HTTP_METHODS" :key="method" :value="method">{{ method }}</option>
          </select>
        </FormField>
        <FormField
          label="接口地址"
          required
          :error="errors['transport.http_url']"
          hint="支持 ${params.xxx} 与 ${secret.xxx} 模板变量"
        >
          <input
            v-model="transport.http_url"
            class="field readout"
            placeholder="http://192.168.1.20/api/temperature"
          />
        </FormField>
      </div>

      <FormField label="请求头" hint="键值都会参与模板渲染，常用于放置 Bearer Token">
        <div class="flex flex-col gap-2">
          <div
            v-for="(row, index) in transport.http_headers"
            :key="index"
            class="flex items-start gap-2"
          >
            <input
              v-model="row.key"
              class="field readout flex-1"
              placeholder="Authorization"
              :class="errors[`transport.http_headers.${index}.key`] ? 'border-rose-500 dark:border-rose-700' : ''"
            />
            <input
              v-model="row.value"
              class="field readout flex-[1.4]"
              placeholder="Bearer ${secret.api_token}"
              :class="errors[`transport.http_headers.${index}.value`] ? 'border-rose-500 dark:border-rose-700' : ''"
            />
            <button type="button" class="btn btn-outline btn-sm" @click="remove_header(index)">删除</button>
          </div>
          <button type="button" class="btn btn-outline btn-sm self-start" @click="add_header">
            + 添加请求头
          </button>
          <p v-if="errors['transport.http_headers.0.key']" class="field-error">
            {{ errors['transport.http_headers.0.key'] }}
          </p>
        </div>
      </FormField>

      <div class="grid gap-3 sm:grid-cols-[1fr_9rem]">
        <FormField label="请求体" hint="GET 一般留空；同样支持模板变量，适合下发 JSON 指令">
          <textarea
            v-model="transport.http_body"
            class="field field-area field-code min-h-[5rem]"
            spellcheck="false"
            :class="errors['transport.http_body'] ? 'border-rose-500 dark:border-rose-700' : ''"
            placeholder='{"target": "${params.channel}"}'
          ></textarea>
        </FormField>
        <FormField label="超时（ms）" hint="留空或 0 使用服务端默认值">
          <input v-model.number="transport.http_timeout_ms" type="number" min="0" class="field readout" />
        </FormField>
      </div>
      <p v-if="errors['transport.http_body']" class="field-error">{{ errors['transport.http_body'] }}</p>
    </template>

    <!-- TCP / UDP 共用一套字段 -->
    <template v-else-if="transport.type === 'tcp' || transport.type === 'udp'">
      <FormField
        label="目标地址"
        required
        :error="errors['transport.net_address']"
        hint="host:port 形式，支持模板变量"
      >
        <input
          v-model="transport.net_address"
          class="field readout"
          placeholder="192.168.1.20:9000"
          :class="errors['transport.net_address'] ? 'border-rose-500 dark:border-rose-700' : ''"
        />
      </FormField>

      <FormField label="载荷编码">
        <ChoicePicker v-model="transport.net_encoding" :options="ENCODING_CHOICES" />
      </FormField>

      <FormField
        label="发送载荷"
        :error="errors['transport.net_payload']"
        hint="支持 ${params.xxx} 模板变量，例如 SERVO:${params.angle}"
      >
        <textarea
          v-model="transport.net_payload"
          class="field field-area field-code min-h-[4.5rem]"
          spellcheck="false"
          :class="errors['transport.net_payload'] ? 'border-rose-500 dark:border-rose-700' : ''"
        ></textarea>
      </FormField>

      <div class="grid gap-3 sm:grid-cols-2">
        <FormField label="应答处理" :hint="transport.type === 'udp' ? 'UDP 建议关闭，除非设备会回包' : '打开后会把应答内容作为取值来源'">
          <label class="flex items-center gap-2 text-base text-ink-body">
            <input v-model="transport.net_expect_reply" type="checkbox" class="field-check" />
            {{ transport.net_expect_reply ? '等待应答' : '只发送，不等应答' }}
          </label>
        </FormField>
        <FormField label="超时（ms）" hint="留空或 0 使用服务端默认值">
          <input v-model.number="transport.net_timeout_ms" type="number" min="0" class="field readout" />
        </FormField>
      </div>
    </template>

    <!-- MQTT -->
    <template v-else-if="transport.type === 'mqtt'">
      <FormField label="工作模式" :error="errors['transport.mqtt_mode']">
        <ChoicePicker v-model="transport.mqtt_mode" :options="mqtt_modes" />
      </FormField>

      <div class="grid gap-3 sm:grid-cols-2">
        <FormField
          label="Broker 地址"
          required
          :error="errors['transport.mqtt_broker']"
          hint="可省略 scheme，默认按 mqtt:// 处理"
        >
          <input
            v-model="transport.mqtt_broker"
            class="field readout"
            placeholder="mqtt://192.168.1.20:1883"
            :class="errors['transport.mqtt_broker'] ? 'border-rose-500 dark:border-rose-700' : ''"
          />
        </FormField>
        <FormField
          label="主题"
          required
          :error="errors['transport.mqtt_topic']"
          :hint="
            mqtt_subscribe
              ? '支持 + 与 # 通配符，例如 home/+/temperature'
              : '发布到该主题，例如 home/livingroom/temp/set'
          "
        >
          <input
            v-model="transport.mqtt_topic"
            class="field readout"
            :class="errors['transport.mqtt_topic'] ? 'border-rose-500 dark:border-rose-700' : ''"
          />
        </FormField>
      </div>

      <div class="grid gap-3 sm:grid-cols-3">
        <FormField label="客户端 ID" hint="留空时由服务端按 Broker 生成">
          <input v-model="transport.mqtt_client_id" class="field readout" placeholder="wrtdeck" />
        </FormField>
        <FormField label="用户名">
          <input v-model="transport.mqtt_username" class="field readout" />
        </FormField>
        <FormField label="密码" hint="支持 ${secret.xxx}">
          <input
            v-model="transport.mqtt_password"
            type="password"
            autocomplete="off"
            class="field readout"
          />
        </FormField>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <FormField label="QoS" hint="0 最多一次、1 至少一次、2 恰好一次">
          <select v-model.number="transport.mqtt_qos" class="field">
            <option :value="0">0</option>
            <option :value="1">1</option>
            <option :value="2">2</option>
          </select>
        </FormField>
        <FormField v-if="!mqtt_subscribe" label="保留消息" hint="Broker 会为新订阅者补发最后一条">
          <label class="flex items-center gap-2 text-base text-ink-body">
            <input v-model="transport.mqtt_retain" type="checkbox" class="field-check" />
            {{ transport.mqtt_retain ? '保留最后一条消息' : '不保留' }}
          </label>
        </FormField>
      </div>

      <template v-if="!mqtt_subscribe">
        <FormField label="载荷编码">
          <ChoicePicker v-model="transport.mqtt_encoding" :options="ENCODING_CHOICES" />
        </FormField>

        <div class="grid gap-3 sm:grid-cols-[1fr_9rem]">
          <FormField label="消息载荷" hint="支持 ${params.xxx} 模板变量">
            <textarea
              v-model="transport.mqtt_payload"
              class="field field-area field-code min-h-[4.5rem]"
              spellcheck="false"
            ></textarea>
          </FormField>
          <FormField label="超时（ms）" hint="等待建连的上限，0 用服务端默认值">
            <input v-model.number="transport.mqtt_timeout_ms" type="number" min="0" class="field readout" />
          </FormField>
        </div>
      </template>

      <p v-else class="field-hint">
        订阅模式下不需要填写载荷。取值由 Broker 推送的消息驱动，卡片会在收到第一条消息前显示「等待推送」。
      </p>
    </template>

    <!-- Exec -->
    <template v-else>
      <FormField
        label="可执行文件"
        required
        :error="errors['transport.exec_executable']"
        hint="绝对路径，受高危命令黑名单拦截"
      >
        <input
          v-model="transport.exec_executable"
          class="field readout"
          placeholder="/usr/bin/uptime"
          :class="errors['transport.exec_executable'] ? 'border-rose-500 dark:border-rose-700' : ''"
        />
      </FormField>

      <FormField label="命令参数" hint="支持模板变量">
        <div class="flex flex-col gap-2">
          <div v-for="(_, index) in transport.exec_args" :key="index" class="flex items-center gap-2">
            <input
              v-model="transport.exec_args[index]"
              class="field readout flex-1"
              :class="errors[`transport.exec_args.${index}`] ? 'border-rose-500 dark:border-rose-700' : ''"
            />
            <button type="button" class="btn btn-outline btn-sm" @click="remove_arg(index)">删除</button>
          </div>
          <button type="button" class="btn btn-outline btn-sm self-start" @click="add_arg">+ 添加参数</button>
          <p v-if="errors['transport.exec_args.0']" class="field-error">{{ errors['transport.exec_args.0'] }}</p>
        </div>
      </FormField>

      <FormField label="超时（ms）">
        <input v-model.number="transport.exec_timeout_ms" type="number" min="0" class="field readout" placeholder="0 为默认" />
      </FormField>
    </template>
  </div>
</template>
