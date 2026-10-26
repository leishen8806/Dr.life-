package queue

import (
	"testing"
	"time"
)

func TestRegistryRejectsInvalidAndDuplicateSpecs(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(TypeSpec{Name: "invalid", Queue: QueueCritical, Timeout: time.Second, MaxRetry: 1}); err == nil {
		t.Fatal("invalid task type was registered")
	}
	spec := TypeSpec{Name: "ledger:outbox_relay:v1", Queue: QueueCritical, Timeout: time.Second, MaxRetry: 1}
	if err := registry.Register(spec); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(spec); err == nil {
		t.Fatal("duplicate task type was registered")
	}
	if _, ok := registry.Lookup("missing:task:v1"); ok {
		t.Fatal("unregistered task type was found")
	}
}
