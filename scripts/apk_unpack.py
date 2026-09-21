"""切分 apk v2 包，把签名段 / 控制段 / 数据段解出来，并写出可断言的摘要 JSON。

开发机上没有 apk-tools，校验与安装模拟都靠这个脚本还原包内的真实结构：
它按 gzip 成员与 tar 记录逐字节解析，不依赖系统 tar 的容错，
因此「段顺序错了」「控制段多写了结束块」这类问题都会在这里暴露。

用法：
  python3 scripts/apk_unpack.py --in dist/wrtdeck-1.0.0-r1.apk --out /tmp/apk
产出：
  /tmp/apk/rootfs/...      数据段解开后的文件树
  /tmp/apk/control/...     控制段条目（.PKGINFO 与安装脚本）
  /tmp/apk/signature/...   签名段条目（未签名时为空）
  /tmp/apk/info.json       结构摘要，供 shell 校验脚本断言
"""

import argparse
import hashlib
import json
import os
import sys

import apk_tar

# 段类型
SIGNATURE = 'signature'
CONTROL = 'control'
DATA = 'data'


# 构造命令行参数
def parse_args(argv):
    parser = argparse.ArgumentParser(description='切分 apk v2 包')
    parser.add_argument('--in', dest='source', required=True, help='输入的 .apk 路径')
    parser.add_argument('--out', required=True, help='输出目录')
    parser.add_argument('--quiet', action='store_true', help='只写文件，不打印摘要')
    return parser.parse_args(argv)


# 判断一段是否是签名段：签名段里只有 .SIGN.* 条目
def is_signature_segment(entries):
    return bool(entries) and all(entry['name'].startswith('.SIGN.') for entry in entries)


# 把一段的条目落到目录里，权限与符号链接按包内描述还原
def extract_entries(entries, target):
    for entry in entries:
        path = os.path.join(target, entry['name'].lstrip('./'))
        if entry['type'] == '5':
            os.makedirs(path, exist_ok=True)
            continue
        os.makedirs(os.path.dirname(path), exist_ok=True)
        if entry['type'] == '2':
            if os.path.lexists(path):
                os.remove(path)
            os.symlink(entry['linkname'], path)
            continue
        with open(path, 'wb') as handle:
            handle.write(entry['data'])
        os.chmod(path, entry['mode'])


# 汇总一段的描述信息，含每条文件的校验和
def describe(entries):
    return [{
        'name': entry['name'],
        'type': entry['type'],
        'mode': '%04o' % entry['mode'],
        'size': entry['size'],
        'uname': entry['uname'],
        'gname': entry['gname'],
        'sha1': hashlib.sha1(entry['data']).hexdigest() if entry['type'] == '0' else '',
        'apk_checksum': entry['pax'].get(apk_tar.CHECKSUM_KEY, ''),
    } for entry in entries]


# 主流程
def main(argv):
    args = parse_args(argv)
    with open(args.source, 'rb') as handle:
        raw = handle.read()

    streams = apk_tar.split_member_streams(raw)
    parsed = []
    for index, stream in enumerate(streams):
        entries = apk_tar.read_entries(stream['body'])
        parsed.append({'index': index, 'stream': stream, 'entries': entries})

    # 段的身份：签名段只含 .SIGN.*；含 .PKGINFO 的是控制段；其余最后一个为数据段
    kinds = []
    for item in parsed:
        if is_signature_segment(item['entries']):
            kinds.append(SIGNATURE)
        elif any(entry['name'] == '.PKGINFO' for entry in item['entries']):
            kinds.append(CONTROL)
        else:
            kinds.append(DATA)
    if kinds[-1] != DATA or CONTROL not in kinds:
        raise SystemExit('包结构异常，段类型依次为 %r' % kinds)

    signature_entries = []
    control_entries = []
    data_entries = []
    data_hash = ''
    for kind, item in zip(kinds, parsed):
        if kind == SIGNATURE:
            signature_entries.extend(item['entries'])
        elif kind == CONTROL:
            control_entries = item['entries']
        else:
            data_entries.extend(item['entries'])
            data_hash = hashlib.sha256(item['stream']['compressed']).hexdigest()

    for name, entries in (('signature', signature_entries), ('control', control_entries), ('data', data_entries)):
        target = os.path.join(args.out, name if name != 'data' else 'rootfs')
        os.makedirs(target, exist_ok=True)
        extract_entries(entries, target)

    fields = apk_tar.pkginfo_fields(control_entries)
    computed = {}
    for entry in data_entries:
        if entry['type'] != '0':
            continue
        digest = hashlib.sha1(entry['data']).hexdigest()
        computed[entry['name']] = (digest, entry['pax'].get(apk_tar.CHECKSUM_KEY, ''))

    info = {
        'file': os.path.abspath(args.source),
        'file_size': len(raw),
        'member_count': len(streams),
        'member_sizes': [len(stream['compressed']) for stream in streams],
        'kinds': kinds,
        'signed': SIGNATURE in kinds,
        'signature_names': [entry['name'] for entry in signature_entries],
        'signature_size': sum(entry['size'] for entry in signature_entries),
        'control': describe(control_entries),
        'data': describe(data_entries),
        'pkginfo': fields,
        'data_hash': data_hash,
        'installed_size': sum(entry['size'] for entry in data_entries if entry['type'] == '0'),
        'checksum_match': all(
            digest and digest == pax for digest, pax in computed.values()),
        'checksum_missing': sorted(
            name for name, (_, pax) in computed.items() if not pax),
    }
    with open(os.path.join(args.out, 'info.json'), 'w') as handle:
        json.dump(info, handle, ensure_ascii=False, indent=2, sort_keys=True)

    if not args.quiet:
        print('包 %s' % info['file'])
        print('段数 %d  类型 %s  已签名 %s' % (info['member_count'], kinds, info['signed']))
        for item in info['control']:
            print('  控制段 %-14s %s %d 字节' % (item['name'], item['mode'], item['size']))
        for item in info['data']:
            print('  数据段 %-14s %s %d 字节' % (item['name'], item['mode'], item['size']))
        print('datahash %s' % info['data_hash'])
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
