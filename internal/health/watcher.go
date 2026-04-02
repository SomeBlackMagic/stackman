package health

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	dockerevents "github.com/docker/docker/api/types/events"

	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

const subscriberBufferSize = 64

// Watcher converts low-level Docker events into normalized stack health events.
type Watcher struct {
	client    swarmint.DockerClient
	stackName string

	mu          sync.RWMutex
	subscribers []chan Event
}

// NewWatcher creates an event watcher bound to one stack namespace.
func NewWatcher(client swarmint.DockerClient, stackName string) *Watcher {
	return &Watcher{
		client:    client,
		stackName: stackName,
	}
}

// Subscribe registers a buffered event subscriber.
func (w *Watcher) Subscribe() <-chan Event {
	ch := make(chan Event, subscriberBufferSize)

	w.mu.Lock()
	w.subscribers = append(w.subscribers, ch)
	w.mu.Unlock()

	return ch
}

// Start listens Docker events until context cancellation or stream error.
func (w *Watcher) Start(ctx context.Context) error {
	eventsCh, errsCh := w.client.Events(ctx, dockerevents.ListOptions{})

	for {
		if eventsCh == nil && errsCh == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-errsCh:
			if !ok {
				errsCh = nil
				continue
			}
			if err != nil {
				return fmt.Errorf("watch docker events: %w", err)
			}
		case msg, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			if msg.Actor.Attributes["com.docker.stack.namespace"] != w.stackName {
				continue
			}

			event := w.convertEvent(msg)
			if event == nil {
				continue
			}
			w.broadcast(*event)
		}
	}
}

func (w *Watcher) broadcast(event Event) {
	w.mu.RLock()
	subs := make([]chan Event, len(w.subscribers))
	copy(subs, w.subscribers)
	w.mu.RUnlock()

	for _, subscriber := range subs {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (w *Watcher) convertEvent(msg dockerevents.Message) *Event {
	event := &Event{
		TaskID:      msg.Actor.Attributes["com.docker.swarm.task.id"],
		ServiceID:   msg.Actor.Attributes["com.docker.swarm.service.id"],
		ContainerID: msg.Actor.ID,
		Timestamp:   eventTimestamp(msg),
		Message:     string(msg.Action),
	}

	switch msg.Action {
	case "create":
		event.Type = EventTypeCreated
	case "start":
		event.Type = EventTypeStarted
	case "die", "kill":
		event.Type = EventTypeFailed
	case "destroy":
		event.Type = EventTypeShutdown
	case "health_status: healthy":
		event.Type = EventTypeHealthy
	case "health_status: unhealthy":
		event.Type = EventTypeUnhealthy
	default:
		if strings.HasPrefix(string(msg.Action), "health_status:") {
			event.Type = EventTypeUnhealthy
		} else {
			return nil
		}
	}

	return event
}

func eventTimestamp(msg dockerevents.Message) time.Time {
	if msg.TimeNano > 0 {
		return time.Unix(0, msg.TimeNano).UTC()
	}
	if msg.Time > 0 {
		return time.Unix(msg.Time, 0).UTC()
	}
	return time.Now().UTC()
}
