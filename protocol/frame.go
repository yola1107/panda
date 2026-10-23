package protocol

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// ReadTCP 在分配内存前检查长度前缀，最多分配 maxSize 字节。
func ReadTCP(r io.Reader, maxSize int) (Message, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return Message{}, err
	}

	size := binary.BigEndian.Uint32(prefix[:])
	if maxSize < HeaderSize || size < HeaderSize || uint64(size) > uint64(maxSize) {
		return Message{}, errors.New("invalid TCP frame size")
	}

	b := make([]byte, int(size))
	if _, err := io.ReadFull(r, b); err != nil {
		return Message{}, err
	}

	return Decode(b, maxSize)
}

// WriteTCP 写入长度前缀和信封；并发写入由调用方串行化。
func WriteTCP(w io.Writer, m Message, maxSize int) error {
	b, err := Encode(m, maxSize)
	if err != nil {
		return err
	}

	return WriteFrame(w, b)
}

// WriteFrame 为已编码的信封添加 TCP 长度前缀。
func WriteFrame(w io.Writer, b []byte) error {
	size := len(b)
	if uint64(size) > math.MaxUint32 {
		return errors.New("TCP frame exceeds uint32 length")
	}

	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(size))

	if err := writeFull(w, prefix[:]); err != nil {
		return err
	}

	return writeFull(w, b)
}

func writeFull(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}

		if n <= 0 || n > len(b) {
			return io.ErrShortWrite
		}

		b = b[n:]
	}

	return nil
}

// ReadWebSocket 解码单个二进制消息的 reader，最多分配 maxSize 字节。
func ReadWebSocket(reader io.Reader, maxSize int) (Message, error) {
	if maxSize < HeaderSize {
		return Message{}, errors.New("invalid WebSocket maximum size")
	}
	data := make([]byte, maxSize)

	size, err := io.ReadFull(reader, data)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Message{}, err
	}

	if err == nil {
		var extra [1]byte
		if _, err := io.ReadFull(reader, extra[:]); !errors.Is(err, io.EOF) {
			return Message{}, errors.New("WebSocket frame exceeds maximum size")
		}
	}

	return Decode(data[:size], maxSize)
}
