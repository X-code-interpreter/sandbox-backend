package build

import (
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/config"
)

type Server struct {
	ID              string          `toml:"id"`
	Test            int             `toml:"test"`
	Subnet          config.IPNet    `toml:"subnet"`
	RootfsBuildMode RootfsBuildMode `toml:"rootfs_build_mode"`
}

type Template struct {
	Name string `toml:"name"`
}

func TestTomlParse(t *testing.T) {
	var (
		content = `
test = 5
[dummy]
some = "dummy"
text = 5
[server]
id = "fc-test"
test = 6
subnet = "10.160.0.0/16"
rootfs_build_mode="normal"
[template."fc-test"]
name = "good"
`
		globalConfig struct {
			Test      int                       `toml:"test"`
			S         toml.Primitive            `toml:"server"`
			Templates map[string]toml.Primitive `toml:"template"`
		}
		server Server
	)
	meta, err := toml.Decode(content, &globalConfig)
	if err != nil {
		t.Errorf("decode global failed %s", err.Error())
	}
	if err = meta.PrimitiveDecode(globalConfig.S, &server); err != nil {
		t.Errorf("decode server failed %s", err.Error())
	}
	if server.ID != "fc-test" {
		t.Errorf("server id should be fc-test, got %s", server.ID)
	}
	if server.Test != 6 {
		t.Errorf("server test should be 6, got %d", server.Test)
	}

	if p, ok := globalConfig.Templates["fc-test"]; !ok {
		t.Errorf("template fc-test not found")
	} else {
		var template Template
		if err := meta.PrimitiveDecode(p, &template); err != nil {
			t.Errorf("decode template failed %s", err.Error())
		}
		if template.Name != "good" {
			t.Errorf("template name should be good, %+v", template)
		}
	}
	t.Logf("global test: %d, server: %+v", globalConfig.Test, server)
}
