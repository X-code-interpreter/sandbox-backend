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
		env       Env
	)
	templatePath := "../mini-agent.json"
	content, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal("read template file err", err)
	}
	if err = json.Unmarshal(content, &env); err != nil {
		t.Fatal("deserialize template configuration file failed: ", err)
	}

	err = EnvInstanceTemplate.Execute(&scriptDef, struct {
		EnvID               string
		StartCmd            string
		StartCmdEnvFilePath string
	}{
		EnvID:               env.EnvID,
		StartCmd:            strings.ReplaceAll(env.StartCmd, "\"", "\\\""),
		StartCmdEnvFilePath: constants.StartCmdEnvFilePath,
	})
	if err != nil {
		t.Fatal("error executing provision script: %w", err)
	}
  t.Log(scriptDef.String())
}
