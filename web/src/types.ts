// 与后端 internal/registry 的 JSON 结构一一对应的类型定义

// 参数类型
export type ParamType = 'string' | 'number' | 'boolean' | 'select' | 'secret'

// 注册项类型
export type EntryKind = 'source' | 'action'

// 参数声明
export interface ParamSpec {
  type: ParamType
  label?: string
  required?: boolean
  default?: unknown
  min?: number
  max?: number
  options?: string[]
}

// HTTP 调用配置
export interface HTTPSpec {
  method?: string
  url: string
  headers?: Record<string, string>
  body?: string
  timeout_ms?: number
}

// TCP 往返配置
export interface TCPSpec {
  address: string
  payload?: string
  encoding?: string
  timeout_ms?: number
  expect_reply?: boolean
}

// UDP 报文配置
export interface UDPSpec {
  address: string
  payload?: string
  encoding?: string
  timeout_ms?: number
  expect_reply?: boolean
}

// MQTT 发布或订阅配置
export interface MQTTSpec {
  broker: string
  client_id?: string
  username?: string
  password?: string
  topic: string
  qos?: number
  retain?: boolean
  payload?: string
}

// 本地命令配置
export interface ExecSpec {
  executable: string
  args?: string[]
  timeout_ms?: number
}

// 传输配置，只有 type 对应的字段会被使用
export interface TransportSpec {
  type: string
  http?: HTTPSpec
  tcp?: TCPSpec
  udp?: UDPSpec
  mqtt?: MQTTSpec
  exec?: ExecSpec
}

// 信息源轮询配置
export interface ScheduleSpec {
  interval_ms: number
}

// 取值配置
export interface ExtractSpec {
  type: string
  path?: string
  pattern?: string
  group?: number
  value_type?: string
}

// 动作二次确认配置
export interface ConfirmSpec {
  enabled?: boolean
  title?: string
  message?: string
}

// 渲染描述
export interface UISpec {
  type: string
  unit?: string
  label?: string
  precision?: number
  variant?: string
  order?: number
  confirm?: ConfirmSpec
}

// 注册项
export interface Entry {
  id: string
  kind: EntryKind
  name: string
  group?: string
  enabled: boolean
  params?: Record<string, ParamSpec>
  transport: TransportSpec
  schedule?: ScheduleSpec
  extract?: ExtractSpec
  ui: UISpec
}

// 信息源运行状态
export interface SourceState {
  id: string
  status: 'unknown' | 'running' | 'ok' | 'error' | 'stale'
  value: unknown
  text?: string
  updated_at: string
  latency_ms: number
  error?: string
  detail?: string
}

// 信息源注册项与状态的组合
export interface SourceItem {
  entry: Entry
  state: SourceState
}

// 服务元信息
export interface ServerInfo {
  version: string
  uptime_s: number
  started_at: string
  dev: boolean
  auth_disabled: boolean
  sources: number
  actions: number
  subscribers: number
}

// Dashboard 首屏数据
export interface DashboardResponse {
  server: ServerInfo
  sources: SourceItem[]
  actions: Entry[]
}

// 动作执行结果
export interface RunResponse {
  id: string
  ok: boolean
  message: string
  latency_ms: number
  preview?: string
}

// SSE hello 帧的载荷，连接建立时下发一次
export interface HelloPayload {
  server: ServerInfo
  at: string
}

// SSE action.finished 帧的载荷
export interface ActionFinishedPayload {
  id: string
  name: string
  ok: boolean
  message: string
}

// SSE registry.changed 帧的载荷
export interface RegistryChangedPayload {
  id: string
  action: string
}
