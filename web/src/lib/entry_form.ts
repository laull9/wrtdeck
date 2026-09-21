// 注册项表单的数据模型、选项常量与客户端校验
// 校验规则刻意与后端 internal/registry/validate.go 保持一致，让错误在提交前就能定位到字段
import type {
  ConfirmSpec,
  Entry,
  EntryKind,
  ExtractSpec,
  ParamSpec,
  ParamType,
  TransportSpec,
  UISpec,
} from '../types'

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

// 展示草稿
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

// 校验问题，path 用点号指向具体字段，便于把错误定位到输入框
export interface Issue {
  path: string
  message: string
}

// 合法的注册项 ID 与参数名
const ID_PATTERN = /^[a-z0-9][a-z0-9_-]{0,63}$/
const NAME_PATTERN = /^[a-zA-Z_][a-zA-Z0-9_]*$/
// 与后端 template.variable_pattern 一致，用于提前发现未声明的模板引用
const VARIABLE_PATTERN = /\$\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:\|[a-z]+)*\s*\}/g

// 服务端默认轮询间隔与最小间隔，提示文案里会用到
export const DEFAULT_INTERVAL_MS = 5000
export const MIN_INTERVAL_MS = 1000

// 把可能来自数字输入框的值统一成去空白的字符串
export function text_of(value: unknown): string {
  if (value === null || value === undefined) {
    return ''
  }
  return String(value).trim()
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

// 把草稿整理成后端条目，丢掉与当前类型无关的分支和空值
// 字段顺序与后端 registry.Entry 保持一致，JSON 高级页签读起来更顺
export function entry_from_draft(draft: EntryDraft): Entry {
  const group = text_of(draft.group)
  const params = draft.kind === 'action' ? params_from_draft(draft.params) : {}
  return {
    id: text_of(draft.id),
    kind: draft.kind,
    name: text_of(draft.name),
    ...(group ? { group } : {}),
    enabled: draft.enabled,
    ...(Object.keys(params).length > 0 ? { params } : {}),
    transport: transport_from_draft(draft.transport),
    ...(draft.kind === 'source'
      ? {
          schedule: { interval_ms: Number(draft.interval_ms) || DEFAULT_INTERVAL_MS },
          extract: extract_from_draft(draft.extract),
        }
      : {}),
    ui: ui_from_draft(draft),
  }
}

// 草稿传输转换成后端传输，只保留当前类型需要的字段
function transport_from_draft(t: TransportDraft): TransportSpec {
  const spec: TransportSpec = { type: t.type }
  if (t.type === 'http') {
    const headers = headers_from_draft(t.http_headers)
    spec.http = { method: t.http_method || 'GET', url: t.http_url.trim() }
    if (headers) {
      spec.http.headers = headers
    }
    if (t.http_body) {
      spec.http.body = t.http_body
    }
    if (t.http_timeout_ms > 0) {
      spec.http.timeout_ms = t.http_timeout_ms
    }
  } else if (t.type === 'tcp') {
    spec.tcp = {
      address: t.net_address.trim(),
      encoding: t.net_encoding || 'text',
      expect_reply: t.net_expect_reply,
    }
    if (t.net_payload) {
      spec.tcp.payload = t.net_payload
    }
    if (t.net_timeout_ms > 0) {
      spec.tcp.timeout_ms = t.net_timeout_ms
    }
  } else if (t.type === 'udp') {
    spec.udp = {
      address: t.net_address.trim(),
      encoding: t.net_encoding || 'text',
      expect_reply: t.net_expect_reply,
    }
    if (t.net_payload) {
      spec.udp.payload = t.net_payload
    }
    if (t.net_timeout_ms > 0) {
      spec.udp.timeout_ms = t.net_timeout_ms
    }
  } else if (t.type === 'mqtt') {
    spec.mqtt = { broker: t.mqtt_broker.trim(), topic: t.mqtt_topic.trim() }
    if (t.mqtt_client_id) {
      spec.mqtt.client_id = t.mqtt_client_id
    }
    if (t.mqtt_username) {
      spec.mqtt.username = t.mqtt_username
    }
    if (t.mqtt_password) {
      spec.mqtt.password = t.mqtt_password
    }
    if (t.mqtt_qos > 0) {
      spec.mqtt.qos = t.mqtt_qos
    }
    if (t.mqtt_retain) {
      spec.mqtt.retain = true
    }
    if (t.mqtt_payload) {
      spec.mqtt.payload = t.mqtt_payload
    }
  } else if (t.type === 'exec') {
    spec.exec = { executable: t.exec_executable.trim() }
    const args = t.exec_args.map((item) => item.trim()).filter((item) => item !== '')
    if (args.length > 0) {
      spec.exec.args = args
    }
    if (t.exec_timeout_ms > 0) {
      spec.exec.timeout_ms = t.exec_timeout_ms
    }
  }
  return spec
}

// 请求头去空行后转成对象，全部为空时返回 null
function headers_from_draft(rows: HeaderDraft[]): Record<string, string> | null {
  const out: Record<string, string> = {}
  for (const row of rows) {
    const key = row.key.trim()
    if (key) {
      out[key] = row.value
    }
  }
  return Object.keys(out).length > 0 ? out : null
}

// 草稿取值转换成后端取值配置
function extract_from_draft(x: ExtractDraft): ExtractSpec {
  const spec: ExtractSpec = { type: x.type || 'raw' }
  if (x.type === 'json' && x.path.trim()) {
    spec.path = x.path.trim()
  }
  if (x.type === 'regex' && x.pattern) {
    spec.pattern = x.pattern
    if (x.group > 0) {
      spec.group = x.group
    }
  }
  if (x.value_type) {
    spec.value_type = x.value_type
  }
  return spec
}

// 草稿展示配置转换成后端展示配置
function ui_from_draft(draft: EntryDraft): UISpec {
  const ui: UISpec = { type: draft.ui.type }
  if (draft.kind === 'source') {
    if (draft.ui.unit) {
      ui.unit = draft.ui.unit
    }
    if (draft.ui.precision > 0) {
      ui.precision = draft.ui.precision
    }
  } else if (draft.ui.variant) {
    ui.variant = draft.ui.variant
  }
  if (draft.ui.ui_label) {
    ui.label = draft.ui.ui_label
  }
  if (draft.ui.order !== 0) {
    ui.order = draft.ui.order
  }
  if (draft.kind === 'action' && draft.ui.confirm_enabled) {
    const confirm: ConfirmSpec = { enabled: true }
    if (draft.ui.confirm_title) {
      confirm.title = draft.ui.confirm_title
    }
    if (draft.ui.confirm_message) {
      confirm.message = draft.ui.confirm_message
    }
    ui.confirm = confirm
  }
  return ui
}

// 草稿参数列表转换成后端参数声明，同名参数后者覆盖前者
function params_from_draft(rows: ParamDraft[]): Record<string, ParamSpec> {
  const out: Record<string, ParamSpec> = {}
  for (const row of rows) {
    const name = text_of(row.name)
    if (!name) {
      continue
    }
    const spec: ParamSpec = { type: row.type }
    if (row.label) {
      spec.label = row.label
    }
    if (row.required) {
      spec.required = true
    }
    const fallback = default_from_draft(row)
    if (fallback !== undefined) {
      spec.default = fallback
    }
    if (row.type === 'number') {
      const min = parse_optional_number(row.min_text)
      const max = parse_optional_number(row.max_text)
      if (min !== null) {
        spec.min = min
      }
      if (max !== null) {
        spec.max = max
      }
    }
    if (row.type === 'select') {
      spec.options = split_options(text_of(row.options_text))
    }
    out[name] = spec
  }
  return out
}

// 按参数类型把默认值文本转回真实类型
function default_from_draft(row: ParamDraft): unknown {
  const text = text_of(row.default_text)
  if (text === '') {
    return undefined
  }
  if (row.type === 'number') {
    const num = Number(text)
    return Number.isFinite(num) ? num : undefined
  }
  if (row.type === 'boolean') {
    return text === 'true' || text === '1'
  }
  return text
}

// 解析可选数字，留空或非法时返回 null
function parse_optional_number(value: unknown): number | null {
  const text = text_of(value)
  if (text === '') {
    return null
  }
  const num = Number(text)
  return Number.isFinite(num) ? num : null
}

// 把逗号或换行分隔的选项文本切成数组
export function split_options(text: string): string[] {
  return text
    .split(/[,，\n]/)
    .map((item) => item.trim())
    .filter((item) => item !== '')
}

// 校验草稿，返回全部问题；空数组表示可以提交
export function validate_draft(draft: EntryDraft): Issue[] {
  const issues: Issue[] = []
  const add = (path: string, message: string): void => {
    issues.push({ path, message })
  }

  const id = draft.id.trim()
  if (!id) {
    add('id', 'ID 不能为空')
  } else if (!ID_PATTERN.test(id)) {
    add('id', '只允许小写字母、数字、- 和 _，且以字母或数字开头，最长 64 位')
  }
  if (!draft.name.trim()) {
    add('name', '名称不能为空')
  }
  validate_transport(draft.transport, add)
  validate_params(draft.params, add)
  if (draft.kind === 'source') {
    if (!(Number(draft.interval_ms) > 0)) {
      add('interval_ms', '轮询间隔必须大于 0 毫秒')
    }
    validate_extract(draft.extract, add)
  }
  check_template_refs(draft, add)
  return issues
}

// 检查传输配置是否填齐了当前类型必填的字段
function validate_transport(t: TransportDraft, add: (path: string, message: string) => void): void {
  if (t.type === 'http' && !t.http_url.trim()) {
    add('transport.http_url', '接口地址不能为空')
  }
  if ((t.type === 'tcp' || t.type === 'udp') && !t.net_address.trim()) {
    add('transport.net_address', '目标地址不能为空，形如 192.168.1.20:9000')
  }
  if (t.type === 'mqtt') {
    if (!t.mqtt_broker.trim()) {
      add('transport.mqtt_broker', 'Broker 地址不能为空')
    }
    if (!t.mqtt_topic.trim()) {
      add('transport.mqtt_topic', '主题不能为空')
    }
  }
  if (t.type === 'exec' && !t.exec_executable.trim()) {
    add('transport.exec_executable', '可执行文件路径不能为空')
  }
}

// 检查每个参数的声明是否自洽
function validate_params(rows: ParamDraft[], add: (path: string, message: string) => void): void {
  const seen = new Set<string>()
  rows.forEach((row, index) => {
    const at = `params.${index}`
    const name = text_of(row.name)
    if (!name) {
      add(`${at}.name`, '参数名不能为空')
    } else if (!NAME_PATTERN.test(name)) {
      add(`${at}.name`, '只能是字母、数字和下划线，且不能以数字开头')
    } else if (seen.has(name)) {
      add(`${at}.name`, `参数名 ${name} 重复`)
    } else {
      seen.add(name)
    }

    if (row.type === 'select' && split_options(text_of(row.options_text)).length === 0) {
      add(`${at}.options_text`, '下拉参数至少要有一个选项')
    }
    if (row.type === 'number') {
      const has_min = text_of(row.min_text) !== ''
      const has_max = text_of(row.max_text) !== ''
      const min = parse_optional_number(row.min_text)
      const max = parse_optional_number(row.max_text)
      if (has_min && min === null) {
        add(`${at}.min_text`, '最小值不是合法数字')
      }
      if (has_max && max === null) {
        add(`${at}.max_text`, '最大值不是合法数字')
      }
      if (min !== null && max !== null && min > max) {
        add(`${at}.min_text`, '最小值不能大于最大值')
      }
    }
  })
}

// 检查取值配置是否填齐
function validate_extract(x: ExtractDraft, add: (path: string, message: string) => void): void {
  if (x.type === 'json' && !x.path.trim()) {
    add('extract.path', '选择 JSON 路径取值时必须填写路径')
  }
  if (x.type === 'regex') {
    if (!x.pattern) {
      add('extract.pattern', '请填写正则表达式')
    } else {
      try {
        new RegExp(x.pattern)
      } catch {
        add('extract.pattern', '不是合法的正则表达式')
      }
    }
  }
}

// 检查模板引用是否都已声明，避免到了运行期才发现变量名写错
function check_template_refs(draft: EntryDraft, add: (path: string, message: string) => void): void {
  const declared = new Set(draft.params.map((row) => `params.${text_of(row.name)}`))
  for (const field of template_fields(draft.transport)) {
    VARIABLE_PATTERN.lastIndex = 0
    for (const match of field.text.matchAll(VARIABLE_PATTERN)) {
      const key = `${match[1]}.${match[2]}`
      if (match[1] === 'secret' || declared.has(key)) {
        continue
      }
      add(field.path, `引用了未声明的变量 \${${key}}`)
      break
    }
  }
}

// 列出草稿里所有支持模板的字段及其表单路径，顺序与界面一致
function template_fields(t: TransportDraft): { path: string; text: string }[] {
  const out: { path: string; text: string }[] = []
  out.push({ path: 'transport.http_url', text: t.http_url })
  out.push({ path: 'transport.http_body', text: t.http_body })
  t.http_headers.forEach((row, index) => {
    out.push({ path: `transport.http_headers.${index}.key`, text: row.key })
    out.push({ path: `transport.http_headers.${index}.value`, text: row.value })
  })
  out.push({ path: 'transport.net_address', text: t.net_address })
  out.push({ path: 'transport.net_payload', text: t.net_payload })
  out.push({ path: 'transport.mqtt_broker', text: t.mqtt_broker })
  out.push({ path: 'transport.mqtt_topic', text: t.mqtt_topic })
  out.push({ path: 'transport.mqtt_username', text: t.mqtt_username })
  out.push({ path: 'transport.mqtt_password', text: t.mqtt_password })
  out.push({ path: 'transport.mqtt_payload', text: t.mqtt_payload })
  out.push({ path: 'transport.exec_executable', text: t.exec_executable })
  t.exec_args.forEach((arg, index) => out.push({ path: `transport.exec_args.${index}`, text: arg }))
  return out
}

// 把问题列表转成 path → message 的映射，方便模板里按字段取错误
export function issue_map(issues: Issue[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const issue of issues) {
    if (!out[issue.path]) {
      out[issue.path] = issue.message
    }
  }
  return out
}
