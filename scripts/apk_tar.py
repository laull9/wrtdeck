"""apk v2 包内的 tar 段与 gzip 流：写入与切分。

apk v2 的包是「段」的拼接：可选签名段 + 控制段 + 数据段，每一段各自独立 gzip。
段是**缺少两个全零结束块**的 tar，这正是 abuild 里 abuild-tar --cut 做的事：
apk 把整个包当成一条连续的 tar 流来读，控制段若带上结束块，
读取器会在那里认为归档已结束，后面的数据段统统被当成填充丢掉。

数据段则相反，它保留结束块，并且每个文件带一条 APK-TOOLS.checksum.SHA1 的
PAX 扩展头（abuild-tar 加的），apk 装包时把它记进已安装数据库。

gzip 流本身不记录成员长度，切分只能边解压边定位：decompressobj 解完一个成员后
把剩余字节放进 unused_data，末尾 8 字节是 CRC32 与原始长度，跳过即可。
"""

import gzip
import hashlib
import io
import struct
import zlib

# tar 的记录单位
RECORD = 512

# 数据段中每一条文件校验和用的 PAX 关键字，取值必须与 apk-tools 一致
CHECKSUM_KEY = 'APK-TOOLS.checksum.SHA1'


# ── PAX 扩展头 ────────────────────────────────────────────────────────────

# 生成一条 PAX 记录，长度前缀包含记录自身的长度（迭代到稳定为止）
def pax_record(key, value):
    payload = '%s=%s\n' % (key, value)
    length = len(payload) + 3
    while length != len(str(length)) + 1 + len(payload):
        length = len(str(length)) + 1 + len(payload)
    return ('%d %s' % (length, payload)).encode('utf-8')


# 把若干 PAX 记录拼成一段
def pax_payload(records):
    return b''.join(pax_record(key, value) for key, value in records.items())


# ── ustar 头 ──────────────────────────────────────────────────────────────

# 按定长字段写入字符串，超长即报错：本项目的路径都在 100 字节内，不需要 GNU 长名扩展
def _field(value, length):
    raw = value.encode('utf-8') if isinstance(value, str) else value
    if len(raw) > length:
        raise ValueError('tar 字段超长: %r（上限 %d 字节）' % (value, length))
    return raw.ljust(length, b'\x00')


# 按八进制写入数值字段，末位留 NUL
def _octal(value, length):
    return _field('%0*o' % (length - 1, value), length)


# 构造一条 tar 记录头，checksum 域先按空格累加再回填
def _header(name, mode, size, mtime, typeflag, linkname=''):
    header = (
        _field(name, 100)
        + _octal(mode & 0o7777, 8)
        + _octal(0, 8)          # uid 固定 0
        + _octal(0, 8)          # gid 固定 0
        + _octal(size, 12)
        + _octal(int(mtime), 12)
        + b' ' * 8              # checksum 占位
        + typeflag
        + _field(linkname, 100)
        + b'ustar\x0000'
        + _field('root', 32)
        + _field('root', 32)
        + _octal(0, 8)
        + _octal(0, 8)
        + _field('', 155)
        + b'\x00' * 12
    )
    if len(header) != RECORD:
        raise ValueError('tar 头长度错误: %d' % len(header))
    return header[:148] + ('%06o\x00 ' % sum(header)).encode('ascii') + header[156:]


# 把内容补齐到 512 字节的整数倍
def _pad(raw):
    remainder = len(raw) % RECORD
    return raw if remainder == 0 else raw + b'\x00' * (RECORD - remainder)


# ── 段 ────────────────────────────────────────────────────────────────────

# Segment 按写入顺序累积 tar 条目，最后产出不含结束块的一段 tar
class Segment:
    # 初始化
    def __init__(self, mtime=0):
        self.mtime = int(mtime)
        self.blocks = []

    # 追加一个普通文件；checksum 为真时附带 apk 要求的 SHA1 校验和扩展头
    def add_file(self, name, data, mode=0o644, checksum=False):
        records = {}
        if checksum:
            records[CHECKSUM_KEY] = hashlib.sha1(data).hexdigest()
        self._add(name, mode, len(data), b'0', records)
        self.blocks.append(_pad(data))
        return self

    # 追加一个目录
    def add_dir(self, name, mode=0o755):
        self._add(name.rstrip('/') + '/', mode, 0, b'5', {})
        return self

    # 追加一个符号链接
    def add_symlink(self, name, target, mode=0o777):
        self._add(name, mode, 0, b'2', {}, target)
        return self

    # 追加一条记录，需要时先写 PAX 扩展头
    def _add(self, name, mode, size, typeflag, records, linkname=''):
        if records:
            payload = pax_payload(records)
            self.blocks.append(
                _header(('PaxHeaders/' + name)[:100], 0o644, len(payload), self.mtime, b'x')
                + _pad(payload)
            )
        self.blocks.append(_header(name, mode, size, self.mtime, typeflag, linkname))

    # 产出段字节；这里刻意不写两个全零结束块，理由见模块顶部说明
    def to_bytes(self):
        return b''.join(self.blocks)


# 给段补上两个全零结束块，得到一份独立的 tar 归档（只有数据段需要）
def with_trailer(segment_bytes):
    return segment_bytes + b'\x00' * (2 * RECORD)


# ── gzip ──────────────────────────────────────────────────────────────────

# 用固定头（不写文件名与时间）压缩，保证同样的内容得到同样的字节
def gzip_bytes(raw, level=9):
    buffer = io.BytesIO()
    with gzip.GzipFile(filename='', mode='wb', compresslevel=level, fileobj=buffer, mtime=0) as handle:
        handle.write(raw)
    return buffer.getvalue()


# ── 切分 ──────────────────────────────────────────────────────────────────

# 计算一个 gzip 成员的头部长度
def gzip_header_length(data, offset):
    if data[offset : offset + 3] != b'\x1f\x8b\x08':
        raise ValueError('第 %d 字节处不是 gzip 成员头' % offset)
    flags = data[offset + 3]
    length = 10
    if flags & 0x04:  # FEXTRA：先来一个 2 字节长度
        extra = struct.unpack('<H', data[offset + 10 : offset + 12])[0]
        length += 2 + extra
    for flag in (0x08, 0x10):  # FNAME / FCOMMENT：以 NUL 结尾的变长字符串
        if flags & flag:
            end = data.index(b'\x00', offset + length)
            length += (end - offset - length) + 1
    if flags & 0x02:  # FHCRC
        length += 2
    return length


# 按 gzip 成员切分，返回每个成员的压缩字节、解压内容与起始偏移
def split_member_streams(data):
    streams = []
    offset = 0
    while offset < len(data):
        # 段与段之间可能夹着零填充，跳过
        while offset < len(data) and data[offset] == 0x00:
            offset += 1
        if offset >= len(data):
            break
        start = offset + gzip_header_length(data, offset)
        decompressor = zlib.decompressobj(-zlib.MAX_WBITS)
        body = decompressor.decompress(data[start:])
        if not decompressor.eof:
            raise ValueError('第 %d 字节处的 gzip 成员没有正常结束' % offset)
        trailer = decompressor.unused_data
        if len(trailer) < 8:
            raise ValueError('第 %d 字节处的 gzip 成员缺少尾部' % offset)
        crc, size = struct.unpack('<II', trailer[:8])
        if crc != (zlib.crc32(body) & 0xFFFFFFFF):
            raise ValueError('第 %d 字节处的 gzip 成员 CRC 校验失败' % offset)
        if size != (len(body) & 0xFFFFFFFF):
            raise ValueError('第 %d 字节处的 gzip 成员长度不符' % offset)
        end = len(data) - len(trailer) + 8
        streams.append({'offset': offset, 'compressed': data[offset:end], 'body': body})
        offset = end
    if not streams:
        raise ValueError('文件里没有任何 gzip 成员')
    return streams


# 按 gzip 成员切分，只返回每段解压后的原始字节
def split_members(data):
    return [stream['body'] for stream in split_member_streams(data)]


# ── 轻量 tar 读取（校验脚本用，按记录逐条读，不猜偏移） ────────────────────

# 读出一条记录头，返回字段字典；全零块返回 None 表示归档结束
def read_header(raw, offset):
    block = raw[offset : offset + RECORD]
    if len(block) < RECORD or block == b'\x00' * RECORD:
        return None
    text = block[0:100].split(b'\x00')[0].decode('utf-8', 'replace')
    prefix = block[345:500].split(b'\x00')[0].decode('utf-8', 'replace')
    if prefix:
        text = prefix + '/' + text
    return {
        'name': text,
        'mode': int(block[100:108].split(b'\x00')[0].strip() or b'0', 8),
        'uid': int(block[108:116].split(b'\x00')[0].strip() or b'0', 8),
        'gid': int(block[116:124].split(b'\x00')[0].strip() or b'0', 8),
        'size': int(block[124:136].split(b'\x00')[0].strip() or b'0', 8),
        'mtime': int(block[136:148].split(b'\x00')[0].strip() or b'0', 8),
        'type': block[156:157].decode('ascii', 'replace'),
        'linkname': block[157:257].split(b'\x00')[0].decode('utf-8', 'replace'),
        'uname': block[265:297].split(b'\x00')[0].decode('utf-8', 'replace'),
        'gname': block[297:329].split(b'\x00')[0].decode('utf-8', 'replace'),
    }


# 逐条读出段内的 tar 条目，PAX 扩展头会被并进紧随其后的那一条
def read_entries(raw):
    entries = []
    pending = {}
    offset = 0
    while True:
        header = read_header(raw, offset)
        if header is None:
            break
        offset += RECORD
        body = raw[offset : offset + header['size']]
        offset += (header['size'] + RECORD - 1) // RECORD * RECORD
        if header['type'] == 'x':
            for line in body.split(b'\n'):
                if not line:
                    continue
                _, _, record = line.partition(b' ')
                key, _, value = record.partition(b'=')
                pending[key.decode('utf-8')] = value.decode('utf-8')
            continue
        header['pax'] = pending
        header['data'] = body
        pending = {}
        entries.append(header)
    return entries


# 从条目里取出 .PKGINFO 的字段表，重复字段（depend 等）合并成列表
def pkginfo_fields(entries):
    for entry in entries:
        if entry['name'] == '.PKGINFO':
            fields = {}
            for line in entry['data'].decode('utf-8').split('\n'):
                line = line.strip()
                if not line or line.startswith('#'):
                    continue
                key, _, value = line.partition('=')
                fields.setdefault(key.strip(), []).append(value.strip())
            return fields
    return {}
