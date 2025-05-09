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
	if path == "" {
		common.LogDebug("No inventory file specified, assuming target is this machine", nil)
		return &Inventory{
			Hosts: map[string]*Host{
				"localhost": {Name: "localhost", IsLocal: true, Host: "localhost"},
			},
		}, nil
	}
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

		if host.Host == "localhost" || host.Host == "" {
			host.IsLocal = true
		}
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

			if h.Host == "localhost" || h.Host == "" {
				h.IsLocal = true
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

// GetInitialFactsForHost gathers and layers facts for a specific host from the inventory.
// It applies global inventory vars, then group vars, then host-specific vars.
func (i Inventory) GetInitialFactsForHost(host *Host) map[string]interface{} {
	facts := make(map[string]interface{})

	// 1. Apply global inventory vars
	for k, v := range i.Vars {
		facts[k] = v
	}

	// 2. Apply group vars
	// Need to consider group hierarchy if that's a feature, but for now, iterate all groups.
	// If a host is in multiple groups, the behavior for conflicting vars might depend on group order or a defined precedence.
	// For simplicity here, we assume simple group membership. Last group var applied for a host wins for group vars.
	// Ansible has more complex group var precedence (e.g., parent groups, child groups).
	// This implementation will iterate groups as found in i.Groups map (order not guaranteed).
	// A more robust solution might sort group names or use the host.Groups field to determine relevant groups.

	// Iterate over the host's declared groups first, if available and defined with precedence
	// This part is a bit tricky without knowing exact group precedence rules Spage aims for.
	// For now, we'll stick to iterating all defined groups and checking membership.
	for groupName, group := range i.Groups { // groupName is from inventory.Groups map key
		// Check if the current host is part of this group definition
		// The inventory loading logic already flattens hosts, so direct check here is sufficient for vars.
		// A host `h` from `i.Hosts` might have `h.Groups` map indicating its memberships.
		if _, isMember := group.Hosts[host.Name]; isMember { // Check if host is explicitly listed in group's hosts
			// Alternative: check if host.Groups map contains groupName
			for k, v := range group.Vars {
				facts[k] = v // Group vars override global vars
			}
		} else {
			// Check if the host is associated with this group via its own host.Groups field
			// This handles cases where groups are assigned to hosts, rather than hosts listed under groups.
			if host.Groups != nil {
				if _, assignedToGroup := host.Groups[groupName]; assignedToGroup {
					for k, v := range group.Vars {
						facts[k] = v
					}
				}
			}
		}
	}

	// 3. Apply host-specific vars (these have the highest precedence)
	if host.Vars != nil {
		for k, v := range host.Vars {
			facts[k] = v // Host vars override group and global vars
		}
	}

	common.LogDebug("Compiled initial facts for host", map[string]interface{}{
		"host":        host.Name,
		"facts_count": len(facts),
	})
	return facts
}
