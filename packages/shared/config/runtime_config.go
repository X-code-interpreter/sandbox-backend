package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/utils"
)

type IP struct {
	net.IP
}

func (ip *IP) UnmarshalText(text []byte) error {
	if tmp := net.ParseIP(string(text)); tmp != nil {
		ip.IP = tmp
		return nil
	}
	return fmt.Errorf("invalid IP address: %s", string(text))
}

// IPNET is a wrapper around net.IPNet to implement custom unmarshalling for toml.
type IPNet struct {
	*net.IPNet
}

func (ipn *IPNet) UnmarshalText(text []byte) error {
	_, network, err := net.ParseCIDR(string(text))
	if err != nil {
		return err
	}
	ipn.IPNet = network
	return nil
}

type CommonConfig struct {
	FCBinaryPath string `toml:"fc_binary_path"`
	CHBinaryPath string `toml:"ch_binary_path"`
	DataRoot     string `toml:"data_root"`
}

func GetConfigFilePath() (configFile string, err error) {
	var homeDir string
	configFile = "./config.toml"
	if utils.CheckFileExists(configFile) {
		return
	}
	if homeDir, err = os.UserHomeDir(); err != nil {
		return
	}
	configFile = filepath.Join(homeDir, "orchestrator", "config.toml")
	if utils.CheckFileExists(configFile) {
		return
	}
	configFile = "/etc/orchestrator/config.toml"
	if utils.CheckFileExists(configFile) {
		return
	}
	err = fmt.Errorf("no valid config exists")
	return
}
