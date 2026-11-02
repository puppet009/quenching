// Package eventbus 提供基于 RabbitMQ 的领域事件发布/订阅。
// 跨服务协作走事件（如 fault_resolved、pitfall_outcome），不做同步互调串状态。
package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Event 领域事件信封。
type Event struct {
	Type       string          `json:"type"`      // 事件类型，如 fault.resolved
	Aggregate  string          `json:"aggregate"` // 聚合根，如 training_session
	ID         string          `json:"id"`        // 聚合根 ID
	Payload    json.RawMessage `json:"payload"`   // 事件数据
	OccurredAt time.Time       `json:"occurred_at"`
	TraceID    string          `json:"trace_id"`
}

// Bus RabbitMQ 事件总线。
type Bus struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

// New 连接 RabbitMQ 并声明 topic 交换机。
func New(url, exchange string) (*Bus, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("打开 channel 失败: %w", err)
	}
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		return nil, fmt.Errorf("声明交换机失败: %w", err)
	}
	return &Bus{conn: conn, ch: ch}, nil
}

// Publish 发布事件，routingKey 形如 "fault.resolved"。
func (b *Bus) Publish(ctx context.Context, exchange, routingKey string, e Event) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return b.ch.PublishWithContext(ctx, exchange, routingKey, false, false,
		amqp.Publishing{ContentType: "application/json", Body: body})
}

// Close 释放连接。
func (b *Bus) Close() {
	if b.ch != nil {
		_ = b.ch.Close()
	}
	if b.conn != nil {
		_ = b.conn.Close()
	}
}
