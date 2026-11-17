package events

import (
	"context"
	"encoding/json"
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
