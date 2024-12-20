package controller

import (
	"fmt"
	"middleware/common"
	"middleware/rabbitmq"
	"middleware/worker/controller/enums"
	"middleware/worker/schema"
	"net"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/op/go-logging"
	amqp "github.com/rabbitmq/amqp091-go"
)

var log = logging.MustGetLogger("log")

const (
	Routing_Broadcast = iota
	Routing_Unicast
)

type HandlerFactory func(job common.JobID) (Handler, EOFValidator, error)

type EOFValidator interface {
	Finish(receivedEOFs map[enums.TokenName]uint) (*EOFMessage, bool)
}

type Protocol interface {
	Unmarshal(rawData []byte) (DataMessage, error)
	Marshal(common.JobID, *common.IdempotencyID, common.Serializable) (common.Serializable, error)
	Route(partitionKey string) (routingKey string)
	Broadcast() (routes []string)
}

type DataMessage interface {
	JobID() common.JobID
	IdemID() *common.IdempotencyID
	IsEOF() bool
	Data() []byte
}

type routing struct {
	Type int
	Key  string
}

type messageToSend struct {
	Routing  routing
	Sequence uint32
	Callback func()
	JobID    common.JobID
	Body     schema.Partitionable
	Ack      *amqp.Delivery
}

type messageFromQueue struct {
	Delivery amqp.Delivery
	Message  DataMessage
}

type Controller struct {
	name    string
	rcvFrom []*rabbitmq.Queue
	to      []*rabbitmq.Exchange

	protocol Protocol

	txFwd             chan<- *messageToSend
	rxFwd             <-chan *messageToSend
	factory           HandlerFactory
	handlers          map[common.JobID]*HandlerRuntime
	txFinish          chan<- *HandlerRuntime
	rxFinish          <-chan *HandlerRuntime
	runtimeWG         sync.WaitGroup
	ManagerConnection net.Conn
	Listener          net.Listener
}

func NewController(controllerName string, from []*rabbitmq.Queue, to []*rabbitmq.Exchange, protocol Protocol, handlerF HandlerFactory) *Controller {
	mts := make(chan *messageToSend, 50)
	h := make(chan *HandlerRuntime, 50)

	var err error = nil

	c := &Controller{
		name:     controllerName,
		rcvFrom:  from,
		to:       to,
		protocol: protocol,
		txFwd:    mts,
		rxFwd:    mts,
		factory:  handlerF,
		handlers: make(map[common.JobID]*HandlerRuntime),
		txFinish: h,
		rxFinish: h,
	}

	c.Listener, err = net.Listen("tcp", fmt.Sprintf(":%s", common.Config.GetString("worker.port")))
	common.FailOnError(err, "Failed to connect to listener")

	log.Infof("Worker listening on port %s", fmt.Sprintf(":%s", common.Config.GetString("worker.port")))

	go func() {
		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGTERM)
		<-term
		// Remove the artificial one
		log.Debugf("Received shutdown signal in controller")
		if c.Listener != nil {
			c.Listener.Close()
		}

		if c.ManagerConnection != nil {
			c.ManagerConnection.Close()
		}
	}()
	return c
}

func (q *Controller) getHandler(j common.JobID) (*HandlerRuntime, error) {
	v, ok := q.handlers[j]
	if !ok {
		h, eof, err := q.factory(j)
		if err != nil {
			return nil, err
		}
		hr, err := NewHandlerRuntime(
			q.name,
			j,
			h,
			eof,
			q.txFwd,
		)
		if err != nil {
			return nil, err
		}
		q.handlers[j] = hr
		v = hr
		q.runtimeWG.Add(1)
	}
	return v, nil
}

func (q *Controller) publish(routing string, m common.Serializable) {
	for _, ex := range q.to {
		ex.Publish(routing, m)
	}
}

func (q *Controller) broadcast(m common.Serializable) {
	rks := q.protocol.Broadcast()
	for _, k := range rks {
		q.publish(k, m)
	}
}

func (c *Controller) HandleManager() {
	defer c.ManagerConnection.Close()

	for {
		message, err := common.Receive(c.ManagerConnection)

		if err != nil {
			log.Errorf("Error receiving manager messages for controller %s", c.name)
			c.ManagerConnection.Close()
			time.Sleep(1 * time.Second)
			break
		}

		messageHealthCheck := common.ManagementMessage{Content: message}

		if !messageHealthCheck.IsHealthCheck() {
			log.Errorf("Expecting HealthCheck message from manager but received %s", messageHealthCheck)
			continue
		}

		if common.Send("ALV", c.ManagerConnection) != nil {
			log.Errorf("Error sending ALV to manager for controller %s", c.name)
			c.ManagerConnection.Close()
			time.Sleep(1 * time.Second)
			break
		}

	}
	log.Debugf("Finish listening for manager messages for controller %s", c.name)

}

func (q *Controller) buildIdemId(sequence uint32) *common.IdempotencyID {
	return &common.IdempotencyID{
		Origin:   q.name,
		Sequence: sequence,
	}
}

func (q *Controller) removeInactiveHandlersTask(s *sync.WaitGroup, rxFinish <-chan bool) {
	defer s.Done()
	ids := make([]uuid.UUID, 0)

	removeIds := func() {
		for _, id := range ids {
			delete(q.handlers, id)
		}
		ids = make([]uuid.UUID, 0)
	}

	finishHandler := func(h *HandlerRuntime) {
		log.Infof("Action: Removing Handler from List %s - %s", h.ControllerName, h.JobId)
		close(h.Tx)
		h.Finish()
		q.runtimeWG.Done()
		ids = append(ids, h.JobId)
	}

	d := 30 * time.Second
	timer := time.NewTimer(d)
	for {
		select {
		case <-rxFinish:
			for id := range q.handlers {
				finishHandler(q.handlers[id])
			}
			removeIds()
			return
		case <-timer.C:
			for id := range q.handlers {
				h := q.handlers[id]
				h.Mark++
				if h.Mark == 3 {
					// Give 2 passes for a little leeway in how much we want to wait
					// at the third, it means that for three passes the handled didn't do anything
					// we can safely close it
					finishHandler(q.handlers[id])
				}
			}
			removeIds()

			timer.Reset(d)
		}
	}

}

func (q *Controller) closingTask(s *sync.WaitGroup) {
	defer s.Done()
	// Once we got a Shutdown AND all the runtimes closed themselves, then close the controller.
	q.runtimeWG.Wait()
	// At this point, absolutely no handler runtime is running. We can close this safely
	close(q.txFinish)
	close(q.txFwd)
	log.Debugf("Shut down controller")
}

func (q *Controller) sendForwardTask(s *sync.WaitGroup) {
	defer s.Done()
	// Listen for messages to send until all handlers AND a shutdown happened.
	for mts := range q.rxFwd {
		m, err := q.protocol.Marshal(mts.JobID, q.buildIdemId(mts.Sequence), mts.Body)
		if err != nil {
			log.Error(err)
		}

		if mts.Routing.Type == Routing_Broadcast {
			q.broadcast(m)
			if mts.Ack != nil {
				mts.Ack.Ack(false)
			}
		}

		if mts.Routing.Type == Routing_Unicast {
			q.publish(q.protocol.Route(mts.Routing.Key), m)
			if mts.Ack != nil {
				mts.Ack.Ack(false)
			}
		}

		if mts.Callback != nil {
			mts.Callback()
		}
	}

	log.Debugf("Sent all pending messages")
}

func (c *Controller) listenManagerTask(s *sync.WaitGroup) {
	defer s.Done()
	for {
		var err error
		c.ManagerConnection, err = c.Listener.Accept()
		if err != nil {
			log.Errorf("Action: Accept connection | Result: Error | Error: %s", err)
			break
		}

		c.HandleManager()
	}
}

func (q *Controller) Start() {
	var end sync.WaitGroup
	f := make(chan bool, 1)

	// (1) Artificially add one to keep it spinning as long as we don't get a shutdown
	q.runtimeWG.Add(1)

	end.Add(1)
	go q.closingTask(&end)

	end.Add(1)
	go q.listenManagerTask(&end)

	end.Add(1)
	go q.sendForwardTask(&end)

	end.Add(1)
	go q.removeInactiveHandlersTask(&end, f)

	cases := make([]reflect.SelectCase, len(q.rcvFrom))
	for i, ch := range q.rcvFrom {
		cases[i] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ch.Consume()),
		}
	}

mainloop:
	for {
		chosen, value, ok := reflect.Select(cases)
		if !ok {
			// At this point, all queues are closed and no messages are in flight
			log.Infof("Closed Queue %s, exiting loop as all Queues are needed.", q.rcvFrom[chosen].ExternalName)
			break mainloop
		}
		d, ok := value.Interface().(amqp.Delivery)
		if !ok {
			log.Fatalf("This really shouldn't happen. How did we got here")
		}

		dm, err := q.protocol.Unmarshal(d.Body)
		if err != nil {
			log.Errorf("Error while parsing the Queue %s message: %s", q.rcvFrom[chosen].ExternalName, err)
			d.Nack(false, true)
			continue
		}

		h, err := q.getHandler(dm.JobID())
		if err != nil {
			log.Errorf("Error while getting a handler for Queue %s, JobID: %s Error: %s", q.rcvFrom[chosen].ExternalName, dm.JobID(), err)
			d.Nack(false, true)
			continue
		}

		h.Tx <- &messageFromQueue{
			Delivery: d,
			Message:  dm,
		}

	}
	log.Debugf("Ending main loop")
	// We have sent everything in flight, finalize the handlers
	f <- true
	close(f)

	// (2) Remove it once we have finished everything and no more messages are sent to handlers
	q.runtimeWG.Done()

	end.Wait()
	log.Debugf("Finalized main loop for controller")
}
