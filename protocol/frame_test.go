package protocol_test

import (
	"bytes"
	"testing"

	"github.com/yola1107/panda/protocol"
)

func TestTCPFrame(t *testing.T) {
	t.Parallel()

	var wire bytes.Buffer
	if err := protocol.WriteTCP(&wire, protocol.Message{Type: protocol.Heartbeat}, 64); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(wire.Bytes()[:4], []byte{0, 0, 0, 13}) {
		t.Fatalf("length prefix: %x", wire.Bytes())
	}

	m, err := protocol.ReadTCP(&wire, 64)
	if err != nil || m.Type != protocol.Heartbeat {
		t.Fatalf("heartbeat: %+v, %v", m, err)
	}

	if _, err := protocol.ReadTCP(bytes.NewReader([]byte{255, 255, 255, 255}), 64); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

func FuzzWebSocket(f *testing.F) {
	f.Add([]byte{5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{255})
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := protocol.ReadWebSocket(bytes.NewReader(b), 1024)
		if err != nil {
			return
		}

		encoded, err := protocol.Encode(m, 1024)
		if err != nil || !bytes.Equal(b, encoded) {
			t.Fatalf("round trip failed: %v", err)
		}
	})
}

func FuzzTCP(f *testing.F) {
	f.Add([]byte{0, 0, 0, 13, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{255, 255, 255, 255})
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := protocol.ReadTCP(bytes.NewReader(b), 1024)
		if err != nil {
			return
		}

		var encoded bytes.Buffer
		if err := protocol.WriteTCP(&encoded, m, 1024); err != nil {
			t.Fatal(err)
		}

		if !bytes.HasPrefix(b, encoded.Bytes()) || len(encoded.Bytes()) > 1028 {
			t.Fatal("invalid bounded round trip")
		}
	})
}
