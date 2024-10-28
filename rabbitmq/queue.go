package rabbitmq

import (
	"middleware/common"
	"os"
	"os/signal"
	"syscall"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Queue struct {
	Channel      *amqp.Channel
	ExternalName string
	Name         string
	Durable      bool
	AutoDeleted  bool
	Exclusive    bool
	NoWait       bool
	Arguments    []string
}

func (q *Queue) Declare() {
	queue, err := q.Channel.QueueDeclare(
		q.ExternalName,
		q.Durable,
		q.AutoDeleted,
		q.Exclusive,
		q.NoWait,
		nil,
	)
	common.FailOnError(err, "Failed to declare a queue")
	q.Name = queue.Name
}

func (q *Queue) Bind(exchange *Exchange, routingKey string) {
	err := q.Channel.QueueBind(
		q.Name,
		routingKey,
		exchange.Name,
		false,
		nil,
	)
	common.FailOnError(err, "Failed to bind a queue")

	log.Debugf("Bound queue %s to exchange %s with routing key %s", q.Name, exchange.Name, routingKey)
}

func (q *Queue) Consume() <-chan amqp.Delivery {

	messages, err := q.Channel.Consume(
		q.Name,
		q.Name,
		false,
		false,
		false,
		false,
		nil,
	)
	go func() {
		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGTERM)
		<-term
		log.Debugf("Cancelling channel for queue %s", q.Name)
		q.Channel.Cancel(q.Name, false)
	}()

	common.FailOnError(err, "Failed to consume messages")

	return messages
}
