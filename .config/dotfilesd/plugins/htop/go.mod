module plugins/htop

go 1.26.3

replace dotfilesd => /home/manu343726/dotfilesd

require (
	connectrpc.com/connect v1.20.0
	dotfilesd v0.0.0-00010101000000-000000000000
	github.com/gdamore/tcell/v2 v2.13.10
	github.com/rivo/tview v0.42.0
	google.golang.org/protobuf v1.36.11
	plugins/resources v0.0.0
)

require (
	connectrpc.com/grpcreflect v1.3.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/net v0.54.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/term v0.44.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace plugins/resources => ../resources
