// Package generator 实现 protoc-gen-go-panda 的声明校验和代码生成。
package generator

import (
	"fmt"

	routev1 "github.com/yola1107/panda/api/panda/route/v1"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

type method struct {
	proto *protogen.Method
	route *routev1.Route
}

type push struct {
	message *protogen.Message
	route   *routev1.Push
}

type service struct {
	proto   *protogen.Service
	file    *protogen.File
	methods []method
	pushes  []push
	numbers map[uint32]string
}

// Generate 校验全部逻辑服务，只有校验通过后才输出文件。
func Generate(plugin *protogen.Plugin) error {
	services, err := collectServices(plugin.Files)
	if err != nil {
		return err
	}

	for _, file := range plugin.Files {
		if err := readPushes(services, file.Messages, file.Generate); err != nil {
			return err
		}
	}

	for _, file := range plugin.Files {
		if !file.Generate {
			continue
		}

		active, err := activeServices(file, services)
		if err != nil {
			return err
		}

		if len(active) > 0 {
			generateFile(plugin, file, active)
		}
	}

	plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
	return nil
}

func collectServices(files []*protogen.File) (map[string]*service, error) {
	services := make(map[string]*service)
	for _, file := range files {
		for _, descriptor := range file.Services {
			s := &service{proto: descriptor, file: file, numbers: make(map[uint32]string)}
			if err := s.readMethods(); err != nil {
				return nil, err
			}

			services[string(descriptor.Desc.FullName())] = s
		}
	}

	return services, nil
}

func (s *service) readMethods() error {
	for _, descriptor := range s.proto.Methods {
		if !proto.HasExtension(descriptor.Desc.Options(), routev1.E_Route) {
			continue
		}

		route, ok := proto.GetExtension(descriptor.Desc.Options(), routev1.E_Route).(*routev1.Route)
		if !ok || route == nil {
			return fmt.Errorf("%s: invalid route option", descriptor.Desc.FullName())
		}

		if descriptor.Desc.IsStreamingClient() || descriptor.Desc.IsStreamingServer() {
			return fmt.Errorf("%s: streaming methods are not client routes", descriptor.Desc.FullName())
		}

		switch route.Kind {
		case routev1.Kind_KIND_LOGIN, routev1.Kind_KIND_REQUEST, routev1.Kind_KIND_NOTIFY:
		case routev1.Kind_KIND_UNSPECIFIED:
			return fmt.Errorf("%s: route kind is required", descriptor.Desc.FullName())
		default:
			return fmt.Errorf("%s: unknown route kind %d", descriptor.Desc.FullName(), route.Kind)
		}

		if err := s.claim(route.Number, string(descriptor.Desc.FullName())); err != nil {
			return err
		}

		s.methods = append(s.methods, method{proto: descriptor, route: route})
	}

	return nil
}

func readPushes(services map[string]*service, messages []*protogen.Message, generated bool) error {
	for _, message := range messages {
		if err := readPushes(services, message.Messages, generated); err != nil {
			return err
		}

		if !proto.HasExtension(message.Desc.Options(), routev1.E_Push) {
			continue
		}

		if err := registerPush(services, message, generated); err != nil {
			return err
		}
	}

	return nil
}

func registerPush(services map[string]*service, message *protogen.Message, generated bool) error {
	route, ok := proto.GetExtension(message.Desc.Options(), routev1.E_Push).(*routev1.Push)
	if !ok || route == nil || route.Service == "" {
		return fmt.Errorf("%s: push service is required", message.Desc.FullName())
	}

	s := services[route.Service]
	if s == nil {
		return fmt.Errorf("%s: push references unknown service %q", message.Desc.FullName(), route.Service)
	}

	if generated && !s.file.Generate {
		return fmt.Errorf("%s: push service %s must be generated in the same invocation", message.Desc.FullName(), route.Service)
	}

	if err := s.claim(route.Number, string(message.Desc.FullName())); err != nil {
		return err
	}

	s.pushes = append(s.pushes, push{message: message, route: route})
	return nil
}

func (s *service) claim(number uint32, name string) error {
	if number == 0 {
		return fmt.Errorf("%s: route zero is reserved", name)
	}
	if previous, exists := s.numbers[number]; exists {
		return fmt.Errorf("%s: duplicate route %d (%s, %s)", s.proto.Desc.FullName(), number, previous, name)
	}

	s.numbers[number] = name
	return nil
}

func activeServices(file *protogen.File, services map[string]*service) ([]*service, error) {
	var active []*service
	for _, descriptor := range file.Services {
		s := services[string(descriptor.Desc.FullName())]
		if len(s.methods) == 0 && len(s.pushes) == 0 {
			continue
		}

		if err := s.validateLogin(); err != nil {
			return nil, err
		}
		if err := s.validateNames(); err != nil {
			return nil, err
		}

		active = append(active, s)
	}

	return active, nil
}

func (s *service) validateLogin() error {
	logins := 0
	for _, method := range s.methods {
		if method.route.Kind == routev1.Kind_KIND_LOGIN {
			logins++
		}
	}

	if logins != 1 {
		return fmt.Errorf("%s: exactly one login route is required", s.proto.Desc.FullName())
	}

	return nil
}

func (s *service) validateNames() error {
	// 路由常量与客户端方法分别检查名称冲突。
	routes := make(map[string]string)
	methods := make(map[string]string)
	for _, m := range s.methods {
		name := string(m.proto.Desc.FullName())
		routes[m.proto.GoName] = name
		methods[m.proto.GoName] = name
	}

	for _, p := range s.pushes {
		name := p.message.GoIdent.GoName
		if conflict := routes["Push"+name]; conflict != "" {
			return fmt.Errorf("%s: generated route identifier conflicts with %s", p.message.Desc.FullName(), conflict)
		}
		if conflict := methods["On"+name]; conflict != "" {
			return fmt.Errorf("%s: generated callback identifier conflicts with %s", p.message.Desc.FullName(), conflict)
		}

		routes["Push"+name] = string(p.message.Desc.FullName())
		methods["On"+name] = string(p.message.Desc.FullName())
	}

	return nil
}
