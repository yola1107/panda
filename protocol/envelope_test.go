package protocol_test

import (
	"bytes"
	"testing"

	"github.com/yola1107/panda/protocol"
)

func TestEnvelope(t *testing.T) {
	t.Parallel()

	want := protocol.Message{
		Type:      protocol.Request,
		Route:     0x01020304,
		RequestID: 0x05060708,
		Payload:   []byte("hi"),
	}
	encoded, err := protocol.Encode(want, protocol.DefaultMaxSize)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(encoded, []byte{1, 1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 'h', 'i'}) {
		t.Fatalf("unexpected wire bytes: %x", encoded)
	}

	got, err := protocol.Decode(encoded, protocol.DefaultMaxSize)
	if err != nil {
		t.Fatal(err)
	}

	if got.Type != want.Type ||
		got.Route != want.Route ||
		got.RequestID != want.RequestID ||
		!bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("round trip: got %+v, want %+v", got, want)
	}
}
