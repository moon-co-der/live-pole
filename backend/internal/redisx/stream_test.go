package redisx

import "testing"

func TestParseStreamMessages(t *testing.T) {
	response := []any{
		[]any{"votes:stream", []any{
			[]any{"1710000000000-0", []any{"voteId", "id-1", "pollId", "poll-1", "optionId", "option-2", "voterHash", "hashed"}},
		}},
	}
	messages := parseStreamMessages(response)
	if len(messages) != 1 || messages[0].ID != "1710000000000-0" || messages[0].Fields["optionId"] != "option-2" || messages[0].Fields["voterHash"] != "hashed" {
		t.Fatalf("unexpected parsed stream messages: %#v", messages)
	}
}

func TestIsOKResponse(t *testing.T) {
	if !isOKResponse("+OK") {
		t.Fatal("expected Redis simple-string OK response to be accepted")
	}
	if isOKResponse("OK") || isOKResponse(nil) {
		t.Fatal("expected non-RESP OK values to be rejected")
	}
}
