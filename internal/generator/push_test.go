package generator_test

import (
	"testing"

	routev1 "github.com/yola1107/panda/api/panda/route/v1"
	"github.com/yola1107/panda/internal/generator"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func pushMessage(name, service string, number uint32) *descriptorpb.DescriptorProto {
	options := &descriptorpb.MessageOptions{}
	proto.SetExtension(options, routev1.E_Push, &routev1.Push{Number: number, Service: service})

	return &descriptorpb.DescriptorProto{
		Name:    new(name),
		Options: options,
		Field: []*descriptorpb.FieldDescriptorProto{{
			Name:   new("value"),
			Number: proto.Int32(1),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		}},
	}
}

func TestPushOwnership(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, service string
		number        uint32
		valid         bool
	}{
		{name: "valid", service: fixtureService, number: 3, valid: true},
		{name: "missing", number: 3},
		{name: "unknown", service: "fixture.Missing", number: 3},
		{name: "login-conflict", service: fixtureService, number: 1},
		{name: "request-conflict", service: fixtureService, number: 2},
		{name: "reserved-zero", service: fixtureService},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := fixture()
			request.ProtoFile[1].MessageType = []*descriptorpb.DescriptorProto{pushMessage("Event", test.service, test.number)}

			plugin, err := (protogen.Options{}).New(request)
			if err != nil {
				t.Fatal(err)
			}

			err = generator.Generate(plugin)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t, got %v", test.valid, err)
			}
		})
	}
}

func TestGeneratedNameCollision(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"PushEvent", "OnEvent"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := fixture()
			file := request.ProtoFile[1]
			file.Service[0].Method[1].Name = new(name)
			file.MessageType = []*descriptorpb.DescriptorProto{pushMessage("Event", fixtureService, 3)}
			plugin, err := (protogen.Options{}).New(request)
			if err != nil {
				t.Fatal(err)
			}
			if err := generator.Generate(plugin); err == nil {
				t.Fatal("generated identifier collision accepted")
			}
		})
	}
}
