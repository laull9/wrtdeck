// 注册项的客户端校验
// 规则刻意与后端 internal/registry/validate.go 保持一致，让错误在提交前就能定位到字段
import {
  parse_optional_number,
  split_options,
  text_of,
  type EntryDraft,
  type ExtractDraft,
  type ParamDraft,
  type TransportDraft,
} from './entry_model'

// 校验问题，path 用点号指向具体字段，便于把错误定位到输入框
export interface Issue {
  path: string
  message: string
}

// 合法的注册项 ID 与参数名
const ID_PATTERN = /^[a-z0-9][a-z0-9_-]{0,63}$/
const NAME_PATTERN = /^[a-zA-Z_][a-zA-Z0-9_]*$/

// 与后端 template.variable_pattern 一致的模板变量引用
// 每次返回新实例，避免多个全局正则共享 lastIndex 造成隐性依赖
function variable_pattern(): RegExp {
  return /\$\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:\|[a-z]+)*\s*\}/g
}

// 校验草稿，返回全部问题；空数组表示可以提交
export function validate_draft(draft: EntryDraft): Issue[] {
  const issues: Issue[] = []
  const add = (path: string, message: string): void => {
    issues.push({ path, message })
  }

  const id = text_of(draft.id)
  if (!id) {
    add('id', 'ID 不能为空')
  } else if (!ID_PATTERN.test(id)) {
    add('id', '只允许小写字母、数字、- 和 _，且以字母或数字开头，最长 64 位')
  }
  if (!text_of(draft.name)) {
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
  if (t.type === 'http' && !text_of(t.http_url)) {
    add('transport.http_url', '接口地址不能为空')
  }
  if ((t.type === 'tcp' || t.type === 'udp') && !text_of(t.net_address)) {
    add('transport.net_address', '目标地址不能为空，形如 192.168.1.20:9000')
  }
  if (t.type === 'mqtt') {
    if (!text_of(t.mqtt_broker)) {
      add('transport.mqtt_broker', 'Broker 地址不能为空')
    }
    if (!text_of(t.mqtt_topic)) {
      add('transport.mqtt_topic', '主题不能为空')
    }
  }
  if (t.type === 'exec' && !text_of(t.exec_executable)) {
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
  if (x.type === 'json' && !text_of(x.path)) {
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
  const pattern = variable_pattern()
  for (const field of template_fields(draft.transport)) {
    for (const match of field.text.matchAll(pattern)) {
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
