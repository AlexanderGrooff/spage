package types

import (
	"fmt"
	"gopkg.in/yaml.v3"
)

// Inventory represents the complete inventory structure
type Inventory struct {
	Hosts  map[string]*Host       `yaml:"all"`
	Vars   map[string]interface{} `yaml:"vars"`
	Groups map[string]*Group      `yaml:"-"` // Groups are parsed manually from top-level keys
	Plugin string                 `yaml:"plugin"`
}

// Host represents a single host in the inventory
type Host struct {
	Name    string `json:"name"`
	Host    string `yaml:"host"`
	IsLocal bool
	Vars    map[string]interface{}
	Groups  map[string]string `yaml:"groups"`
	Config  interface{}       // SSH configuration or other host-specific config
}

// Group represents a group of hosts in the inventory
type Group struct {
	Hosts map[string]*Host
	Vars  map[string]interface{}
}

// Prepare initializes the host's maps if they are nil
func (h *Host) Prepare() {
	if h.Vars == nil {
		h.Vars = make(map[string]interface{})
	}
	if h.Groups == nil {
		h.Groups = make(map[string]string)
	}

	if h.Host == "localhost" || h.Host == "" {
		h.IsLocal = true
	}
}

// UnmarshalYAML implements custom YAML unmarshaling for Inventory to handle top-level groups
func (inv *Inventory) UnmarshalYAML(value *yaml.Node) error {
	// Create a map to capture all YAML data
	var rawData map[string]interface{}
	if err := value.Decode(&rawData); err != nil {
		return err
	}

	// Initialize maps
	inv.Hosts = make(map[string]*Host)
	inv.Vars = make(map[string]interface{})
	inv.Groups = make(map[string]*Group)
	if pluginVal, ok := rawData["plugin"]; ok {
		if pluginStr, ok := pluginVal.(string); ok {
			inv.Plugin = pluginStr
		}
	}

	// Process each top-level key
	for key, val := range rawData {
		switch key {
		case "vars":
			// Handle global variables
			if varsData, ok := val.(map[string]interface{}); ok {
				inv.Vars = varsData
			}
		case "plugin":
			// Skip plugin field, already handled above
			continue
		default:
			// All top-level keys (including "all") are groups with hosts and vars subkeys
			if groupData, ok := val.(map[string]interface{}); ok {
				var group Group
				// Convert groupData to YAML bytes and unmarshal into Group
				groupBytes, err := yaml.Marshal(groupData)
				if err != nil {
					return fmt.Errorf("failed to marshal group data for %s: %w", key, err)
				}
				if err := yaml.Unmarshal(groupBytes, &group); err != nil {
					return fmt.Errorf("failed to unmarshal group %s: %w", key, err)
				}
				// Ensure hosts map is initialized
				if group.Hosts == nil {
					group.Hosts = make(map[string]*Host)
				}
				// Handle special case for "all" group (may not have hosts subkey)
				if key == "all" && group.Hosts != nil {
					for hostName, host := range group.Hosts {
						inv.Hosts[hostName] = host
					}
				}
				inv.Groups[key] = &group
			}
		}
	}

	return nil
}

// String returns the host name as string representation
func (h Host) String() string {
	return h.Name
}

// UnmarshalYAML implements custom YAML unmarshaling for Host to capture unknown fields into Vars
func (h *Host) UnmarshalYAML(value *yaml.Node) error {
	// Create a map to capture all YAML data
	var rawData map[string]interface{}
	if err := value.Decode(&rawData); err != nil {
		return err
	}

	// Initialize Vars map if nil
	if h.Vars == nil {
		h.Vars = make(map[string]interface{})
	}

	// Process known fields
	if host, ok := rawData["host"]; ok {
		if hostStr, ok := host.(string); ok {
			h.Host = hostStr
		}
	}

	if groups, ok := rawData["groups"]; ok {
		if groupsMap, ok := groups.(map[string]interface{}); ok {
			if h.Groups == nil {
				h.Groups = make(map[string]string)
			}
			for k, v := range groupsMap {
				if vStr, ok := v.(string); ok {
					h.Groups[k] = vStr
				}
			}
		}
	}

	// Put all other fields into Vars (including ansible_ssh_private_key_file)
	knownFields := map[string]bool{
		"host":   true,
		"groups": true,
	}

	for key, value := range rawData {
		if !knownFields[key] {
			h.Vars[key] = value
		}
	}

	return nil
}

// GetInitialFactsForHost gathers and layers facts for a specific host from the inventory.
// It applies global inventory vars, then group vars, then host-specific vars.
func (i *Inventory) GetInitialFactsForHost(host *Host) map[string]interface{} {
	facts := make(map[string]interface{})

	// 1. Apply global inventory vars
	for k, v := range i.Vars {
		facts[k] = v
	}

	// 2. Apply group vars
	for groupName, group := range i.Groups {
		// Check if host is explicitly listed in group's hosts
		if _, isMember := group.Hosts[host.Name]; isMember {
			for k, v := range group.Vars {
				facts[k] = v // Group vars override global vars
			}
		} else {
			// Check if the host is associated with this group via its own host.Groups field
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

	return facts
}

// GetHostByName returns a host by name from the inventory
func (i *Inventory) GetHostByName(name string) (*Host, error) {
	host, ok := i.Hosts[name]
	if !ok {
		return nil, fmt.Errorf("host '%s' not found in inventory", name)
	}
	return host, nil
}
