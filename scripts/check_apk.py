"""校验 apk 包结构，逐条对应 apk 装包时的检查点。

开发机上装不了 apk 也跑不了设备，因此把「能不能装、装了对不对」拆成可断言的项：
段边界、签名、逐文件校验和、.PKGINFO 字段、权限属主、ELF 属性、init 契约。
apk 的读法是「把整个包当一条连续 tar 流读」，因此段的定位与结束块位置是这里最要紧的断言。

用法：
  python3 scripts/check_apk.py dist/wrtdeck-1.0.0-r1.apk --preset wrtdeck [--pubkey 公钥路径]
  python3 scripts/check_apk.py dist/xxx.apk --advice-counts /tmp/counts
"""

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile

import apk_tar

# 各包的结构预期。新增包时补一条即可，校验逻辑不用改。
PRESETS = {
    'wrtdeck': {
        'name': 'wrtdeck',
        'files': [
            ('usr/bin/wrtdeck', '0755'),
            ('etc/init.d/wrtdeck', '0755'),
            ('etc/wrtdeck/config.json', '0644'),
        ],
        'depends': ['ca-bundle'],
        'scripts': ['post-install', 'post-upgrade', 'pre-deinstall'],
        # 升级路径必须把旧版本留下的配置补齐：config.json 是配置文件，
        # 升级时设备保留的往往是旧内容（apk 默认不覆盖 /etc 下被改过的文件），
        # 少这一步就会继续按旧的 0.0.0.0:8080 启动。
        'script_require': {'post-upgrade': ['migrate_config']},
        'script_identical': [],
        'elf': True,
        'arch_elf': {
            'aarch64': 'ARM aarch64',
            'arm_': 'ARM',
            'x86_64': 'x86-64',
            'mipsel': 'MIPS',
            'mips': 'MIPS',
        },
        'init': 'etc/init.d/wrtdeck',
        'config': 'etc/wrtdeck/config.json',
        'program': '/usr/bin/wrtdeck',
        'conf_dir': '/etc/wrtdeck',
    },
    'luci': {
        'name': 'luci-app-wrtdeck',
        'files': [
            ('usr/share/luci/menu.d/luci-app-wrtdeck.json', '0644'),
            ('usr/share/rpcd/acl.d/luci-app-wrtdeck.json', '0644'),
            ('usr/libexec/rpcd/wrtdeck', '0755'),
            ('www/luci-static/resources/view/wrtdeck/panel.js', '0644'),
            # 同源网关入口必须是可执行文件，否则 Web 服务器不会把它当 CGI 调起
            ('www/cgi-bin/wrtdeck-api', '0755'),
        ],
        'depends': ['luci-base', 'wrtdeck'],
        # apk 升级只跑 post-upgrade、不跑 post-install，两条都必须声明：
        # 漏掉 post-upgrade 的话，从 1.0.0 升上来的设备不会重启 rpcd、
        # 不会补 cgi_prefix 符号链接，LuCI 里点进去是个空白框。
        'scripts': ['post-install', 'post-upgrade'],
        'script_require': {},
        'script_identical': [['post-install', 'post-upgrade']],
        'elf': False,
        'arch_elf': {},
        'init': '',
        'config': '',
        'program': '',
        'conf_dir': '',
    },
}

# 合法的 apk 安装脚本条目名
SCRIPT_ENTRY_NAMES = [
    '.pre-install', '.post-install', '.pre-upgrade', '.post-upgrade',
    '.pre-deinstall', '.post-deinstall', '.trigger',
]

# 必填的 .PKGINFO 字段
REQUIRED_FIELDS = ['pkgname', 'pkgver', 'pkgdesc', 'url', 'builddate', 'packager',
                   'size', 'arch', 'origin', 'license', 'datahash']


# 断言计数器，输出格式与 shell 版校验脚本保持一致
class Report:
    # 初始化
    def __init__(self):
        self.passed = 0
        self.failed = 0

    # 打印小节标题
    def step(self, title):
        print('\n== %s ==' % title)

    # 通过一项
    def ok(self, text):
        print('  [ok]   %s' % text)
        self.passed += 1

    # 失败一项
    def ng(self, text):
        print('  [FAIL] %s' % text)
        self.failed += 1

    # 条件断言
    def expect(self, condition, desc, detail=''):
        if condition:
            self.ok(desc)
        else:
            self.ng('%s%s' % (desc, ('（%s）' % detail) if detail else ''))

    # 相等断言
    def expect_eq(self, actual, want, desc):
        self.expect(actual == want, desc, '实际 %r，期望 %r' % (actual, want))


# 构造命令行参数
def parse_args(argv):
    parser = argparse.ArgumentParser(description='校验 apk 包结构')
    parser.add_argument('package', help='待校验的 .apk 路径')
    parser.add_argument('--preset', default='wrtdeck', choices=sorted(PRESETS), help='结构预期')
    parser.add_argument('--pubkey', default='', help='用于验签的公钥路径，给出则实际验签')
    parser.add_argument('--counts-out', default='', help='把 通过数 失败数 写到这个文件，供外层合并')
    parser.add_argument('--allow-unsigned', action='store_true', help='允许未签名')
    return parser.parse_args(argv)


# 在数据段里找条目
def find_entry(entries, name):
    for entry in entries:
        if entry['name'] == name:
            return entry
    return None


# 汇总一段里所有条目的名称
def entry_names(entries):
    return [entry['name'] for entry in entries]


# 执行外部命令并返回 (返回码, 输出)
def run(command):
    result = subprocess.run(command, capture_output=True)
    return result.returncode, (result.stdout + result.stderr).decode('utf-8', 'replace')


# 小节一：包容器与段结构
def check_segments(report, raw, streams, allow_unsigned):
    report.step('1. gzip 段与 tar 结构')
    report.expect(raw[:2] == b'\x1f\x8b', '文件是 gzip 流（apk 包体格式）')
    kinds = []
    for stream in streams:
        entries = apk_tar.read_entries(stream['body'])
        if entries and all(name.startswith('.SIGN.') for name in entry_names(entries)):
            kinds.append('signature')
        elif any(name == '.PKGINFO' for name in entry_names(entries)):
            kinds.append('control')
        else:
            kinds.append('data')
    signed = kinds[0] == 'signature'
    expected = ['signature', 'control', 'data'] if signed else ['control', 'data']
    report.expect_eq(kinds, expected, '段类型与顺序为 %s' % ' + '.join(expected))
    if not signed:
        report.expect(allow_unsigned, '未签名（安装需要 apk add --allow-untrusted）')

    # 关键断言：apk 把整个包当一条连续 tar 流读，控制段带上结束块会让后面的数据段被当成填充
    for index, stream in enumerate(streams):
        body = stream['body']
        is_data = kinds[index] == 'data'
        has_trailer = body.endswith(b'\x00' * 1024)
        if is_data:
            report.expect(has_trailer, '数据段带两个全零结束块（最后一个段需要它）')
        else:
            report.expect(not has_trailer, '%s段不含结束块（否则数据段会被整段忽略）' % ('签名' if kinds[index] == 'signature' else '控制'))
    return kinds, signed


# 小节二：签名
def check_signature(report, signature_entries, control_gz, args, work_dir):
    report.step('2. 签名段')
    names = entry_names(signature_entries)
    report.expect(len(names) == 2, '同时给出 SHA-1 与 SHA-256 两条签名', '实际 %r' % names)
    pubs = [name.split('.rsa.pub')[0].split('.')[-1] for name in names]
    report.expect(len(set(pubs)) == 1 and bool(pubs), '两条签名指向同一个公钥')
    if args.pubkey and pubs:
        report.expect_eq(pubs[0] + '.rsa.pub', os.path.basename(args.pubkey),
                         '签名条目的公钥名与验签公钥文件名一致')
    for entry in signature_entries:
        report.expect(entry['size'] >= 256, '%s 签名长度合理（%d 字节）' % (entry['name'], entry['size']))
        report.expect(entry['mode'] == 0o644, '%s 权限为 0644' % entry['name'])

    if not args.pubkey:
        return
    if not os.path.exists(args.pubkey):
        report.ng('公钥不存在：%s' % args.pubkey)
        return
    # 签名对象是控制段压缩后的字节，与 apk 的验签方式一致
    control_path = os.path.join(work_dir, 'control.tar.gz')
    with open(control_path, 'wb') as handle:
        handle.write(control_gz)
    for entry in signature_entries:
        hash_name = 'sha256' if entry['name'].startswith('.SIGN.RSA256.') else 'sha1'
        sig_path = os.path.join(work_dir, 'verify.sig')
        with open(sig_path, 'wb') as handle:
            handle.write(entry['data'])
        code, output = run(['openssl', 'dgst', '-' + hash_name,
                            '-verify', args.pubkey, '-signature', sig_path, control_path])
        report.expect(code == 0, '%s 用公钥验签通过' % entry['name'], output.strip()[:120])


# 小节三：控制段与 .PKGINFO
def check_control(report, control_entries, fields, preset, data_hash, installed_size, work_dir):
    report.step('3. 控制段与 .PKGINFO')
    names = entry_names(control_entries)
    report.expect('.PKGINFO' in names, '控制段含 .PKGINFO')
    for name in names:
        if name == '.PKGINFO':
            continue
        report.expect(name in SCRIPT_ENTRY_NAMES, '%s 是 apk 认可的安装脚本名' % name)

    for key in REQUIRED_FIELDS:
        report.expect(bool(fields.get(key)), '含字段 %s = %s' % (key, (fields.get(key) or [''])[0]))

    # 安装脚本：apk 是直接 exec 它们的，少一条就等于少一次启用/重启动作
    for script in preset['scripts']:
        report.expect('.' + script in names, '含安装脚本 .%s' % script)

    report.expect_eq((fields.get('pkgname') or [''])[0], preset['name'], '包名为 %s' % preset['name'])
    report.expect_eq(int((fields.get('size') or ['0'])[0] or '0'), installed_size,
                     'size 字段等于数据段文件字节合计')
    report.expect_eq((fields.get('datahash') or [''])[0], data_hash,
                     'datahash 等于压缩后数据段的 SHA256')
    for depend in preset['depends']:
        report.expect(depend in fields.get('depend', []), '依赖包含 %s' % depend)

    # 安装脚本语法：apk 是直接 exec 这些脚本的，语法错误要到运行时才炸
    contents = {}
    for entry in control_entries:
        if entry['name'] not in SCRIPT_ENTRY_NAMES:
            continue
        name = entry['name'].lstrip('.')
        contents[name] = entry['data']
        path = os.path.join(work_dir, 'hook%s' % entry['name'])
        with open(path, 'wb') as handle:
            handle.write(entry['data'])
        code, output = run(['sh', '-n', path])
        report.expect(code == 0, '%s 语法合法' % entry['name'], output.strip()[:120])
        report.expect(entry['mode'] & 0o111 != 0, '%s 带可执行权限' % entry['name'])

    # 脚本内容约束。声明了却接错文件、或者升级入口落在一个不干活的脚本上，
    # 光看结构是发现不了的——装上去才发现「升级后功能没生效」，最难查。
    for name, needles in (preset.get('script_require') or {}).items():
        for needle in needles:
            report.expect(needle.encode() in contents.get(name, b''),
                          '%s 里出现 %s' % (name, needle))
    for group in (preset.get('script_identical') or []):
        first = contents.get(group[0], b'')
        for other in group[1:]:
            report.expect(first and first == contents.get(other, b''),
                          '%s 与 %s 内容一致（同一个钩子接在多个入口上）' % (group[0], other))


# 小节四：数据段内容
def check_data(report, data_entries, preset, fields, work_dir):
    report.step('4. 数据段内容')
    for name, mode in preset['files']:
        entry = find_entry(data_entries, name)
        if entry is None:
            report.ng('缺少 %s' % name)
            continue
        report.expect_eq('%04o' % entry['mode'], mode, '%s 权限为 %s' % (name, mode))

    bad_owner = []
    for entry in data_entries:
        if entry['uname'] != 'root' or entry['gname'] != 'root':
            bad_owner.append('%s %s:%s' % (entry['name'], entry['uname'], entry['gname']))
    report.expect(not bad_owner, '属主统一为 root:root', '；'.join(bad_owner[:3]))

    missing = []
    wrong = []
    for entry in data_entries:
        if entry['type'] != '0':
            continue
        actual = hashlib.sha1(entry['data']).hexdigest()
        recorded = entry['pax'].get(apk_tar.CHECKSUM_KEY, '')
        if not recorded:
            missing.append(entry['name'])
        elif recorded != actual:
            wrong.append(entry['name'])
    report.expect(not missing, '每个文件都有 APK-TOOLS.checksum.SHA1 扩展头', '缺少：%s' % missing[:3])
    report.expect(not wrong, '记录的 SHA1 与实际内容一致', '不符：%s' % wrong[:3])

    # apk 用 arch 与服务端索引比对，写错会直接报架构不匹配
    arch = (fields.get('arch') or [''])[0]
    report.expect(bool(arch), '声明了 arch 字段')
    if preset['elf']:
        check_elf(report, data_entries, arch, preset, work_dir)


# 小节五：二进制属性
def check_elf(report, data_entries, arch, preset, work_dir):
    entry = find_entry(data_entries, preset['files'][0][0])
    if entry is None:
        report.ng('数据段里没有 %s，无法校验 ELF 属性' % preset['files'][0][0])
        return
    path = os.path.join(work_dir, 'program')
    with open(path, 'wb') as handle:
        handle.write(entry['data'])
    os.chmod(path, 0o755)
    _, info = run(['file', '-b', path])
    report.expect('ELF' in info, '是 ELF 可执行文件', info.strip())
    report.expect('statically linked' in info, '静态链接（精简版 OpenWrt 无 libc 依赖）', info.strip())
    want = ''
    for prefix, elf_name in sorted(preset['arch_elf'].items()):
        if arch.startswith(prefix):
            want = elf_name
            break
    if want:
        report.expect(want in info, 'ELF 目标架构匹配 arch=%s' % arch, info.strip())
    else:
        report.ng('架构 %s 没有对应的 ELF 预期，请补充 arch_elf 表' % arch)


# 小节六：procd init 契约
def check_init(report, data_entries, preset):
    if not preset['init']:
        return
    report.step('6. procd init 脚本')
    entry = find_entry(data_entries, preset['init'])
    if entry is None:
        report.ng('数据段里没有 %s' % preset['init'])
        return
    text = entry['data'].decode('utf-8', 'replace')
    for token in ['USE_PROCD=1', 'procd_open_instance', 'procd_set_param command',
                  'procd_set_param respawn', 'procd_close_instance', 'start_service', 'stop_service']:
        report.expect(token in text, '含 %s' % token)

    # init 脚本用变量拼装路径，把它取出来核对包内是否真有对应文件
    program = ''
    conf_dir = ''
    for line in text.split('\n'):
        if line.startswith('PROG='):
            program = line[len('PROG='):].strip()
        if line.startswith('CONF_DIR='):
            conf_dir = line[len('CONF_DIR='):].strip()
    report.expect_eq(program, preset['program'], '启动命令路径与包内约定一致')
    report.expect_eq(conf_dir, preset['conf_dir'], '配置目录与包内约定一致')
    report.expect(find_entry(data_entries, preset['program'].lstrip('/')) is not None,
                  '启动命令 %s 在包内存在' % preset['program'])


# 小节七：默认配置
def check_config(report, data_entries, preset):
    if not preset['config']:
        return
    report.step('7. 默认配置')
    entry = find_entry(data_entries, preset['config'])
    if entry is None:
        report.ng('缺少 %s' % preset['config'])
        return
    try:
        parsed = json.loads(entry['data'].decode('utf-8'))
        report.ok('%s 是合法 JSON' % preset['config'])
    except ValueError as error:
        report.ng('%s 不是合法 JSON（%s）' % (preset['config'], error))
        return
    report.expect('listen' in parsed, '含 listen 字段')
    report.expect(parsed.get('data_dir') == preset['conf_dir'],
                  'data_dir 指向 %s（与 init 一致）' % preset['conf_dir'])


# 小节八：文件名与版本约定
def check_naming(report, path, preset, fields):
    report.step('8. 文件名与版本')
    base = os.path.basename(path)
    # 版本号里允许出现 -rc1 / -beta2 这类预发布后缀，因此不能简单按第一个 - 切分
    pattern = re.compile(r'^%s-(?P<version>[0-9][A-Za-z0-9._]*(?:-[A-Za-z][A-Za-z0-9._]*)*)-r(?P<release>[0-9]+)\.apk$'
                         % re.escape(preset['name']))
    matched = pattern.match(base)
    report.expect(matched is not None, '文件名符合 name-版本-r发布号.apk 约定', base)
    pkgver = (fields.get('pkgver') or [''])[0]
    if matched:
        report.expect_eq(pkgver, '%s-r%s' % (matched.group('version'), matched.group('release')),
                         'pkgver 与文件名一致')
    else:
        report.expect(bool(pkgver), '含 pkgver = %s' % pkgver)


# 主流程
def main(argv):
    args = parse_args(argv)
    preset = PRESETS[args.preset]
    if not os.path.exists(args.package):
        raise SystemExit('找不到包：%s' % args.package)
    with open(args.package, 'rb') as handle:
        raw = handle.read()

    report = Report()
    print('被测文件: %s' % os.path.abspath(args.package))
    streams = apk_tar.split_member_streams(raw)
    parsed = [apk_tar.read_entries(stream['body']) for stream in streams]

    kinds, signed = check_segments(report, raw, streams, args.allow_unsigned)
    signature_entries = parsed[0] if signed else []
    control_entries = parsed[1] if signed else parsed[0]
    data_entries = parsed[-1]
    fields = apk_tar.pkginfo_fields(control_entries)
    data_hash = hashlib.sha256(streams[-1]['compressed']).hexdigest()
    installed_size = sum(entry['size'] for entry in data_entries if entry['type'] == '0')

    work_dir = os.environ.get('APK_CHECK_WORK', '') or tempfile.mkdtemp(prefix='apkcheck-')
    os.makedirs(work_dir, exist_ok=True)

    if signed:
        check_signature(report, signature_entries, streams[-2]['compressed'], args, work_dir)
    else:
        report.step('2. 签名段')
        report.expect(args.allow_unsigned, '未签名包，跳过敏签断言')

    check_control(report, control_entries, fields, preset, data_hash, installed_size, work_dir)
    check_data(report, data_entries, preset, fields, work_dir)
    check_init(report, data_entries, preset)
    check_config(report, data_entries, preset)
    check_naming(report, args.package, preset, fields)

    print('\n== 结果（结构校验） ==\n  通过 %d 项，失败 %d 项' % (report.passed, report.failed))
    if args.counts_out:
        with open(args.counts_out, 'w') as handle:
            handle.write('%d %d\n' % (report.passed, report.failed))
    return 1 if report.failed else 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
