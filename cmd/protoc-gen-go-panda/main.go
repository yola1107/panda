// Package main protoc-gen-go-panda 生成长连接业务的 Handler 和类型化 Go 客户端。
package main

import (
	"github.com/yola1107/panda/internal/generator"

	"google.golang.org/protobuf/compiler/protogen"
)

func main() { protogen.Options{}.Run(generator.Generate) }
