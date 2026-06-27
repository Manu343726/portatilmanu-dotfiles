module plugins/tmuxbar

go 1.26.3

replace (
	dotfilesd => /home/manu343726/dotfilesd
	plugins/resources => ../resources
)

require (
	connectrpc.com/connect v1.20.0
	dotfilesd v0.0.0
	google.golang.org/protobuf v1.36.11
	plugins/resources v0.0.0
)

require (
	connectrpc.com/grpcreflect v1.3.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
