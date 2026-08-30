package planner

import (
	"context"
	"reflect"
	"testing"
)

func TestLocalIsDeterministic(t *testing.T) {
	request := Request{Prompt: "A victorious shipping dance"}
	first, err := (Local{}).Plan(context.Background(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	second, err := (Local{}).Plan(context.Background(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("plans differ:\n%#v\n%#v", first, second)
	}
}

func TestLocalSeedRerollsPlan(t *testing.T) {
	first, _ := (Local{}).Plan(context.Background(), Request{Prompt: "hello", Seed: 1})
	second, _ := (Local{}).Plan(context.Background(), Request{Prompt: "hello", Seed: 2})
	if reflect.DeepEqual(first.Spec, second.Spec) {
		t.Fatal("different seeds produced identical plans")
	}
}
