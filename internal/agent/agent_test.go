package agent

import (
	"context"
	"testing"
)

type fake struct{ id string }

func (f fake) ID() string                 { return f.id }
func (f fake) Name() string               { return f.id }
func (f fake) Icon() string               { return "" }
func (f fake) PushSignal() PushSignal     { return PushPoll }
func (f fake) Capabilities() Capabilities { return Capabilities{} }
func (f fake) Detect(context.Context) Detection {
	return Detection{}
}
func (f fake) ListSessions(context.Context, int) ([]Session, error) { return nil, nil }
func (f fake) Meta(string, string) Meta                             { return Meta{} }

func TestRegistryOrderAndCollisions(t *testing.T) {
	ctx := context.Background()
	Register(fake{"test-b"})
	Register(fake{"test-a"})
	RegisterProvider("test", func(context.Context) []Agent {
		return []Agent{fake{"test-z"}, fake{"test-a"}, fake{"test-y"}}
	})

	var ids []string
	for _, a := range All(ctx) {
		ids = append(ids, a.ID())
	}
	want := []string{"test-b", "test-a", "test-y", "test-z"}
	if len(ids) != len(want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("got %v, want %v", ids, want)
		}
	}
	if _, ok := Get(ctx, "test-y"); !ok {
		t.Fatal("provider agent not found by Get")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Register did not panic")
		}
	}()
	Register(fake{"test-a"})
}
