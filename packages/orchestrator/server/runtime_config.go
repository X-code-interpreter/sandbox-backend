package server

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/X-code-interpreter/sandbox-backend/packages/orchestrator/constants"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/config"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
)

type OrchestratorConfig struct {
	Port       int          `toml:"port"`
	Host       config.IP    `toml:"host"`
	Subnet     config.IPNet `toml:"subnet"`
	CgroupName string       `toml:"cgroup_name"`

	DataRoot     string `toml:"-"`
	FCBinaryPath string `toml:"-"`
	CHBinaryPath string `toml:"-"`
}

func (cfg *OrchestratorConfig) Validate() error {
	if cfg.DataRoot == "" {
		return fmt.Errorf("data_root cannot be empty")
	}
	var fcExists, chExists bool
	if _, err := exec.LookPath(cfg.FCBinaryPath); err == nil {
		fcExists = true
	}
	if _, err := exec.LookPath(cfg.CHBinaryPath); err == nil {
		chExists = true
	}
	if !fcExists && !chExists {
		return fmt.Errorf("neither firecracker nor cloud-hypervisor binary found")
	}
	return nil
}

func (cfg *OrchestratorConfig) setDefaultVal() {
	if cfg.Port == 0 {
		cfg.Port = consts.DefaultOrchestratorPort
	}
	if cfg.Host.IP == nil {
		cfg.Host.IP = net.ParseIP("0.0.0.0")
	}
	if cfg.Subnet.IPNet == nil {
		cfg.Subnet.IPNet = &net.IPNet{
			IP:   net.ParseIP("10.168.0.0"),
			Mask: net.CIDRMask(16, 32),
		}
	}
	if cfg.CgroupName == "" {
		cfg.CgroupName = consts.DefaultCgroupName
	}
	if cfg.FCBinaryPath == "" {
		cfg.FCBinaryPath = constants.FcBinaryName
	}
	if cfg.CHBinaryPath == "" {
		cfg.CHBinaryPath = constants.ChBinaryName
	}
}

func createSandboxCgroup(path string) error {
	if err := utils.CreateDirAllIfNotExists(path, 0o755); err != nil {
		return err
	}
	// enable all controllers in controllers into subtree_control
	b, err := os.ReadFile(filepath.Join(path, "cgroup.controllers"))
	if err != nil {
		panic(fmt.Errorf("read cgroup.controllers in %s failed: %w", path, err))
	}
	controllers := strings.Fields(string(b))
	for idx, c := range controllers {
		controllers[idx] = "+" + c
	}
	f, err := os.OpenFile(filepath.Join(path, "cgroup.subtree_control"), os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open cgroup.subtree_control in %s failed: %w", path, err)
	}
	defer f.Close()
	enableRequest := strings.Join(controllers, " ")
	if _, err := f.WriteString(enableRequest); err != nil {
		return fmt.Errorf("write %s to cgroup.subtree_control in %s failed: %w", enableRequest, path, err)
	}
	return nil
}

func (cfg *OrchestratorConfig) initialize() error {
	path := filepath.Join(consts.CgroupfsPath, cfg.CgroupName)
	if err := createSandboxCgroup(path); err != nil {
		return err
	}
	return nil
}

// PraseConfig parses the configuration file for the orchestrator.
//
// @configFile: the path to the configuration file.
func ParseConfig(configFile string) (*OrchestratorConfig, error) {
	var (
		err          error
		cfg          OrchestratorConfig
		globalConfig struct {
			config.CommonConfig
			Orchestrator toml.Primitive `toml:"orchestrator"`
		}
	)

	if len(configFile) == 0 {
		// if no config file is provided, use the default config path
		configFile, err = config.GetConfigFilePath()
		if err != nil {
			return nil, err
		}
	}
	meta, err := toml.DecodeFile(configFile, &globalConfig)
	if err != nil {
		return nil, err
	}
	if err = meta.PrimitiveDecode(globalConfig.Orchestrator, &cfg); err != nil {
		return nil, err
	}
	cfg.DataRoot = globalConfig.CommonConfig.DataRoot
	cfg.FCBinaryPath = globalConfig.CommonConfig.FCBinaryPath
	cfg.CHBinaryPath = globalConfig.CommonConfig.CHBinaryPath

	cfg.setDefaultVal()
	if err = cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
