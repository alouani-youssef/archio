package events

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"archio/internal/config"
	"github.com/google/uuid"
)

func TestObjectVersionCreatedEvent_JSON(t *testing.T) {
	event := ObjectVersionCreatedEvent{
		VersionID:      uuid.New(),
		ObjectKey:      "docs/a.txt",
		BucketID:       7,
		BucketName:     "main",
		GroupID:        uuid.New(),
		Size:           42,
		ChecksumSHA256: "abc",
		ContentType:    "text/plain",
		CreatedAt:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"versionId", "objectKey", "bucketId", "bucketName", "groupId", "size", "checksumSha256", "contentType", "createdAt"}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want exactly %v", got, want)
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in %s", key, raw)
		}
	}
}

func TestAMQPPublisher_Publish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	broker, err := Open(ctx, config.BrokerConfig{Host: "127.0.0.1", Port: 5672, Username: "archio", Password: "rabbitmq_local_password"})
	if err != nil {
		t.Skipf("rabbitmq unavailable: %v", err)
	}
	defer broker.Close()

	publisher, err := NewPublisher(broker)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()

	ch, err := broker.Connection().Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	queue, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(queue.Name, ObjectVersionCreated, Exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	deliveries, err := ch.Consume(queue.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	event := ObjectVersionCreatedEvent{VersionID: uuid.New(), ObjectKey: "k", BucketID: 1, BucketName: "b", GroupID: uuid.New(), Size: 3, ChecksumSHA256: "c", ContentType: "text/plain", CreatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := publisher.Publish(ctx, ObjectVersionCreated, event); err != nil {
		t.Fatal(err)
	}

	select {
	case d := <-deliveries:
		if d.ContentType != "application/json" {
			t.Errorf("content type = %q", d.ContentType)
		}
		var got ObjectVersionCreatedEvent
		if err := json.Unmarshal(d.Body, &got); err != nil {
			t.Fatal(err)
		}
		if got != event {
			t.Errorf("got %+v, want %+v", got, event)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}

func TestNoopPublisher(t *testing.T) {
	var p Publisher = NoopPublisher{}
	if err := p.Publish(context.Background(), ObjectVersionCreated, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAMQPPublisher_PublishCancelledContext(t *testing.T) {
	p := &AMQPPublisher{sem: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Publish(ctx, ObjectVersionCreated, map[string]string{"a": "b"}); err == nil {
		t.Fatal("expected error for cancelled context")
	}
	// A blocked publisher must still honor cancellation while waiting.
	p.sem <- struct{}{}
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := p.Publish(ctx, ObjectVersionCreated, "x"); err == nil {
		t.Fatal("expected error when waiting past deadline")
	}
}

func openTestBroker(t *testing.T, ctx context.Context) *Broker {
	t.Helper()
	broker, err := Open(ctx, config.BrokerConfig{Host: "127.0.0.1", Port: 5672, Username: "archio", Password: "rabbitmq_local_password"})
	if err != nil {
		t.Skipf("rabbitmq unavailable: %v", err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	return broker
}

func TestAMQPPublisher_PublishAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	publisher, err := NewPublisher(openTestBroker(t, ctx))
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Close(); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(ctx, ObjectVersionCreated, "x"); err == nil {
		t.Fatal("expected error publishing after Close")
	}
}

func TestAMQPPublisher_ConcurrentPublish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	broker := openTestBroker(t, ctx)
	publisher, err := NewPublisher(broker)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()

	ch, err := broker.Connection().Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	queue, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(queue.Name, ObjectVersionCreated, Exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	deliveries, err := ch.Consume(queue.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- publisher.Publish(ctx, ObjectVersionCreated, map[string]int{"i": i})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[int]bool{}
	for len(seen) < n {
		select {
		case d := <-deliveries:
			var m map[string]int
			if err := json.Unmarshal(d.Body, &m); err != nil {
				t.Fatal(err)
			}
			seen[m["i"]] = true
		case <-ctx.Done():
			t.Fatalf("received %d of %d messages", len(seen), n)
		}
	}
}
