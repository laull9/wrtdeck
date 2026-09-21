// 注册项表单的数据模型与选项常量
// 这里的字段刻意与后端 internal/registry 的模型一一对应，新增字段时两边要同步
import type { Entry, EntryKind, ParamType } from '../types'

// 下拉选项，hint 会在界面上解释该选项的实际行为
export interface Choice {
  value: string
  label: string
  hint?: string
}

// 注册项类型
export const KIND_CHOICES: Choice[] = [
  { value: 'source', label: '信息源', hint: '按固定周期采集一个值，在面板上渲染为卡片' },
  { value: 'action', label: '动作', hint: '由人手动触发，在面板上渲染为按钮' },
]

// 传输方式
export const TRANSPORT_CHOICES: Choice[] = [
  { value: 'http', label: 'HTTP', hint: '请求一个 HTTP 接口，最常用' },
  { value: 'tcp', label: 'TCP', hint: '建立 TCP 连接发送载荷，可选等待应答' },
  { value: 'udp', label: 'UDP', hint: '发送一个 UDP 报文，可选等待应答' },
  { value: 'mqtt', label: 'MQTT', hint: '向 Broker 发布消息，需要服务端接入 MQTT 客户端' },
  { value: 'exec', label: '本地命令', hint: '执行本机可执行文件，需要服务端开启 exec 白名单' },
]

// HTTP 方法
export const HTTP_METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']

// 载荷编码
export const ENCODING_CHOICES: Choice[] = [
  { value: 'text', label: '文本', hint: '按 UTF-8 文本发送' },
  { value: 'hex', label: '十六进制', hint: '写成 0102FF 形式的字节序列' },
  { value: 'base64', label: 'Base64', hint: '按 base64 解码后的字节发送' },
]

// 取值方式
export const EXTRACT_CHOICES: Choice[] = [
  { value: 'raw', label: '原始文本', hint: '把响应体整体当作取值' },
  { value: 'json', label: 'JSON 路径', hint: '按 a.b.0.c 路径取字段，数组用下标' },
  { value: 'regex', label: '正则捕获', hint: '用正则从响应体中捕获指定分组' },
]

// 取值类型
export const VALUE_TYPE_CHOICES: Choice[] = [
  { value: '', label: '自动推断' },
  { value: 'string', label: '字符串' },
  { value: 'number', label: '数字' },
  { value: 'boolean', label: '布尔' },
]

// 信息源卡片形态
export const SOURCE_UI_CHOICES: Choice[] = [
  { value: 'metric', label: '数值读数', hint: '大号数字，可配单位与小数位' },
  { value: 'status', label: '状态徽标', hint: '彩色标签，适合 ok / error 这类取值' },
  { value: 'text', label: '文本块', hint: '多行文本，适合版本号、原始报文' },
]

// 动作按钮配色
export const VARIANT_CHOICES: Choice[] = [
  { value: 'primary', label: '主要', hint: '实心高亮按钮' },
  { value: 'danger', label: '危险', hint: '红色按钮，适合不可逆操作' },
  { value: 'ghost', label: '次要', hint: '描边按钮' },
]

// 参数类型
export const PARAM_TYPE_CHOICES: Choice[] = [
  { value: 'string', label: '字符串' },
  { value: 'number', label: '数字' },
  { value: 'boolean', label: '布尔' },
  { value: 'select', label: '下拉选择' },
  { value: 'secret', label: '密钥', hint: '前端输入时掩码显示' },
]

// 服务端默认轮询间隔与最小间隔，提示文案里会用到
export const DEFAULT_INTERVAL_MS = 5000
export const MIN_INTERVAL_MS = 1000

// 运行参数草稿，取值统一用字符串承载，保存时再按类型转换
// 数字输入框的 v-model 会把值转成 number，因此文本字段允许 string | number
export interface ParamDraft {
  name: string
  type: ParamType
  label: string
  required: boolean
  default_text: string | number
  min_text: string | number
  max_text: string | number
  options_text: string
}

// 请求头草稿，用数组保证编辑时行序稳定
export interface HeaderDraft {
  key: string
  value: string
}

// 传输草稿：五种传输的子字段全部保留，切换类型不会丢失已填内容
export interface TransportDraft {
  type: string
  http_method: string
  http_url: string
  http_headers: HeaderDraft[]
  http_body: string
  http_timeout_ms: number
  // TCP 与 UDP 的字段含义一致，共用一组，保存时按 type 写入对应分支
  net_address: string
  net_payload: string
  net_encoding: string
  net_timeout_ms: number
  net_expect_reply: boolean
  mqtt_broker: string
  mqtt_topic: string
  mqtt_client_id: string
  mqtt_username: string
  mqtt_password: string
  mqtt_qos: number
  mqtt_retain: boolean
  mqtt_payload: string
  exec_executable: string
  exec_args: string[]
  exec_timeout_ms: number
}

// 取值草稿
export interface ExtractDraft {
  type: string
  path: string
  pattern: string
  group: number
  value_type: string
}

// 展示草稿，ui_label 后端暂未消费，仅原样透传以免编辑时丢字段
export interface UIDraft {
  type: string
  unit: string
  ui_label: string
  precision: number
  variant: string
  order: number
  confirm_enabled: boolean
  confirm_title: string
  confirm_message: string
}

// 表单草稿，所有可选子对象都补齐，便于直接双向绑定
export interface EntryDraft {
  id: string
  kind: EntryKind
  name: string
  group: string
  enabled: boolean
  params: ParamDraft[]
  transport: TransportDraft
  interval_ms: number
  extract: ExtractDraft
  ui: UIDraft
}

// 把可能来自数字输入框的值统一成去空白的字符串
export function text_of(value: unknown): string {
  if (value === null || value === undefined) {
    return ''
  }
  return String(value).trim()
}

// 把逗号或换行分隔的选项文本切成数组
export function split_options(text: string): string[] {
  return text
    .split(/[,，\n]/)
    .map((item) => item.trim())
    .filter((item) => item !== '')
}

// 解析可选数字，留空或非法时返回 null
export function parse_optional_number(value: unknown): number | null {
  const text = text_of(value)
  if (text === '') {
    return null
  }
  const num = Number(text)
  return Number.isFinite(num) ? num : null
}

// 新建一份空草稿
export function empty_draft(kind: EntryKind = 'source', group = ''): EntryDraft {
  return {
    id: '',
    kind,
    name: '',
    group,
    enabled: true,
    params: [],
    transport: empty_transport(),
    interval_ms: DEFAULT_INTERVAL_MS,
    extract: { type: kind === 'action' ? 'raw' : 'json', path: '', pattern: '', group: 0, value_type: '' },
    ui: {
      type: kind === 'action' ? 'button' : 'metric',
      unit: '',
      ui_label: '',
      precision: 0,
      variant: 'primary',
      order: 100,
      confirm_enabled: false,
      confirm_title: '',
      confirm_message: '',
    },
  }
}

// 新建一份空传输草稿
function empty_transport(): TransportDraft {
  return {
    type: 'http',
    http_method: 'GET',
    http_url: '',
    http_headers: [],
    http_body: '',
    http_timeout_ms: 3000,
    net_address: '',
    net_payload: '',
    net_encoding: 'text',
    net_timeout_ms: 3000,
    net_expect_reply: false,
    mqtt_broker: '',
    mqtt_topic: '',
    mqtt_client_id: '',
    mqtt_username: '',
    mqtt_password: '',
    mqtt_qos: 0,
    mqtt_retain: false,
    mqtt_payload: '',
    exec_executable: '',
    exec_args: [],
    exec_timeout_ms: 5000,
  }
}

// 把后端条目展开成可绑定的草稿，缺失字段补默认值
export function draft_from_entry(entry: Entry): EntryDraft {
  const t = entry.transport ?? { type: 'http' }
  const ui = entry.ui ?? { type: 'metric' }
  return {
    id: entry.id,
    kind: entry.kind,
    name: entry.name,
    group: entry.group ?? '',
    enabled: entry.enabled,
    params: Object.entries(entry.params ?? {}).map(([name, spec]) => ({
      name,
      type: spec.type,
      label: spec.label ?? '',
      required: spec.required ?? false,
      default_text: spec.default === undefined || spec.default === null ? '' : String(spec.default),
      min_text: spec.min === undefined ? '' : String(spec.min),
      max_text: spec.max === undefined ? '' : String(spec.max),
      options_text: (spec.options ?? []).join(', '),
    })),
    transport: {
      type: t.type || 'http',
      http_method: t.http?.method || 'GET',
      http_url: t.http?.url ?? '',
      http_headers: Object.entries(t.http?.headers ?? {}).map(([key, value]) => ({ key, value })),
      http_body: t.http?.body ?? '',
      http_timeout_ms: t.http?.timeout_ms ?? 0,
      net_address: t.tcp?.address ?? t.udp?.address ?? '',
      net_payload: t.tcp?.payload ?? t.udp?.payload ?? '',
      net_encoding: t.tcp?.encoding || t.udp?.encoding || 'text',
      net_timeout_ms: t.tcp?.timeout_ms ?? t.udp?.timeout_ms ?? 0,
      net_expect_reply: t.tcp?.expect_reply ?? t.udp?.expect_reply ?? false,
      mqtt_broker: t.mqtt?.broker ?? '',
      mqtt_topic: t.mqtt?.topic ?? '',
      mqtt_client_id: t.mqtt?.client_id ?? '',
      mqtt_username: t.mqtt?.username ?? '',
      mqtt_password: t.mqtt?.password ?? '',
      mqtt_qos: t.mqtt?.qos ?? 0,
      mqtt_retain: t.mqtt?.retain ?? false,
      mqtt_payload: t.mqtt?.payload ?? '',
      exec_executable: t.exec?.executable ?? '',
      exec_args: [...(t.exec?.args ?? [])],
      exec_timeout_ms: t.exec?.timeout_ms ?? 0,
    },
    interval_ms: entry.schedule?.interval_ms ?? DEFAULT_INTERVAL_MS,
    extract: {
      type: entry.extract?.type || 'raw',
      path: entry.extract?.path ?? '',
      pattern: entry.extract?.pattern ?? '',
      group: entry.extract?.group ?? 0,
      value_type: entry.extract?.value_type ?? '',
    },
    ui: {
      type: ui.type,
      unit: ui.unit ?? '',
      ui_label: ui.label ?? '',
      precision: ui.precision ?? 0,
      variant: ui.variant || 'primary',
      order: ui.order ?? 0,
      confirm_enabled: ui.confirm?.enabled ?? false,
      confirm_title: ui.confirm?.title ?? '',
      confirm_message: ui.confirm?.message ?? '',
    },
  }
}
