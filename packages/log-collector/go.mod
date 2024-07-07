module github.com/X-code-interpreter/sandbox-backend/packages/log-collector

go 1.21.0

require (
	github.com/X-code-interpreter/sandbox-backend/packages/shared v0.0.0
	go.uber.org/zap v1.27.0
)

require go.uber.org/multierr v1.10.0 // indirect

replace github.com/X-code-interpreter/sandbox-backend/packages/shared v0.0.0 => ../shared
