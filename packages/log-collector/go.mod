module github.com/X-code-interpreter/sandbox-backend/packages/log-collector

go 1.21.0

require (
	github.com/X-code-interpreter/sandbox-backend/packages/shared v0.0.0
	github.com/rs/zerolog v1.33.0
)

require (
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	golang.org/x/sys v0.20.0 // indirect
)

replace github.com/X-code-interpreter/sandbox-backend/packages/shared v0.0.0 => ../shared
