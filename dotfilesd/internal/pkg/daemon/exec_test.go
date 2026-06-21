package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var ctx = context.Background()

var _ = Describe("execServer", func() {
	var (
		sessions *SessionStore
		server   *execServer
		mux      *http.ServeMux
		handler  http.Handler
	)

	BeforeEach(func() {
		sessions = NewSessionStore()
		server = &execServer{sessions: sessions}

		mux = http.NewServeMux()
		path, h := dotfilesdv1connect.NewExecServiceHandler(server)
		mux.Handle(path, h)
		handler = mux
	})

	Describe("Exec", func() {
		It("executes a command ephemerally when no session is provided", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Exec(ctx, connect.NewRequest(&dotfilesdv1.ExecRequest{
				Command: "echo hello_ephemeral",
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.ExitCode).To(Equal(int32(0)))
			Expect(strings.TrimSpace(resp.Msg.Stdout)).To(Equal("hello_ephemeral"))
			Expect(resp.Msg.Stderr).To(BeEmpty())
		})

		It("executes within a session when session ID is provided", func() {
			s := sessions.Create()
			s.SetVariables(map[string]string{"SESSION_VAR": "from_session"})

			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, srv.URL)

			resp, err := client.Exec(ctx, connect.NewRequest(&dotfilesdv1.ExecRequest{
				Command: "echo \"hello $SESSION_VAR\"",
				Session: &dotfilesdv1.Session{Id: s.id},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.ExitCode).To(Equal(int32(0)))
			Expect(strings.TrimSpace(resp.Msg.Stdout)).To(Equal("hello from_session"))

			resp, err = client.Exec(ctx, connect.NewRequest(&dotfilesdv1.ExecRequest{
				Command: "echo \"$SESSION_VAR\"",
				Session: &dotfilesdv1.Session{Id: s.id},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.ExitCode).To(Equal(int32(0)))
			Expect(strings.TrimSpace(resp.Msg.Stdout)).To(Equal("from_session"))
		})

		It("receives variables through the Session message", func() {
			s := sessions.Create()

			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Exec(ctx, connect.NewRequest(&dotfilesdv1.ExecRequest{
				Command: "echo \"$NEW_VAR\"",
				Session: &dotfilesdv1.Session{
					Id:        s.id,
					Variables: map[string]string{"NEW_VAR": "via_session"},
				},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.ExitCode).To(Equal(int32(0)))
			Expect(strings.TrimSpace(resp.Msg.Stdout)).To(Equal("via_session"))
		})

		It("degrades finalized session to ephemeral execution", func() {
			s := sessions.Create()
			sessions.Finalize(s.id)

			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Exec(ctx, connect.NewRequest(&dotfilesdv1.ExecRequest{
				Command: "echo test",
				Session: &dotfilesdv1.Session{Id: s.id},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.ExitCode).To(Equal(int32(0)))
		})
	})

	Describe("SudoExec", func() {
		It("issues an auth challenge when no password or method is provided", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.SudoExec(ctx, connect.NewRequest(&dotfilesdv1.SudoExecRequest{
				Command: "whoami",
			}))
			Expect(err).To(Succeed())
			challenge := resp.Msg.GetAuthChallenge()
			Expect(challenge).ToNot(BeNil())
			Expect(challenge.Methods).ToNot(BeEmpty())
		})

		It("returns a result when password is provided", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.SudoExec(ctx, connect.NewRequest(&dotfilesdv1.SudoExecRequest{
				Command:  "echo sudo_test",
				Password: "wrong_password",
			}))
			Expect(err).To(Succeed())
			result := resp.Msg.GetResult()
			Expect(result).ToNot(BeNil())
			Expect(result.AuthCancelled).To(BeFalse())
		})
	})
})

var _ = Describe("ExecRaw (internal)", func() {
	var (
		sessions *SessionStore
		server   *execServer
	)

	BeforeEach(func() {
		sessions = NewSessionStore()
		server = &execServer{sessions: sessions}
	})

	It("executes a command without session or variable injection", func() {
		resp, err := server.ExecRaw(ctx, "echo raw_output", false)
		Expect(err).To(Succeed())
		Expect(resp.Msg.ExitCode).To(Equal(int32(0)))
		Expect(strings.TrimSpace(resp.Msg.Stdout)).To(Equal("raw_output"))
	})

	It("reports non-zero exit codes", func() {
		resp, err := server.ExecRaw(ctx, "exit 99", false)
		Expect(err).To(Succeed())
		Expect(resp.Msg.ExitCode).To(Equal(int32(99)))
	})
})
