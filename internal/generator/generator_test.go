package generator_test

import (
	"strings"
	"testing"

	routev1 "github.com/yola1107/panda/api/panda/route/v1"
	"github.com/yola1107/panda/internal/generator"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"google.golang.org/protobuf/types/pluginpb"
)

const fixtureService = "fixture.Echo"

func fixture() *pluginpb.CodeGeneratorRequest {
	method := func(name string, number uint32, kind routev1.Kind) *descriptorpb.MethodDescriptorProto {
		options := &descriptorpb.MethodOptions{}
		proto.SetExtension(options, routev1.E_Route, &routev1.Route{Number: number, Kind: kind})

		return &descriptorpb.MethodDescriptorProto{
			Name:       new(name),
			InputType:  new(".google.protobuf.StringValue"),
			OutputType: new(".google.protobuf.StringValue"),
			Options:    options,
		}
	}
	file := &descriptorpb.FileDescriptorProto{
		Name:       new("fixture.proto"),
		Package:    new("fixture"),
		Syntax:     new("proto3"),
		Dependency: []string{"google/protobuf/wrappers.proto"},
		Options:    &descriptorpb.FileOptions{GoPackage: new("example.com/fixture;fixture")},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: new("Echo"),
			Method: []*descriptorpb.MethodDescriptorProto{
				method("Login", 1, routev1.Kind_KIND_LOGIN),
				method("Echo", 2, routev1.Kind_KIND_REQUEST),
			},
		}},
	}

	return &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"fixture.proto"},
		ProtoFile: []*descriptorpb.FileDescriptorProto{
			protodesc.ToFileDescriptorProto(wrapperspb.File_google_protobuf_wrappers_proto),
			file,
		},
	}
}

func TestDuplicateRoute(t *testing.T) {
	t.Parallel()

	request := fixture()
	proto.SetExtension(request.ProtoFile[1].Service[0].Method[1].Options, routev1.E_Route, &routev1.Route{
		Number: 1,
		Kind:   routev1.Kind_KIND_REQUEST,
	})

	plugin, err := (protogen.Options{}).New(request)
	if err != nil {
		t.Fatal(err)
	}

	if err := generator.Generate(plugin); err == nil || !strings.Contains(err.Error(), "1") {
		t.Fatalf("expected duplicate route 1, got %v", err)
	}
}
