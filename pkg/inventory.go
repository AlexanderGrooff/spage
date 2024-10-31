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
	Name    string
	Host    string `yaml:"host"`
	Vars    map[string]interface{}
	Groups  map[string]string `yaml:"groups"`
	IsLocal bool
}

func (h Host) String() string {
	return h.Name
}

type Group struct {
	Hosts map[string]Host        `yaml:"hosts"`
	Vars  map[string]interface{} `yaml:"vars"`
}

func LoadInventory(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Error reading YAML file: %v", err)
	}
	var inventory Inventory
	inventory.Hosts = make(map[string]Host)
	inventory.Groups = make(map[string]Group)
	err = yaml.Unmarshal(data, &inventory)
	if err != nil {
		return nil, err
	}
	// Add hosts from groups to inventory
	for _, group := range inventory.Groups {
		for name, host := range group.Hosts {
			host.Name = name
			inventory.Hosts[name] = host
		}
	}
	for name, host := range inventory.Hosts {
		host.Name = name
		DebugOutput("Setting name for host %q to %q", name, host.Name)
		inventory.Hosts[name] = host
	}
	return &inventory, nil
}

func (i Inventory) GetContextForHost(host Host) (*HostContext, error) {
	facts := make(Facts)
	// Apply vars in order of precedence: global, group, host
	for k, v := range i.Vars {
		facts[k] = v.(ModuleOutput)
	}
	for _, groupName := range i.Hosts[host.Name].Groups {
		group, ok := i.Groups[groupName]
		if !ok {
			return nil, fmt.Errorf("could not find group %s in inventory", groupName)
		}
		for k, v := range group.Vars {
			facts[k] = v.(ModuleOutput)
		}
	}
	for k, v := range i.Hosts[host.Name].Vars {
		facts[k] = v.(ModuleOutput)
	}
	// TODO: also compare hostnames? Or even CLI flag?
	if host.Host == "localhost" {
		host.IsLocal = true
	}
	return &HostContext{Facts: facts, Host: host, History: make(map[string]ModuleOutput)}, nil
}

func (i Inventory) GetContextForRun() (map[string]*HostContext, error) {
	var err error
	contexts := make(map[string]*HostContext)
	for _, host := range i.Hosts {
		DebugOutput("Getting context for host %q", host.Name)
		contexts[host.Name], err = i.GetContextForHost(host)
		if err != nil {
			return nil, fmt.Errorf("could not get context for host '%s': %v", host, err)
		}
		// TODO: host_vars and group_vars
	}
	return contexts, nil
}
