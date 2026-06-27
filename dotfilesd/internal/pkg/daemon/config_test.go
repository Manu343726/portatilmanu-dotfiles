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
		sessions     *SessionStore
		server       *configServer
		mux          *http.ServeMux
		handler      http.Handler
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
