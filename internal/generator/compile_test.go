package generator_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	routev1 "github.com/yola1107/panda/api/panda/route/v1"
	"github.com/yola1107/panda/internal/generator"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func TestGeneratedCode(t *testing.T) {
	t.Parallel()

	request := fixture()
	file := request.ProtoFile[1]
	file.Service[0].Method[1].Name = new("Update")
	for i, m := range file.Service[0].Method {
		proto.SetExtension(m.Options, routev1.E_Route, &routev1.Route{
			Number: uint32(i + 10),
			Kind:   []routev1.Kind{routev1.Kind_KIND_LOGIN, routev1.Kind_KIND_REQUEST}[i],
		})
	}

	other, ok := proto.Clone(file.Service[0]).(*descriptorpb.ServiceDescriptorProto)
	if !ok {
		t.Fatal("service clone failed")
	}

	other.Name = new("Other")
	file.Service = append(file.Service, other)
	file.MessageType = []*descriptorpb.DescriptorProto{
		pushMessage("Update", fixtureService, 1),
		pushMessage("Second", "fixture.Other", 1),
	}

	plugin, err := (protogen.Options{}).New(request)
	if err != nil {
		t.Fatal(err)
	}

	if err := generator.Generate(plugin); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	writeGenerated(t, directory, plugin.Response())
	data, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	command := exec.CommandContext(t.Context(), "go", "run", "google.golang.org/protobuf/cmd/protoc-gen-go")
	command.Stdin = bytes.NewReader(data)

	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}

	response := &pluginpb.CodeGeneratorResponse{}
	if err := proto.Unmarshal(output, response); err != nil {
		t.Fatal(err)
	}

	writeGenerated(t, directory, response)

	testPath := filepath.Join(directory, "fixture_test.go")
	if err := os.WriteFile(testPath, []byte(generatedAcceptance), 0o600); err != nil {
		t.Fatal(err)
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	manifest := "module example.com/fixture\n\ngo 1.27.3\n\n" +
		"require github.com/yola1107/panda v0.0.0\n" +
		"replace github.com/yola1107/panda => " + strconv.Quote(root) + "\n"
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	command = exec.CommandContext(t.Context(), "go", "test", "-mod=mod", "-timeout=20s", ".")
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated code: %v\n%s", err, output)
	}
}

func writeGenerated(t *testing.T, directory string, response *pluginpb.CodeGeneratorResponse) {
	t.Helper()

	if response.GetError() != "" {
		t.Fatal(response.GetError())
	}

	for _, file := range response.File {
		path := filepath.Join(directory, filepath.Base(file.GetName()))
		if err := os.WriteFile(path, []byte(file.GetContent()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

const generatedAcceptance = `package fixture

import (
	"context"
	"testing"
	"time"

	"github.com/yola1107/panda/client"
	"github.com/yola1107/panda/gate"
	"github.com/yola1107/panda/node"
	"github.com/yola1107/panda/session"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

type business struct{ sessions chan session.Session }

func (*business) Login(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, string, error) {
	return wrapperspb.String("login"), "user", nil
}

func (h *business) Update(ctx context.Context, v *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	s, _ := session.FromContext(ctx)
	h.sessions <- s
	return v, nil
}

func TestGeneratedServices(t *testing.T) {
	for _, codec := range []string{"proto", "json"} {
		t.Run(codec, func(t *testing.T) {
			n, err := node.New(node.Config{})
			if err != nil {
				t.Fatal(err)
			}
			h := &business{sessions: make(chan session.Session, 2)}
			if err := RegisterEcho(n, h); err != nil {
				t.Fatal(err)
			}
			if err := RegisterOther(n, h); err != nil {
				t.Fatal(err)
			}

			g, err := gate.New(gate.Config{
				ID: "generated",
				Entries: []gate.Entry{
					{
						Network:    "tcp",
						Address:    "127.0.0.1:0",
						Service:    EchoService,
						Codec:      codec,
						LoginRoute: EchoLoginRoute,
					},
					{
						Network:    "tcp",
						Address:    "127.0.0.1:0",
						Service:    OtherService,
						Codec:      codec,
						LoginRoute: OtherLoginRoute,
					},
				},
			}, gate.WithSelector(gate.Local(n)))
			if err != nil {
				t.Fatal(err)
			}
			n.SetPusher(g)

			done := make(chan error, 1)
			go func() {
				done <- g.Start(t.Context())
			}()
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := g.Stop(ctx); err != nil {
					t.Error(err)
				}
				if err := <-done; err != nil {
					t.Error(err)
				}
				if err := n.Stop(ctx); err != nil {
					t.Error(err)
				}
			}()
			select {
			case <-g.Ready():
			case <-time.After(time.Second):
				t.Fatal("startup timeout")
			}

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			a, err := client.Dial(ctx, client.Config{
				Network: "tcp",
				Address: g.Addresses()[0],
				Codec:   codec,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := client.Dial(ctx, client.Config{
				Network: "tcp",
				Address: g.Addresses()[1],
				Codec:   codec,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()

			first, second := NewEchoPandaClient(a), NewOtherPandaClient(b)
			if _, err := first.Login(ctx, wrapperspb.String("")); err != nil {
				t.Fatal(err)
			}
			if _, err := second.Login(ctx, wrapperspb.String("")); err != nil {
				t.Fatal(err)
			}
			if _, err := first.Update(ctx, wrapperspb.String("one")); err != nil {
				t.Fatal(err)
			}
			if _, err := second.Update(ctx, wrapperspb.String("two")); err != nil {
				t.Fatal(err)
			}
			targetA, targetB := <-h.sessions, <-h.sessions

			messages := make(chan string, 2)
			first.OnUpdate(func(_ context.Context, v *Update) error {
				messages <- v.Value
				return nil
			})
			second.OnSecond(func(_ context.Context, v *Second) error {
				messages <- v.Value
				return nil
			})
			if err := PushEchoUpdate(ctx, n, targetA, &Update{Value: "one"}); err != nil {
				t.Fatal(err)
			}
			if err := PushOtherSecond(ctx, n, targetB, &Second{Value: "two"}); err != nil {
				t.Fatal(err)
			}

			seen := map[string]bool{}
			for range 2 {
				select {
				case value := <-messages:
					seen[value] = true
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if !seen["one"] || !seen["two"] {
				t.Fatal(seen)
			}
		})
	}
}
`
