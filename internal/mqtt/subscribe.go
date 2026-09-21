package mqtt

import (
	"context"
	"fmt"
	"sort"
)

// SetSubscriptions 声明期望的订阅集合，已连接时立即收敛，未连接时等连上后自动下发
func (c *Client) SetSubscriptions(desired map[string]int) error {
	c.mu.Lock()
	c.wanted = make(map[string]int, len(desired))
	for topic, qos := range desired {
		c.wanted[topic] = qos
	}
	c.mu.Unlock()
	return c.reconcile()
}

// WantedTopics 返回当前期望订阅的排序后的主题，便于日志与自检
func (c *Client) WantedTopics() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.wanted))
	for topic := range c.wanted {
		out = append(out, topic)
	}
	sort.Strings(out)
	return out
}

// reconcile 让 broker 上的订阅与期望集合同步，由连接建立与 SetSubscriptions 共同触发
func (c *Client) reconcile() error {
	// 串行化，避免并发下发重复的 SUBSCRIBE 或相互覆盖 applied
	c.reconcile_mu.Lock()
	defer c.reconcile_mu.Unlock()

	c.mu.Lock()
	if c.conn == nil {
		// 尚未连接：期望集合已记录，连上后由 run 再次调用
		c.mu.Unlock()
		return nil
	}
	var add []string
	var remove []string
	for topic, qos := range c.wanted {
		if applied, ok := c.applied[topic]; !ok || applied != qos {
			add = append(add, topic)
		}
	}
	for topic := range c.applied {
		if _, ok := c.wanted[topic]; !ok {
			remove = append(remove, topic)
		}
	}
	c.mu.Unlock()

	sort.Strings(add)
	sort.Strings(remove)
	for _, topic := range add {
		if err := c.subscribe_one(topic, c.wanted_qos(topic)); err != nil {
			return err
		}
	}
	for _, topic := range remove {
		if err := c.unsubscribe_one(topic); err != nil {
			return err
		}
	}
	return nil
}

// wanted_qos 取出某个主题期望的 QoS
func (c *Client) wanted_qos(topic string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wanted[topic]
}

// subscribe_one 下发一条订阅并等待 SUBACK
func (c *Client) subscribe_one(topic string, qos int) error {
	if !ValidTopicFilter(topic) {
		return fmt.Errorf("非法的 MQTT 主题过滤器 %q", topic)
	}
	packet_id, ch, err := c.register()
	if err != nil {
		return err
	}
	defer c.unregister(packet_id)

	if err = c.send(build_subscribe(packet_id, topic, qos)); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.base_ctx(), ack_timeout)
	defer cancel()
	code, err := c.wait(ctx, ch)
	if err != nil {
		return fmt.Errorf("订阅 %s 失败: %w", topic, err)
	}
	if code >= 0x80 {
		return fmt.Errorf("broker 拒绝订阅 %s，返回码 0x%02x", topic, code)
	}

	c.mu.Lock()
	c.applied[topic] = qos
	c.mu.Unlock()
	return nil
}

// unsubscribe_one 撤销一条订阅并等待 UNSUBACK
func (c *Client) unsubscribe_one(topic string) error {
	packet_id, ch, err := c.register()
	if err != nil {
		return err
	}
	defer c.unregister(packet_id)

	if err = c.send(build_unsubscribe(packet_id, topic)); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.base_ctx(), ack_timeout)
	defer cancel()
	if _, err = c.wait(ctx, ch); err != nil {
		return fmt.Errorf("退订 %s 失败: %w", topic, err)
	}

	c.mu.Lock()
	delete(c.applied, topic)
	c.mu.Unlock()
	return nil
}
