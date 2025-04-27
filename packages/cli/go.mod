module github.com/X-code-interpreter/sandbox-backend/packages/cli

go 1.23

require (
	github.com/X-code-interpreter/sandbox-backend/packages/shared v0.0.0
	github.com/golang/protobuf v1.5.4
	github.com/google/uuid v1.6.0
	github.com/jedib0t/go-pretty/v6 v6.6.1
	github.com/spf13/cobra v1.8.1
	google.golang.org/grpc v1.64.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-runewidth v0.0.15 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/testify v1.9.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.18.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240515191416-fc5f0ca64291 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
)

replace github.com/X-code-interpreter/sandbox-backend/packages/shared v0.0.0 => ../shared
