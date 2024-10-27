package pkg

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"log"
	"os"
)

type Inventory struct {
	Hosts  map[string]Host        `yaml:"all"`
	Vars   map[string]interface{} `yaml:"vars"`
	Groups map[string]Group       `yaml:"groups"`
}

type Host struct {
	Host   string `yaml:"host"`
	Vars   map[string]interface{}
	Groups []string `yaml:"groups"`
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
	fmt.Printf("Loaded Inventory:\n%v\n", inventory)
	return &inventory, nil
}

func (i Inventory) GetContextForHost(host string) (map[string]interface{}, error) {
	context := make(map[string]interface{})
	// Apply vars in order of precedence: global, group, host
	for k, v := range i.Vars {
		context[k] = v
	}
	for _, groupName := range i.Hosts[host].Groups {
		group, ok := i.Groups[groupName]
		if !ok {
			return nil, fmt.Errorf("could not find group %s in inventory", groupName)
		}
		for k, v := range group.Vars {
			context[k] = v
		}
	}
	for k, v := range i.Hosts[host].Vars {
		context[k] = v
	}
	return context, nil
}

func (i Inventory) GetContextForRun() (map[string]Context, error) {
	var err error
	contexts := make(map[string]Context)
	for hostname := range i.Hosts {
		contexts[hostname], err = i.GetContextForHost(hostname)
		if err != nil {
			return nil, fmt.Errorf("could not get context for host '%s': %v", hostname, err)
		}
		// TODO: host_vars and group_vars
	}
	return contexts, nil
}
