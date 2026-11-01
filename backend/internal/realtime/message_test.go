package realtime

import (
	"encoding/json"
	"testing"
)

func TestMessageIncludesZeroTotal(t *testing.T) {
	payload, err := json.Marshal(Message{Type: "snapshot"})
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if got, ok := fields["total"]; !ok || string(got) != "0" {
		t.Fatalf("snapshot total = %s, present = %t; want 0", got, ok)
	}
}
