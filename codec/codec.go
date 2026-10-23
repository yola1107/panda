// Package codec 提供入口使用的 Protobuf 与 JSON 编码器。
package codec

import (
	"fmt"

	"github.com/go-kratos/kratos/v3/encoding"
	// 注册入口支持的 Kratos 编码器。
	_ "github.com/go-kratos/kratos/v3/encoding/proto"
	_ "github.com/go-kratos/kratos/v3/encoding/protojson"
)

// Lookup 将入口的 json 映射到 Protobuf JSON，未知名称返回错误。
func Lookup(name string) (encoding.Codec, error) {
	switch name {
	case "proto":
		return encoding.GetCodec("proto"), nil
	case "json":
		return encoding.GetCodec("protojson"), nil
	default:
		return nil, fmt.Errorf("unsupported codec %q", name)
	}
}
