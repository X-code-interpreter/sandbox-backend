package build

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/X-code-interpreter/sandbox-backend/packages/template-manager/constants"
)

func TestProvision(t *testing.T) {
	var (
		scriptDef bytes.Buffer
		cfg       TemplateManagerConfig
	)
	templatePath := "../mini-agent.json"
	content, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal("read template file err", err)
	}
	if err = json.Unmarshal(content, &cfg); err != nil {
		t.Fatal("deserialize template configuration file failed: ", err)
	}

	err = EnvInstanceTemplate.Execute(&scriptDef, struct {
		TemplateID          string
		StartCmd            string
		StartCmdEnvFilePath string
	}{
		TemplateID:          cfg.TemplateID,
		StartCmd:            strings.ReplaceAll(cfg.StartCmd.Cmd, "\"", "\\\""),
		StartCmdEnvFilePath: constants.StartCmdEnvFilePath,
	})
	if err != nil {
		t.Fatal("error executing provision script: %w", err)
	}
	t.Log(scriptDef.String())
}
