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

var _ = Describe("configServer", func() {
	var (
		sessions   *SessionStore
		server     *configServer
		mux        *http.ServeMux
		handler    http.Handler
		savedRestart func(time.Duration)
	)

	BeforeEach(func() {
		// Prevent gracefulRestart from exec'ing the test binary.
		savedRestart = restartDaemon
		restartDaemon = func(time.Duration) {}

		sessions = NewSessionStore()
		server = &configServer{sessions: sessions}

		mux = http.NewServeMux()
		path, h := dotfilesdv1connect.NewConfigServiceHandler(server)
		mux.Handle(path, h)
		handler = mux
	})

	AfterEach(func() {
		restartDaemon = savedRestart
	})

	Describe("Reload", func() {
		It("reloads tmux config", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Reload(ctx, connect.NewRequest(&dotfilesdv1.ReloadRequest{
				Target: dotfilesdv1.ReloadTarget_RELOAD_TARGET_TMUX,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Results).To(HaveLen(1))
			Expect(resp.Msg.Results[0].Target).To(Equal("tmux"))
			// tmux may or may not be running — success/failure depends on context
			// We just verify the plumbing works
		})

		It("reloads i3 config", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Reload(ctx, connect.NewRequest(&dotfilesdv1.ReloadRequest{
				Target: dotfilesdv1.ReloadTarget_RELOAD_TARGET_I3,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Results).To(HaveLen(1))
			Expect(resp.Msg.Results[0].Target).To(Equal("i3"))
		})

		It("reloads kitty config", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Reload(ctx, connect.NewRequest(&dotfilesdv1.ReloadRequest{
				Target: dotfilesdv1.ReloadTarget_RELOAD_TARGET_KITTY,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Results).To(HaveLen(1))
			Expect(resp.Msg.Results[0].Target).To(Equal("kitty"))
		})

		It("reloads all configs", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Reload(ctx, connect.NewRequest(&dotfilesdv1.ReloadRequest{
				Target: dotfilesdv1.ReloadTarget_RELOAD_TARGET_ALL,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Results).To(HaveLen(3))
			targets := []string{}
			for _, r := range resp.Msg.Results {
				targets = append(targets, r.Target)
			}
			Expect(targets).To(ConsistOf("tmux", "i3", "kitty"))
		})

		It("returns error for unknown target", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Reload(ctx, connect.NewRequest(&dotfilesdv1.ReloadRequest{
				Target: dotfilesdv1.ReloadTarget_RELOAD_TARGET_UNSPECIFIED,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Results).To(HaveLen(1))
			Expect(resp.Msg.Results[0].Success).To(BeFalse())
		})
	})

	Describe("Reconfigure", func() {
		It("changes log level to debug", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Reconfigure(ctx, connect.NewRequest(&dotfilesdv1.ReconfigureRequest{
				LogLevel: dotfilesdv1.LogLevel_LOG_LEVEL_DEBUG,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Success).To(BeTrue())
			Expect(resp.Msg.Message).To(ContainSubstring("LOG_LEVEL_DEBUG"))
		})

		It("changes log level to info", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Reconfigure(ctx, connect.NewRequest(&dotfilesdv1.ReconfigureRequest{
				LogLevel: dotfilesdv1.LogLevel_LOG_LEVEL_INFO,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Success).To(BeTrue())
			Expect(resp.Msg.Message).To(ContainSubstring("LOG_LEVEL_INFO"))
		})

		It("returns error for unspecified log level", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Reconfigure(ctx, connect.NewRequest(&dotfilesdv1.ReconfigureRequest{
				LogLevel: dotfilesdv1.LogLevel_LOG_LEVEL_UNSPECIFIED,
			}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Success).To(BeFalse())
		})
	})

	Describe("Restart", func() {
		It("returns restart message", func() {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, srv.URL)
			resp, err := client.Restart(ctx, connect.NewRequest(&dotfilesdv1.RestartRequest{}))
			Expect(err).To(Succeed())
			Expect(resp.Msg.Message).To(ContainSubstring("restarting"))
		})
	})
})
