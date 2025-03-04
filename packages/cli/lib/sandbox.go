package lib

import (
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/grpc/orchestrator"
)

func PrintSandboxInfo(title string, sandboxes ...*orchestrator.SandboxInfo) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)

	t.SetTitle(title)
	t.Style().Title = table.TitleOptions{Align: text.AlignCenter}
	t.AppendHeader(table.Row{"SandboxID", "TemplateID", "PrivateIP", "Pid", "State", "StartTime"})
	for _, sbx := range sandboxes {
		var (
			templateID string = "Unknown"
			privateIP  string = "Unknown"
			startTime  string = "Unknown"
			pid        uint32
		)
		if sbx.TemplateID != nil {
			templateID = *sbx.TemplateID
		}
		if sbx.PrivateIP != nil {
			privateIP = *sbx.PrivateIP
		}
		if sbx.StartTime != nil {
			startTime = sbx.StartTime.AsTime().String()
		}
		if sbx.Pid != nil {
			pid = *sbx.Pid
		}
		t.AppendRow(table.Row{sbx.SandboxID, templateID, privateIP, pid, sbx.State.String(), startTime})
	}
	t.SortBy([]table.SortBy{
		{Name: "StartTime", Mode: table.Asc},
	})
	t.Render()
}
