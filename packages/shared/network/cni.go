package network

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
	"github.com/firecracker-microvm/firecracker-go-sdk"
)

func writeCNIConfWithHostLocalSubnet(path, networkName, subnet string) error {
	return os.WriteFile(path, []byte(fmt.Sprintf(`{
		"cniVersion": "0.4.0",
		"name": "%s",
		"plugins": [
		  {
			"type": "ptp",
			"ipam": {
			  "type": "host-local",
			  "subnet": "%s"
			}
		  },
		  {
			"type": "firewall"
		  },
		  {
			"type": "tc-redirect-tap"
		  }
		]
	 }`, networkName, subnet)), 0644)
}

func GetDefaultCNINetworkConfig() (firecracker.NetworkInterface, error) {
	// Network config
	cniConfPath := filepath.Join(consts.CNIConfigDir, fmt.Sprintf("%s.conflist", consts.CNINetworkName))

	if _, err := os.Stat(cniConfPath); err != nil {
		if !os.IsNotExist(err) {
			return firecracker.NetworkInterface{}, fmt.Errorf("error when stat %s: %w", cniConfPath, err)
		}
		err = writeCNIConfWithHostLocalSubnet(cniConfPath, consts.CNINetworkName, consts.Subnet)
		if err != nil {
			return firecracker.NetworkInterface{}, fmt.Errorf("error when write cni config list: %w", err)
		}
	}

	networkInterface := firecracker.NetworkInterface{
		CNIConfiguration: &firecracker.CNIConfiguration{
			NetworkName: consts.CNINetworkName,
			IfName:      consts.IfName,
			ConfDir:     consts.CNIConfigDir,
			BinPath:     []string{consts.CNIBinDir},
			VMIfName:    "eth0",
		},
		AllowMMDS: true,
	}

	return networkInterface, nil
}
