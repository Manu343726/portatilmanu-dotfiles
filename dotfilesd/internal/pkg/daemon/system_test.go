package daemon

import (
	"net/http"
	"net/http/httptest"
	"time"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1"
	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"

	"connectrpc.com/connect"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("systemServer", func() {
	var (
		sessions *SessionStore
		server   *systemServer
		mux      *http.ServeMux
		handler  http.Handler
	)

	BeforeEach(func() {
		sessions = NewSessionStore()
		server = &systemServer{startedAt: time.Now(), sessions: sessions}

		mux = http.NewServeMux()
		path, h := dotfilesdv1connect.NewSystemServiceHandler(server)
		mux.Handle(path, h)
		handler = mux
	})

	Describe("Ping", func() {
		It("returns version and uptime", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSystemServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Ping(ctx, connect.NewRequest(&dotfilesdv1.PingRequest{}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Version).To(Equal("0.1.0"))
			Expect(resp.Msg.Pid).To(BeNumerically(">", 0))
			Expect(resp.Msg.UptimeSecs).To(BeNumerically(">=", 0))
		})
	})

	Describe("SystemInfo", func() {
		It("returns system information", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSystemServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.SystemInfo(ctx, connect.NewRequest(&dotfilesdv1.SystemInfoRequest{}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Os).To(Equal("linux"))
			Expect(resp.Msg.Kernel).ToNot(BeEmpty())
			Expect(resp.Msg.Shell).ToNot(BeEmpty())
		})
	})

	Describe("SudoMethods", func() {
		It("returns available sudo methods", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSystemServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.SudoMethods(ctx, connect.NewRequest(&dotfilesdv1.SudoMethodsRequest{}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.HasElevation).To(BeTrue())
			Expect(resp.Msg.AvailableMethods).ToNot(BeEmpty())
		})
	})
})

var _ = Describe("dotfilesServer", func() {
	var (
		sessions *SessionStore
		server   *dotfilesServer
		mux      *http.ServeMux
		handler  http.Handler
	)

	BeforeEach(func() {
		sessions = NewSessionStore()
		server = &dotfilesServer{sessions: sessions}

		mux = http.NewServeMux()
		path, h := dotfilesdv1connect.NewDotfilesServiceHandler(server)
		mux.Handle(path, h)
		handler = mux
	})

	Describe("Status", func() {
		It("returns dotfiles status", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Status(ctx, connect.NewRequest(&dotfilesdv1.StatusRequest{}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Hostname).ToNot(BeEmpty())
			Expect(resp.Msg.Uptime).ToNot(BeEmpty())
		})
	})

	Describe("Git", func() {
		It("returns status for STATUS action", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Git(ctx, connect.NewRequest(&dotfilesdv1.GitRequest{
				Action: dotfilesdv1.GitAction_GIT_ACTION_STATUS,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.ExitCode).To(Equal(int32(0)))
			Expect(resp.Msg.Stdout).ToNot(BeEmpty())
		})

		It("returns log for LOG action", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Git(ctx, connect.NewRequest(&dotfilesdv1.GitRequest{
				Action: dotfilesdv1.GitAction_GIT_ACTION_LOG,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.ExitCode).To(Equal(int32(0)))
			Expect(resp.Msg.Stdout).ToNot(BeEmpty())
		})

		It("returns diff for DIFF action", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Git(ctx, connect.NewRequest(&dotfilesdv1.GitRequest{
				Action: dotfilesdv1.GitAction_GIT_ACTION_DIFF,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.ExitCode).To(Equal(int32(0)))
		})

		It("returns error for unknown action", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Git(ctx, connect.NewRequest(&dotfilesdv1.GitRequest{
				Action: dotfilesdv1.GitAction_GIT_ACTION_UNSPECIFIED,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.ExitCode).To(Equal(int32(1)))
			Expect(resp.Msg.Stderr).To(ContainSubstring("unknown"))
		})
	})
})

var _ = Describe("sessionServer", func() {
	var (
		store   *SessionStore
		server  *sessionServer
		mux     *http.ServeMux
		handler http.Handler
	)

	BeforeEach(func() {
		store = NewSessionStore()
		server = newSessionServer(store)

		mux = http.NewServeMux()
		path, h := dotfilesdv1connect.NewSessionServiceHandler(server)
		mux.Handle(path, h)
		handler = mux
	})

	Describe("CreateSession", func() {
		It("creates a new session and returns it", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.CreateSession(ctx, connect.NewRequest(&dotfilesdv1.CreateSessionRequest{}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Session).ToNot(BeNil())
			Expect(resp.Msg.Session.Id).ToNot(BeEmpty())
			Expect(resp.Msg.Session.Finalized).To(BeFalse())

			// Verify it's actually in the store
			s := store.Get(resp.Msg.Session.Id)
			Expect(s).ToNot(BeNil())
		})
	})

	Describe("Connect", func() {
		It("creates a new session when no ID is provided", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Connect(ctx, connect.NewRequest(&dotfilesdv1.ConnectRequest{
				CallbackUrl: "http://127.0.0.1:9999",
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Session).ToNot(BeNil())
			Expect(resp.Msg.Session.Id).ToNot(BeEmpty())
		})

		It("connects to an existing session", func() {
			existing := store.Create()

			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Connect(ctx, connect.NewRequest(&dotfilesdv1.ConnectRequest{
				Session: &dotfilesdv1.Session{Id: existing.id},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Session.Id).To(Equal(existing.id))
		})

		It("returns error when session does not exist", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			_, err := client.Connect(ctx, connect.NewRequest(&dotfilesdv1.ConnectRequest{
				Session: &dotfilesdv1.Session{Id: "nonexistent"},
			}))
			Expect(err).To(HaveOccurred())
		})

		It("sets variables from the Connect request", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Connect(ctx, connect.NewRequest(&dotfilesdv1.ConnectRequest{
				Session: &dotfilesdv1.Session{
					Variables: map[string]string{"K": "v"},
				},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Session.Variables["K"]).To(Equal("v"))
		})
	})

	Describe("FinalizeSession", func() {
		It("finalizes an existing session", func() {
			existing := store.Create()

			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.FinalizeSession(ctx, connect.NewRequest(&dotfilesdv1.FinalizeSessionRequest{
				Session: &dotfilesdv1.Session{Id: existing.id},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Success).To(BeTrue())

			// Verify it's finalized in store
			Expect(existing.finalized).To(BeTrue())
		})

		It("returns false for unknown session", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.FinalizeSession(ctx, connect.NewRequest(&dotfilesdv1.FinalizeSessionRequest{
				Session: &dotfilesdv1.Session{Id: "nonexistent"},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Success).To(BeFalse())
		})
	})

	Describe("GetSession", func() {
		It("returns an existing session", func() {
			existing := store.Create()
			existing.SetVariables(map[string]string{"KEY": "val"})

			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.GetSession(ctx, connect.NewRequest(&dotfilesdv1.GetSessionRequest{
				Session: &dotfilesdv1.Session{Id: existing.id},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Session).ToNot(BeNil())
			Expect(resp.Msg.Session.Id).To(Equal(existing.id))
			Expect(resp.Msg.Session.Variables["KEY"]).To(Equal("val"))
		})

		It("returns empty for unknown session", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.GetSession(ctx, connect.NewRequest(&dotfilesdv1.GetSessionRequest{
				Session: &dotfilesdv1.Session{Id: "nonexistent"},
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Session).To(BeNil())
		})
	})

	Describe("ListSessions", func() {
		It("lists active sessions", func() {
			s1 := store.Create()
			s2 := store.Create()
			store.Finalize(s2.id)

			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.ListSessions(ctx, connect.NewRequest(&dotfilesdv1.ListSessionsRequest{}))
			Expect(err).To(Succeed())

			// Only non-finalized sessions should be listed
			ids := []string{}
			for _, s := range resp.Msg.Sessions {
				ids = append(ids, s.Id)
			}
			Expect(ids).To(ContainElement(s1.id))
			Expect(ids).NotTo(ContainElement(s2.id))
		})

		It("returns empty list when no active sessions", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewSessionServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.ListSessions(ctx, connect.NewRequest(&dotfilesdv1.ListSessionsRequest{}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Sessions).To(BeEmpty())
		})
	})
})
