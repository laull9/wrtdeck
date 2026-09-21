// 草稿与后端条目之间的转换：保存前把表单整理成后端能直接收下的 JSON
import type { ConfirmSpec, Entry, ExtractSpec, ParamSpec, TransportSpec, UISpec } from '../types'
import {
  DEFAULT_INTERVAL_MS,
  parse_optional_number,
  split_options,
  text_of,
  type EntryDraft,
  type ExtractDraft,
  type HeaderDraft,
  type ParamDraft,
  type TransportDraft,
} from './entry_model'

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
  } else if (t.type === 'tcp' || t.type === 'udp') {
    const net = {
      address: t.net_address.trim(),
      encoding: t.net_encoding || 'text',
      expect_reply: t.net_expect_reply,
      ...(t.net_payload ? { payload: t.net_payload } : {}),
      ...(t.net_timeout_ms > 0 ? { timeout_ms: t.net_timeout_ms } : {}),
    }
    if (t.type === 'tcp') {
      spec.tcp = net
    } else {
      spec.udp = net
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
