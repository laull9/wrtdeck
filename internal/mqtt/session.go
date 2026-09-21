package mqtt

import (
	"bufio"
	"context"
	"errors"
	"time"
)

// read_loop 持续读取报文直到连接出错，同时负责保活超时判定
func (c *Client) read_loop(ctx context.Context, reader *bufio.Reader) error {
	stop_ping := make(chan struct{})
	defer close(stop_ping)
	if c.opts.Keepalive > 0 {
		go c.ping_loop(ctx, stop_ping)
	}

	for {
		if c.opts.Keepalive > 0 {
			// 超过 1.5 倍保活周期没有任何报文，判定链路已死
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn == nil {
				return errors.New("连接已被关闭")
			}
			if err := conn.SetReadDeadline(time.Now().Add(c.opts.Keepalive * 3 / 2)); err != nil {
				return err
			}
		}
		pkt, err := read_packet(reader)
		if err != nil {
			return err
		}
		c.handle(pkt)
	}
}

// ping_loop 按半个保活周期发送 PINGREQ
func (c *Client) ping_loop(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(c.opts.Keepalive / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			if err := c.send(wrap(packet_pingreq, 0, nil)); err != nil {
				return
			}
		}
	}
}

// handle 分发一个收到的报文
func (c *Client) handle(pkt *packet) {
	switch pkt.ptype {
	case packet_publish:
		c.handle_publish(pkt)
	case packet_puback:
		c.resolve(pkt.body, 0)
	case packet_pubrec:
		c.handle_pubrec(pkt)
	case packet_pubrel:
		c.handle_pubrel(pkt)
	case packet_pubcomp:
		c.resolve(pkt.body, 2)
	case packet_suback:
		c.handle_suback(pkt)
	case packet_unsuback:
		c.resolve(pkt.body, 0)
	case packet_pingresp:
		// 保活应答不携带信息，收到即代表链路正常
	default:
		c.opts.Logger.Printf("MQTT 收到未处理的报文类型 %d", pkt.ptype)
	}
}

// handle_publish 处理收到的 PUBLISH，按 QoS 完成应答后交给上层
func (c *Client) handle_publish(pkt *packet) {
	dec := &decoder{buf: pkt.body}
	topic, err := dec.read_string()
	if err != nil {
		return
	}
	qos := int((pkt.flags >> 1) & 0x03)
	var packet_id uint16
	if qos > 0 {
		if packet_id, err = dec.read_uint16(); err != nil {
			return
		}
	}
	payload := dec.read_rest()

	switch qos {
	case 1:
		_ = c.send(build_ack(packet_puback, packet_id, 0))
	case 2:
		c.mu.Lock()
		duplicate := c.inbound_qos2[packet_id]
		c.inbound_qos2[packet_id] = true
		c.mu.Unlock()
		_ = c.send(build_ack(packet_pubrec, packet_id, 0))
		if duplicate {
			// 重复投递只重发 PUBREC，不再向上分发
			return
		}
	}

	if c.opts.OnMessage != nil {
		c.opts.OnMessage(topic, payload)
	}
}

// handle_pubrec 收到 PUBREC 后回复 PUBREL，完成 QoS2 发送的第二步
func (c *Client) handle_pubrec(pkt *packet) {
	dec := &decoder{buf: pkt.body}
	packet_id, err := dec.read_uint16()
	if err != nil {
		return
	}
	_ = c.send(build_ack(packet_pubrel, packet_id, 0x02))
	c.deliver(packet_id, 1)
}

// handle_pubrel 收到 PUBREL 后回复 PUBCOMP，完成 QoS2 接收
func (c *Client) handle_pubrel(pkt *packet) {
	dec := &decoder{buf: pkt.body}
	packet_id, err := dec.read_uint16()
	if err != nil {
		return
	}
	c.mu.Lock()
	delete(c.inbound_qos2, packet_id)
	c.mu.Unlock()
	_ = c.send(build_ack(packet_pubcomp, packet_id, 0))
}

// handle_suback 把 SUBACK 的授权 QoS 交给等待中的订阅请求
func (c *Client) handle_suback(pkt *packet) {
	dec := &decoder{buf: pkt.body}
	packet_id, err := dec.read_uint16()
	if err != nil {
		return
	}
	if dec.remaining() == 0 {
		c.resolve(pkt.body, 0)
		return
	}
	code, err := dec.read_byte()
	if err != nil {
		return
	}
	c.deliver(packet_id, code)
}

// resolve 把只带报文标识符的应答交给等待中的调用方，code 由报文类型决定
func (c *Client) resolve(body []byte, code byte) {
	dec := &decoder{buf: body}
	packet_id, err := dec.read_uint16()
	if err != nil {
		return
	}
	c.deliver(packet_id, code)
}

// deliver 按报文标识符找到等待通道并投放结果
func (c *Client) deliver(packet_id uint16, code byte) {
	c.mu.Lock()
	ch := c.pending[packet_id]
	c.mu.Unlock()
	if ch != nil {
		push(ch, code)
	}
}
