package sandbox

import (
	"fmt"
	"github.com/txn2/txeh"
)

type DNS struct {
	// This already hold a mutex
	*txeh.Hosts
}

func NewDNS() (*DNS, error) {
	hosts, err := txeh.NewHostsDefault()
	if err != nil {
		return nil, fmt.Errorf("error initializing etc hosts handler: %w", err)
	}

	reloadErr := hosts.Reload()
	if reloadErr != nil {
		return nil, fmt.Errorf("error reloading etc hosts: %w", reloadErr)
	}

	return &DNS{
		hosts,
	}, nil
}

// ip: for example 10.5.8.2
func (d *DNS) Add(ip, instanceID string) error {
	d.AddHost(ip, instanceID)

	err := d.Save()
	if err != nil {
		return fmt.Errorf("error adding env instance to etc hosts: %w", err)
	}

	return nil
}

func (d *DNS) Remove(instanceID string) error {
	d.RemoveHost(instanceID)

	err := d.Save()
	if err != nil {
		return fmt.Errorf("error removing env instance to etc hosts: %w", err)
	}

	return nil
}
