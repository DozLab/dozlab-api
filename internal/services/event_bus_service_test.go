package services

import (
	"context"
	"errors"
	"testing"

	"dozlab-backend/internal/websocket"
)

type fakeTransport struct {
	published  []*websocket.Event
	failFirst  int
	handlers   map[websocket.EventType]websocket.EventHandler
	subscribed []websocket.EventType
}

func (f *fakeTransport) Publish(ctx context.Context, e *websocket.Event) error {
	if f.failFirst > 0 {
		f.failFirst--
		return errors.New("broker down")
	}
	f.published = append(f.published, e)
	return nil
}

func (f *fakeTransport) RegisterHandler(t websocket.EventType, h websocket.EventHandler) {
	if f.handlers == nil {
		f.handlers = map[websocket.EventType]websocket.EventHandler{}
	}
	f.handlers[t] = h
}

func (f *fakeTransport) Subscribe(ctx context.Context, types ...websocket.EventType) error {
	f.subscribed = append(f.subscribed, types...)
	return nil
}

func TestEventBusServicePublishEventPublishes(t *testing.T) {
	transport := &fakeTransport{}
	svc := NewEventBusService(transport)

	event := &websocket.Event{ID: "e1", Type: websocket.EventSessionCreated}
	if err := svc.PublishEvent(context.Background(), event); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}
	if len(transport.published) != 1 || transport.published[0] != event {
		t.Errorf("published = %v, want the event once", transport.published)
	}
}

func TestEventBusServicePublishEventSwallowsErrors(t *testing.T) {
	svc := NewEventBusService(&fakeTransport{failFirst: 1})
	if err := svc.PublishEvent(context.Background(), map[string]string{"k": "v"}); err != nil {
		t.Errorf("non-critical publish returned %v, want nil", err)
	}
}

func TestEventBusServicePublishCriticalEventRetries(t *testing.T) {
	tests := []struct {
		name      string
		failFirst int
		wantErr   bool
	}{
		{"succeeds after two failures", 2, false},
		{"fails after three attempts", 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &fakeTransport{failFirst: tt.failFirst}
			svc := NewEventBusService(transport)
			err := svc.PublishCriticalEvent(context.Background(), &websocket.Event{ID: "c1"})
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEventBusServiceHandlers(t *testing.T) {
	transport := &fakeTransport{}
	svc := NewEventBusService(transport)

	var got interface{}
	svc.RegisterHandler("lab.started", func(ctx context.Context, e interface{}) error {
		got = e
		return nil
	})
	if err := svc.Subscribe(context.Background(), []string{"lab.started"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.subscribed) != 1 || transport.subscribed[0] != websocket.EventLabStarted {
		t.Errorf("subscribed = %v", transport.subscribed)
	}
	event := &websocket.Event{ID: "h1", Type: websocket.EventLabStarted}
	if err := transport.handlers[websocket.EventLabStarted](context.Background(), event); err != nil || got != event {
		t.Errorf("handler adapter: err=%v got=%v", err, got)
	}
}
