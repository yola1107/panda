package echo_test

import (
	"context"
	"strconv"
	"sync"
	"testing"

	echov1 "github.com/yola1107/panda/api/echo/v1"
	"github.com/yola1107/panda/gate"

	kerrors "github.com/go-kratos/kratos/v3/errors"
)

func TestInteractions(t *testing.T) {
	t.Parallel()
	matrix(t, func(t *testing.T, mode deployment) {
		t.Helper()
		h := start(t, mode, settings{entry: gate.Entry{MaxInFlight: 64}})
		c, api := h.dial(t)

		ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
		defer cancel()

		_, err := api.Say(ctx, &echov1.SayRequest{})
		reason(t, err, "UNAUTHENTICATED")
		_, err = api.Login(ctx, &echov1.LoginRequest{Token: "invalid"})
		reason(t, err, "INVALID_TOKEN")
		_, err = api.Say(ctx, &echov1.SayRequest{})
		reason(t, err, "UNAUTHENTICATED")
		login(t, api)
		checkConcurrentRequests(t, api)
		_, err = api.Say(ctx, &echov1.SayRequest{Text: "error"})
		reason(t, err, "DEMO_ERROR")

		if kerrors.FromError(err).Metadata["field"] != "text" {
			t.Fatal("error metadata lost")
		}

		updates := make(chan string, 1)

		api.OnUpdate(func(_ context.Context, update *echov1.Update) error {
			updates <- update.Text
			return nil
		})

		if err := api.Report(ctx, &echov1.ReportRequest{Text: "business push"}); err != nil {
			t.Fatal(err)
		}

		if got := receive(t, updates); got != "business push" {
			t.Fatal(got)
		}

		if err := c.Heartbeat(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

func checkConcurrentRequests(t *testing.T, api *echov1.EchoPandaClient) {
	t.Helper()

	var workers sync.WaitGroup
	for i := range 16 {
		workers.Go(func() {
			ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
			defer cancel()

			text := strconv.Itoa(i)

			reply, err := api.Say(ctx, &echov1.SayRequest{Text: text})
			if err != nil {
				t.Error(err)
				return
			}

			if reply.Text != text || reply.UserId != "demo-user" {
				t.Errorf("request correlation or identity lost: %v", reply)
			}
		})
	}

	workers.Wait()
}
