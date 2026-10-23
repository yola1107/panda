// Package protocol 定义客户端信封和有界分帧。
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Type 标识客户端消息类型。
type Type uint8

// 消息类型在 TCP 与 WebSocket 上使用相同编号。
const (
	Request Type = iota + 1
	Response
	Push
	Notify
	Heartbeat
)

// HeaderSize 是信封固定头部的字节数。
const HeaderSize = 13

// DefaultMaxSize 是包含头部、不含 TCP 长度前缀的默认最大包长。
const DefaultMaxSize = 64 * 1024

// Message 的 Payload 与解码输入共享内存，调用方须保持其不可变。
type Message struct {
	Type      Type
	Route     uint32
	RequestID uint32
	Code      int32
	Payload   []byte
}

// Encode 校验信封并编码为大端序。
func Encode(m Message, maxSize int) ([]byte, error) {
	if maxSize < HeaderSize || len(m.Payload) > maxSize-HeaderSize {
		return nil, errors.New("envelope exceeds maximum size")
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}

	b := make([]byte, HeaderSize+len(m.Payload))
	b[0] = byte(m.Type)
	binary.BigEndian.PutUint32(b[1:5], m.Route)
	binary.BigEndian.PutUint32(b[5:9], m.RequestID)
	binary.BigEndian.PutUint32(b[9:13], encodeCode(m.Code))
	copy(b[HeaderSize:], m.Payload)

	return b, nil
}

// Decode 不分配 payload 内存，拒绝超长或无效信封。
func Decode(b []byte, maxSize int) (Message, error) {
	if len(b) < HeaderSize || len(b) > maxSize {
		return Message{}, errors.New("invalid envelope size")
	}

	m := Message{
		Type: Type(b[0]), Route: binary.BigEndian.Uint32(b[1:5]),
		RequestID: binary.BigEndian.Uint32(b[5:9]), Code: decodeCode(binary.BigEndian.Uint32(b[9:13])),
		Payload: b[HeaderSize:],
	}

	return m, m.Validate()
}

// Validate 拒绝消息类型不使用的字段以及保留的零路由和零 RequestID。
func (m Message) Validate() error {
	if m.Type != Response && m.Code != 0 {
		return errors.New("only responses can carry an error code")
	}

	valid := false

	switch m.Type {
	case Request, Response:
		valid = m.Route != 0 && m.RequestID != 0
	case Push, Notify:
		valid = m.Route != 0 && m.RequestID == 0
	case Heartbeat:
		valid = m.Route == 0 && m.RequestID == 0 && len(m.Payload) == 0
	}

	if !valid {
		return fmt.Errorf("invalid envelope fields for type %d", m.Type)
	}

	return nil
}

func encodeCode(code int32) uint32 {
	bits := uint32(code & math.MaxInt32)
	if code < 0 {
		bits |= 1 << 31
	}

	return bits
}

func decodeCode(bits uint32) int32 {
	code := int32(bits & math.MaxInt32)
	if bits>>31 != 0 {
		code += math.MinInt32
	}

	return code
}
