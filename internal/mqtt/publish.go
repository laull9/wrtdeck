package mqtt

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Publish 发布一条消息，QoS 大于 0 时按协议等待确认
func (c *Client) Publish(ctx context.Context, topic string, payload []byte, qos int, retain bool) error {
	if topic == "" {
		return errors.New("MQTT 主题不能为空")
	}
	if qos < 0 || qos > 2 {
		return fmt.Errorf("不支持的 QoS %d", qos)
	}
	if err := c.AwaitConnection(ctx); err != nil {
		return err
	}

	if qos == 0 {
		return c.send(build_publish(topic, payload, 0, retain, 0, false))
	}

	packet_id, ch, err := c.register()
	if err != nil {
		return err
	}
	defer c.unregister(packet_id)

	if err = c.send(build_publish(topic, payload, qos, retain, packet_id, false)); err != nil {
		return err
	}
	if code, err := c.wait(ctx, ch); err != nil {
		return err
	} else if qos == 1 {
		if code != 0 {
			return fmt.Errorf("QoS1 发布 %s 未确认", topic)
		}
		return nil
	}

	// QoS2 还需要等待 PUBCOMP
	if code, err := c.wait(ctx, ch); err != nil {
		return err
	} else if code != 2 {
		return fmt.Errorf("QoS2 发布 %s 未完成", topic)
	}
	return nil
}

// register 分配一个报文标识符并登记等待应答的通道
func (c *Client) register() (uint16, chan byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return 0, nil, errors.New("MQTT 未连接")
	}
	for i := 0; i < 65535; i++ {
		c.next_id++
		if c.next_id == 0 {
			c.next_id = 1
		}
		if _, busy := c.pending[c.next_id]; !busy {
			// QoS2 需要先后收到两种应答，缓冲区留两个位置
			ch := make(chan byte, 2)
			c.pending[c.next_id] = ch
			return c.next_id, ch, nil
		}
	}
	return 0, nil, errors.New("MQTT 报文标识符已耗尽")
}

// unregister 注销等待应答的通道
func (c *Client) unregister(packet_id uint16) {
	c.mu.Lock()
	delete(c.pending, packet_id)
	c.mu.Unlock()
}

// wait 等待一次应答；连接断开时 link 通道会关闭，立即返回错误而不必等满超时
func (c *Client) wait(ctx context.Context, ch chan byte) (byte, error) {
	c.mu.Lock()
	link := c.link
	c.mu.Unlock()
	if link == nil {
		return 0, errors.New("MQTT 连接在等待应答时断开")
	}

	timer := time.NewTimer(ack_timeout)
	defer timer.Stop()
	select {
	case code := <-ch:
		return code, nil
	case <-link:
		return 0, errors.New("MQTT 连接在等待应答时断开")
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
		return 0, errors.New("等待 MQTT 应答超时")
	}
}

// push 向等待通道投放结果，通道写满或已无人接收时直接丢弃
func push(ch chan byte, code byte) {
	select {
	case ch <- code:
	default:
	}
}
