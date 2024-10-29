package pkg

import (
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Inventory struct {
	Hosts  map[string]Host        `yaml:"all"`
	Vars   map[string]interface{} `yaml:"vars"`
	Groups map[string]Group       `yaml:"groups"`
}

type Host struct {
	Host    string `yaml:"host"`
	Vars    map[string]interface{}
	Groups  []string `yaml:"groups"`
	IsLocal bool
}

type Group struct {
	Hosts []Host                 `yaml:"hosts"`
	Vars  map[string]interface{} `yaml:"vars"`
}

func LoadInventory(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Error reading YAML file: %v", err)
	}
	var inventory Inventory
	err = yaml.Unmarshal(data, &inventory)
	if err != nil {
		return nil, err
	}
	return &inventory, nil
}

func (i Inventory) GetContextForHost(host Host) (Context, error) {
	facts := make(map[string]interface{})
	// Apply vars in order of precedence: global, group, host
	for k, v := range i.Vars {
		facts[k] = v
	}
	for _, groupName := range i.Hosts[host.Host].Groups {
		group, ok := i.Groups[groupName]
		if !ok {
			return Context{}, fmt.Errorf("could not find group %s in inventory", groupName)
		}
		for k, v := range group.Vars {
			facts[k] = v
		}
	}
	for k, v := range i.Hosts[host.Host].Vars {
		facts[k] = v
	}
	// TODO: also compare hostnames? Or even CLI flag?
	if host.Host == "localhost" {
		host.IsLocal = true
	}
	return Context{Facts: facts, Host: host}, nil
}

func (i Inventory) GetContextForRun() (map[string]Context, error) {
	var err error
	contexts := make(map[string]Context)
	for hostname, host := range i.Hosts {
		contexts[hostname], err = i.GetContextForHost(host)
		if err != nil {
			return nil, fmt.Errorf("could not get context for host '%s': %v", hostname, err)
		}
		// TODO: host_vars and group_vars
	}
	return contexts, nil
}
