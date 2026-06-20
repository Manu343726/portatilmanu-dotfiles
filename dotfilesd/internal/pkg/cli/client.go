package cli

import (
	"fmt"
	"net/http"

	"dotfilesd/proto/dotfilesd/v1/dotfilesdv1/dotfilesdv1connect"
)

type Clients struct {
	Sys  dotfilesdv1connect.SystemServiceClient
	Dot  dotfilesdv1connect.DotfilesServiceClient
	Exec dotfilesdv1connect.ExecServiceClient
	Cfg  dotfilesdv1connect.ConfigServiceClient
}

func NewClients(port string) *Clients {
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", port)
	return &Clients{
		Sys:  dotfilesdv1connect.NewSystemServiceClient(http.DefaultClient, baseURL),
		Dot:  dotfilesdv1connect.NewDotfilesServiceClient(http.DefaultClient, baseURL),
		Exec: dotfilesdv1connect.NewExecServiceClient(http.DefaultClient, baseURL),
		Cfg:  dotfilesdv1connect.NewConfigServiceClient(http.DefaultClient, baseURL),
	}
}
