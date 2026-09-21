// Package mqtt 是一个只依赖标准库的 MQTT 3.1.1 客户端。
//
// 刻意选择 3.1.1 而不是 5.0：3.1.1 是物联网设备事实上的通用版本，
// ESP32、Tasmota、Zigbee2MQTT 以及绝大多数局域网 broker 都支持它，
// 而 5.0 会把这些设备全部排除在外。协议实现只覆盖本项目真正用到的部分：
// CONNECT / PUBLISH(0,1,2) / SUBSCRIBE / UNSUBSCRIBE / PINGREQ / DISCONNECT。
package mqtt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// 控制报文类型
const (
	packet_connect     = 0x01
	packet_connack     = 0x02
	packet_publish     = 0x03
	packet_puback      = 0x04
	packet_pubrec      = 0x05
	packet_pubrel      = 0x06
	packet_pubcomp     = 0x07
	packet_subscribe   = 0x08
	packet_suback      = 0x09
	packet_unsubscribe = 0x0a
	packet_unsuback    = 0x0b
	packet_pingreq     = 0x0c
	packet_pingresp    = 0x0d
	packet_disconnect  = 0x0e
)

// CONNACK 返回码
const (
	connack_accepted           = 0x00
	connack_bad_protocol       = 0x01
	connack_id_rejected        = 0x02
	connack_server_unavailable = 0x03
	connack_bad_credentials    = 0x04
	connack_not_authorized     = 0x05
)

// protocol_level 是 MQTT 3.1.1 的协议级别
const protocol_level = 0x04

// packet 是一个解码后的控制报文
type packet struct {
	ptype byte
	flags byte
	body  []byte
}

// connack_text 把 CONNACK 返回码翻成可读说明
func connack_text(code byte) string {
	switch code {
	case connack_bad_protocol:
		return "broker 不接受 MQTT 3.1.1 协议版本"
	case connack_id_rejected:
		return "broker 拒绝了该客户端 ID"
	case connack_server_unavailable:
		return "broker 当前不可用"
	case connack_bad_credentials:
		return "用户名或密码错误"
	case connack_not_authorized:
		return "未授权"
	default:
		return fmt.Sprintf("未知返回码 0x%02x", code)
	}
}

// append_remaining_length 把剩余长度编码成 MQTT 的可变字节整数
func append_remaining_length(buf []byte, n int) []byte {
	for {
		digit := byte(n % 128)
		n /= 128
		if n > 0 {
			digit |= 0x80
		}
		buf = append(buf, digit)
		if n == 0 {
			return buf
		}
	}
}

// read_remaining_length 从流中读出可变字节整数
func read_remaining_length(r *bufio.Reader) (int, error) {
	multiplier := 1
	value := 0
	for i := 0; i < 4; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value += int(b&0x7f) * multiplier
		if b&0x80 == 0 {
			return value, nil
		}
		multiplier *= 128
	}
	return 0, errors.New("MQTT 剩余长度字段非法")
}

// read_packet 读取一个完整的控制报文
func read_packet(r *bufio.Reader) (*packet, error) {
	head, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	length, err := read_remaining_length(r)
	if err != nil {
		return nil, err
	}
	body := make([]byte, length)
	if length > 0 {
		if _, err = io.ReadFull(r, body); err != nil {
			return nil, err
		}
	}
	return &packet{ptype: head >> 4, flags: head & 0x0f, body: body}, nil
}

// wrap 给可变部分加上固定头，得到完整报文
func wrap(ptype, flags byte, body []byte) []byte {
	out := []byte{ptype<<4 | flags}
	out = append_remaining_length(out, len(body))
	return append(out, body...)
}

// append_uint16 追加两字节大端整数
func append_uint16(buf []byte, v uint16) []byte {
	return append(buf, byte(v>>8), byte(v))
}

// append_string 追加带两字节长度前缀的 UTF-8 字符串
func append_string(buf []byte, s string) []byte {
	buf = append_uint16(buf, uint16(len(s)))
	return append(buf, s...)
}

// append_binary 追加带两字节长度前缀的字节串
func append_binary(buf []byte, b []byte) []byte {
	buf = append_uint16(buf, uint16(len(b)))
	return append(buf, b...)
}

// build_connect 构造 CONNECT 报文
func build_connect(client_id, username, password string, keepalive uint16, clean_session bool) []byte {
	body := make([]byte, 0, 64+len(client_id)+len(username)+len(password))
	body = append_string(body, "MQTT")
	body = append(body, protocol_level)

	var flags byte
	if clean_session {
		flags |= 0x02
	}
	if username != "" {
		flags |= 0x80
	}
	if password != "" {
		flags |= 0x40
	}
	body = append(body, flags)
	body = append_uint16(body, keepalive)

	body = append_string(body, client_id)
	if username != "" {
		body = append_string(body, username)
	}
	if password != "" {
		body = append_string(body, password)
	}
	return wrap(packet_connect, 0, body)
}

// build_publish 构造 PUBLISH 报文，QoS 大于 0 时携带报文标识符
func build_publish(topic string, payload []byte, qos int, retain bool, packet_id uint16, dup bool) []byte {
	var flags byte
	if retain {
		flags |= 0x01
	}
	flags |= byte(qos&0x03) << 1
	if dup {
		flags |= 0x08
	}

	body := make([]byte, 0, len(topic)+len(payload)+4)
	body = append_string(body, topic)
	if qos > 0 {
		body = append_uint16(body, packet_id)
	}
	body = append(body, payload...)
	return wrap(packet_publish, flags, body)
}

// build_ack 构造只带报文标识符的确认报文
func build_ack(ptype byte, packet_id uint16, flags byte) []byte {
	return wrap(ptype, flags, append_uint16(nil, packet_id))
}

// build_subscribe 构造 SUBSCRIBE 报文，固定头低四位必须是 0b0010
func build_subscribe(packet_id uint16, topic string, qos int) []byte {
	body := append_uint16(nil, packet_id)
	body = append_string(body, topic)
	body = append(body, byte(qos&0x03))
	return wrap(packet_subscribe, 0x02, body)
}

// build_unsubscribe 构造 UNSUBSCRIBE 报文，固定头低四位必须是 0b0010
func build_unsubscribe(packet_id uint16, topic string) []byte {
	body := append_uint16(nil, packet_id)
	body = append_string(body, topic)
	return wrap(packet_unsubscribe, 0x02, body)
}

// decoder 顺序读取报文可变部分
type decoder struct {
	buf []byte
	pos int
}

// remaining 返回尚未读取的字节数
func (d *decoder) remaining() int { return len(d.buf) - d.pos }

// read_byte 读取一个字节
func (d *decoder) read_byte() (byte, error) {
	if d.pos >= len(d.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	v := d.buf[d.pos]
	d.pos++
	return v, nil
}

// read_uint16 读取两字节大端整数
func (d *decoder) read_uint16() (uint16, error) {
	if d.pos+2 > len(d.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	v := uint16(d.buf[d.pos])<<8 | uint16(d.buf[d.pos+1])
	d.pos += 2
	return v, nil
}

// read_string 读取带长度前缀的字符串
func (d *decoder) read_string() (string, error) {
	length, err := d.read_uint16()
	if err != nil {
		return "", err
	}
	if d.pos+int(length) > len(d.buf) {
		return "", io.ErrUnexpectedEOF
	}
	v := string(d.buf[d.pos : d.pos+int(length)])
	d.pos += int(length)
	return v, nil
}

// read_rest 读取剩余全部字节
func (d *decoder) read_rest() []byte {
	v := d.buf[d.pos:]
	d.pos = len(d.buf)
	return v
}
