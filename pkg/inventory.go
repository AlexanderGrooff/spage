package pkg

import (
	"fmt"
	"log"
	"os"

	"github.com/AlexanderGrooff/spage/pkg/common"

	"gopkg.in/yaml.v3"
)

type Inventory struct {
	Hosts  map[string]*Host       `yaml:"all"`
	Vars   map[string]interface{} `yaml:"vars"`
	Groups map[string]*Group      `yaml:"groups"`
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

func (h *Host) Prepare() {
	if h.Vars == nil {
		h.Vars = make(map[string]interface{})
	}
	if h.Groups == nil {
		h.Groups = make(map[string]string)
	}
}

type Group struct {
	Hosts map[string]*Host       `yaml:"hosts"`
	Vars  map[string]interface{} `yaml:"vars"`
}

func LoadInventory(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Error reading YAML file: %v", err)
	}
	var inventory Inventory
	inventory.Hosts = make(map[string]*Host)
	inventory.Groups = make(map[string]*Group)
	err = yaml.Unmarshal(data, &inventory)
	if err != nil {
		return nil, err
	}
	for name, host := range inventory.Hosts {
		common.DebugOutput("Adding host %q to inventory", name)
		host.Prepare()
		host.Name = name
		inventory.Hosts[name] = host
	}
	for groupName, group := range inventory.Groups {
		for name, host := range group.Hosts {
			common.DebugOutput("Found host %q in group %q", name, groupName)
			var h *Host
			if h = inventory.Hosts[name]; h != nil {
				common.DebugOutput("Host %q already in inventory", name)
			} else {
				h = host
			}
			h.Prepare()
			if h.Host == "" {
				h.Host = host.Host
			}
			if h.Name == "" {
				h.Name = name
			}

			common.DebugOutput("Adding host %q to inventory from group %q", name, groupName)

			inventory.Hosts[name] = h
			for k, v := range group.Vars {
				inventory.Hosts[name].Vars[k] = v
			}
		}
	}
	for k, v := range inventory.Vars {
		for _, host := range inventory.Hosts {
			host.Vars[k] = v
		}
	}
	return &inventory, nil
}

func (i Inventory) GetContextForHost(host *Host) (*HostContext, error) {
	ctx, err := InitializeHostContext(host)
	if err != nil {
		return nil, err
	}
	defer ctx.Close()

	for k, v := range i.Vars {
		ctx.Facts.Store(k, v)
	}

	for _, group := range i.Groups {
		if group.Hosts != nil {
			if _, hostInGroup := group.Hosts[host.Name]; hostInGroup {
				if group.Vars != nil {
					for k, v := range group.Vars {
						if _, exists := host.Vars[k]; !exists {
							ctx.Facts.Store(k, v)
						}
					}
				}
			}
		}
	}

	if host.Vars != nil {
		for k, v := range host.Vars {
			ctx.Facts.Store(k, v)
		}
	}

	if host.Host == "localhost" || host.Host == "" {
		host.IsLocal = true
	}

	return ctx, nil
}

func (i Inventory) GetContextForRun() (map[string]*HostContext, error) {
	var err error
	contexts := make(map[string]*HostContext)
	for _, host := range i.Hosts {
		common.DebugOutput("Getting context for host %q", host.Name)
		contexts[host.Name], err = i.GetContextForHost(host)
		if err != nil {
			return nil, fmt.Errorf("could not get context for host '%s' (%s): %w", host.Name, host.Host, err)
		}
	}
	return contexts, nil
}
