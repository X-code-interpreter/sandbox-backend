package lib

import (
	"os"
	"time"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

func PrintSandboxInfo(title string, sandboxes ...*orchestrator.SandboxInfo) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)

	t.SetTitle(title)
	t.Style().Title = table.TitleOptions{Align: text.AlignCenter}
	t.AppendHeader(table.Row{"SandboxID", "TemplateID", "PrivateIP", "StartTime"})
	for _, sbx := range sandboxes {
		var (
			templateID string
			privateIP  string
			startTime  time.Time
		)
		if sbx.TemplateID != nil {
			templateID = *sbx.TemplateID
		}
		if sbx.PrivateIP != nil {
			privateIP = *sbx.PrivateIP
		}
		if sbx.StartTime != nil {
			startTime = sbx.StartTime.AsTime()
		}
		t.AppendRow(table.Row{sbx.SandboxID, templateID, privateIP, startTime})
	}
	t.Render()
}
