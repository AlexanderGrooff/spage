package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/netbox-community/go-netbox/v4"
	"github.com/netbox-community/go-netbox/v4/netbox/client"
	"github.com/netbox-community/go-netbox/v4/netbox/client/dcim"
	"github.com/netbox-community/go-netbox/v4/netbox/models"
)

// NetBoxInventoryPlugin implements the InventoryPlugin interface
type NetBoxInventoryPlugin struct {
	name   string
	client *client.NetboxAPI
}

// Plugin configuration structure
type NetBoxPluginConfig struct {
	APIEndpoint    string                 `yaml:"api_endpoint"`
	Token          string                 `yaml:"token"`
	ValidateSSL    bool                   `yaml:"validate_ssl"`
	ConfigContext  bool                   `yaml:"config_context"`
	FlattenConfig  bool                   `yaml:"flatten_config_context"`
	QueryFilters   map[string]interface{} `yaml:"query_filters"`
	KeyedGroups    []KeyedGroup           `yaml:"keyed_groups"`
	Groups         map[string]string      `yaml:"groups"`
	Compose        map[string]string      `yaml:"compose"`
	StrictGroups   bool                   `yaml:"strict_groups"`
	TimeoutSeconds int                    `yaml:"timeout"`
	CacheEnabled   bool                   `yaml:"cache"`
	CacheTTL       int                    `yaml:"cache_ttl"`
}

type KeyedGroup struct {
	Key        string `yaml:"key"`
	Prefix     string `yaml:"prefix"`
	Separator  string `yaml:"separator"`
}

// PluginInventoryResult represents the plugin result format
type PluginInventoryResult struct {
	Hosts  map[string]*PluginHost  `json:"_meta"`
	Groups map[string]*PluginGroup `json:",inline"`
}

type PluginHost struct {
	Name string                 `json:"-"`
	Vars map[string]interface{} `json:"hostvars,omitempty"`
}

type PluginGroup struct {
	Hosts    []string               `json:"hosts,omitempty"`
	Children []string               `json:"children,omitempty"`
	Vars     map[string]interface{} `json:"vars,omitempty"`
}

// Name returns the plugin name
func (p *NetBoxInventoryPlugin) Name() string {
	return p.name
}

// Execute runs the NetBox inventory plugin
func (p *NetBoxInventoryPlugin) Execute(ctx context.Context, config map[string]interface{}) (*PluginInventoryResult, error) {
	// Parse plugin configuration
	pluginConfig, err := p.parseConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse NetBox plugin configuration: %w", err)
	}

	// Initialize NetBox client using the official library
	p.client = netbox.NewNetboxWithAPIKey(pluginConfig.APIEndpoint, pluginConfig.Token)

	// Fetch devices from NetBox
	devices, err := p.fetchDevices(ctx, pluginConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch devices from NetBox: %w", err)
	}

	// Convert devices to inventory format
	result, err := p.convertToInventory(devices, pluginConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to convert NetBox devices to inventory: %w", err)
	}

	return result, nil
}

// parseConfig parses the plugin configuration from the map
func (p *NetBoxInventoryPlugin) parseConfig(config map[string]interface{}) (*NetBoxPluginConfig, error) {
	pluginConfig := &NetBoxPluginConfig{
		ValidateSSL:    true,
		ConfigContext:  true,
		FlattenConfig:  false,
		StrictGroups:   true,
		TimeoutSeconds: 30,
		CacheEnabled:   false,
		CacheTTL:       300,
	}

	// Parse API endpoint
	if endpoint, ok := config["api_endpoint"].(string); ok {
		pluginConfig.APIEndpoint = strings.TrimSuffix(endpoint, "/")
	} else {
		return nil, fmt.Errorf("api_endpoint is required")
	}

	// Parse token
	if token, ok := config["token"].(string); ok {
		pluginConfig.Token = token
	} else {
		return nil, fmt.Errorf("token is required")
	}

	// Parse optional configurations
	if validate, ok := config["validate_ssl"].(bool); ok {
		pluginConfig.ValidateSSL = validate
	}

	if configCtx, ok := config["config_context"].(bool); ok {
		pluginConfig.ConfigContext = configCtx
	}

	if flatten, ok := config["flatten_config_context"].(bool); ok {
		pluginConfig.FlattenConfig = flatten
	}

	if timeout, ok := config["timeout"].(int); ok {
		pluginConfig.TimeoutSeconds = timeout
	}

	// Parse query filters
	if filters, ok := config["query_filters"].(map[string]interface{}); ok {
		pluginConfig.QueryFilters = filters
	}

	// Parse keyed groups
	if keyedGroups, ok := config["keyed_groups"].([]interface{}); ok {
		for _, kg := range keyedGroups {
			if keyedGroupMap, ok := kg.(map[string]interface{}); ok {
				keyedGroup := KeyedGroup{
					Separator: "_",
				}
				if key, ok := keyedGroupMap["key"].(string); ok {
					keyedGroup.Key = key
				}
				if prefix, ok := keyedGroupMap["prefix"].(string); ok {
					keyedGroup.Prefix = prefix
				}
				if sep, ok := keyedGroupMap["separator"].(string); ok {
					keyedGroup.Separator = sep
				}
				pluginConfig.KeyedGroups = append(pluginConfig.KeyedGroups, keyedGroup)
			}
		}
	}

	// Parse groups
	if groups, ok := config["groups"].(map[string]interface{}); ok {
		pluginConfig.Groups = make(map[string]string)
		for k, v := range groups {
			if strVal, ok := v.(string); ok {
				pluginConfig.Groups[k] = strVal
			}
		}
	}

	// Parse compose
	if compose, ok := config["compose"].(map[string]interface{}); ok {
		pluginConfig.Compose = make(map[string]string)
		for k, v := range compose {
			if strVal, ok := v.(string); ok {
				pluginConfig.Compose[k] = strVal
			}
		}
	}

	return pluginConfig, nil
}

// fetchDevices retrieves devices from NetBox API using the official client
func (p *NetBoxInventoryPlugin) fetchDevices(ctx context.Context, config *NetBoxPluginConfig) ([]*models.Device, error) {
	params := dcim.NewDcimDevicesListParams().WithContext(ctx)
	
	// Apply query filters
	for key, value := range config.QueryFilters {
		switch key {
		case "site":
			if site, ok := value.(string); ok {
				params = params.WithSite(&site)
			}
		case "site_id":
			if siteID, ok := value.(int); ok {
				siteIDInt64 := int64(siteID)
				params = params.WithSiteID(&siteIDInt64)
			}
		case "role":
			if role, ok := value.(string); ok {
				params = params.WithRole(&role)
			}
		case "role_id":
			if roleID, ok := value.(int); ok {
				roleIDInt64 := int64(roleID)
				params = params.WithRoleID(&roleIDInt64)
			}
		case "tenant":
			if tenant, ok := value.(string); ok {
				params = params.WithTenant(&tenant)
			}
		case "tenant_id":
			if tenantID, ok := value.(int); ok {
				tenantIDInt64 := int64(tenantID)
				params = params.WithTenantID(&tenantIDInt64)
			}
		case "platform":
			if platform, ok := value.(string); ok {
				params = params.WithPlatform(&platform)
			}
		case "status":
			if status, ok := value.(string); ok {
				params = params.WithStatus(&status)
			}
		case "tag":
			if tag, ok := value.(string); ok {
				params = params.WithTag(&tag)
			}
		}
	}
	
	var allDevices []*models.Device
	limit := int64(100)
	offset := int64(0)
	
	for {
		params = params.WithLimit(&limit).WithOffset(&offset)
		
		resp, err := p.client.Dcim.DcimDevicesList(params, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch devices from NetBox: %w", err)
		}
		
		if resp.Payload == nil {
			break
		}
		
		allDevices = append(allDevices, resp.Payload.Results...)
		
		// Check if there are more results
		if resp.Payload.Next == nil || len(resp.Payload.Results) < int(limit) {
			break
		}
		
		offset += limit
	}
	
	return allDevices, nil
}

// convertToInventory converts NetBox devices to inventory format
func (p *NetBoxInventoryPlugin) convertToInventory(devices []*models.Device, config *NetBoxPluginConfig) (*PluginInventoryResult, error) {
	result := &PluginInventoryResult{
		Hosts:  make(map[string]*PluginHost),
		Groups: make(map[string]*PluginGroup),
	}

	for _, device := range devices {
		if device == nil {
			continue
		}
		
		// Skip devices without a name
		if device.Name == nil {
			continue
		}
		
		hostName := *device.Name
		hostAddress := ""
		
		// Determine host address
		if device.PrimaryIp4 != nil && device.PrimaryIp4.Address != nil {
			hostAddress = strings.Split(*device.PrimaryIp4.Address, "/")[0]
		} else if device.PrimaryIp6 != nil && device.PrimaryIp6.Address != nil {
			hostAddress = strings.Split(*device.PrimaryIp6.Address, "/")[0]
		} else {
			// Use device name as fallback
			hostAddress = hostName
		}

		// Create host variables
		hostVars := make(map[string]interface{})
		
		// Basic device information
		if device.ID != nil {
			hostVars["netbox_id"] = *device.ID
		}
		hostVars["netbox_name"] = hostName
		if device.DisplayName != nil {
			hostVars["netbox_display_name"] = *device.DisplayName
		}
		
		// Device type information
		if device.DeviceType != nil {
			if device.DeviceType.Model != nil {
				hostVars["netbox_device_type"] = *device.DeviceType.Model
			}
			if device.DeviceType.Slug != nil {
				hostVars["netbox_device_type_slug"] = *device.DeviceType.Slug
			}
			if device.DeviceType.Manufacturer != nil {
				if device.DeviceType.Manufacturer.Name != nil {
					hostVars["netbox_manufacturer"] = *device.DeviceType.Manufacturer.Name
				}
				if device.DeviceType.Manufacturer.Slug != nil {
					hostVars["netbox_manufacturer_slug"] = *device.DeviceType.Manufacturer.Slug
				}
			}
		}
		
		// Device role information  
		if device.DeviceRole != nil {
			if device.DeviceRole.Name != nil {
				hostVars["netbox_device_role"] = *device.DeviceRole.Name
			}
			if device.DeviceRole.Slug != nil {
				hostVars["netbox_device_role_slug"] = *device.DeviceRole.Slug
			}
		}
		
		// Site information
		if device.Site != nil {
			if device.Site.Name != nil {
				hostVars["netbox_site"] = *device.Site.Name
			}
			if device.Site.Slug != nil {
				hostVars["netbox_site_slug"] = *device.Site.Slug
			}
		}
		
		// Status information
		if device.Status != nil {
			if device.Status.Value != nil {
				hostVars["netbox_status"] = *device.Status.Value
			}
			if device.Status.Label != nil {
				hostVars["netbox_status_label"] = *device.Status.Label
			}
		}
		
		// Optional fields
		if device.Tenant != nil {
			if device.Tenant.Name != nil {
				hostVars["netbox_tenant"] = *device.Tenant.Name
			}
			if device.Tenant.Slug != nil {
				hostVars["netbox_tenant_slug"] = *device.Tenant.Slug
			}
		}
		
		if device.Platform != nil {
			if device.Platform.Name != nil {
				hostVars["netbox_platform"] = *device.Platform.Name
			}
			if device.Platform.Slug != nil {
				hostVars["netbox_platform_slug"] = *device.Platform.Slug
			}
		}
		
		if device.Rack != nil {
			if device.Rack.Name != nil {
				hostVars["netbox_rack"] = *device.Rack.Name
			}
			if device.Position != nil {
				hostVars["netbox_rack_position"] = *device.Position
			}
			if device.Face != nil && device.Face.Value != nil {
				hostVars["netbox_rack_face"] = *device.Face.Value
			}
		}
		
		// IP addresses
		if device.PrimaryIp4 != nil && device.PrimaryIp4.Address != nil {
			hostVars["netbox_primary_ip4"] = *device.PrimaryIp4.Address
		}
		if device.PrimaryIp6 != nil && device.PrimaryIp6.Address != nil {
			hostVars["netbox_primary_ip6"] = *device.PrimaryIp6.Address
		}
		
		// Tags
		if len(device.Tags) > 0 {
			tagNames := make([]string, 0, len(device.Tags))
			for _, tag := range device.Tags {
				if tag != nil && tag.Name != nil {
					tagNames = append(tagNames, *tag.Name)
				}
			}
			if len(tagNames) > 0 {
				hostVars["netbox_tags"] = tagNames
			}
		}
		
		// Custom fields
		if device.CustomFields != nil {
			for key, value := range device.CustomFields {
				if value != nil {
					hostVars[fmt.Sprintf("netbox_custom_%s", key)] = value
				}
			}
		}
		
		// Config context
		if config.ConfigContext && device.ConfigContext != nil {
			if config.FlattenConfig {
				p.flattenMap(device.ConfigContext, "netbox_config_", hostVars)
			} else {
				hostVars["netbox_config_context"] = device.ConfigContext
			}
		}
		
		// Local context data
		if device.LocalContextData != nil {
			if config.FlattenConfig {
				p.flattenMap(device.LocalContextData, "netbox_local_", hostVars)
			} else {
				hostVars["netbox_local_context_data"] = device.LocalContextData
			}
		}
		
		// Apply compose transformations
		for composeKey, composeExpr := range config.Compose {
			if value := p.evaluateExpression(composeExpr, hostVars); value != nil {
				hostVars[composeKey] = value
			}
		}
		
		// Set standard inventory variables
		hostVars["inventory_hostname"] = hostName
		hostVars["ansible_host"] = hostAddress
		
		// Create host
		host := &PluginHost{
			Name: hostName,
			Vars: hostVars,
		}
		result.Hosts[hostName] = host
		
		// Add to groups based on device attributes
		if device.DeviceRole != nil && device.DeviceRole.Slug != nil {
			p.addToGroup(result, fmt.Sprintf("netbox_device_role_%s", *device.DeviceRole.Slug), hostName)
		}
		if device.Site != nil && device.Site.Slug != nil {
			p.addToGroup(result, fmt.Sprintf("netbox_site_%s", *device.Site.Slug), hostName)
		}
		if device.DeviceType != nil {
			if device.DeviceType.Manufacturer != nil && device.DeviceType.Manufacturer.Slug != nil {
				p.addToGroup(result, fmt.Sprintf("netbox_manufacturer_%s", *device.DeviceType.Manufacturer.Slug), hostName)
			}
			if device.DeviceType.Slug != nil {
				p.addToGroup(result, fmt.Sprintf("netbox_device_type_%s", *device.DeviceType.Slug), hostName)
			}
		}
		if device.Status != nil && device.Status.Value != nil {
			p.addToGroup(result, fmt.Sprintf("netbox_status_%s", *device.Status.Value), hostName)
		}
		
		if device.Tenant != nil && device.Tenant.Slug != nil {
			p.addToGroup(result, fmt.Sprintf("netbox_tenant_%s", *device.Tenant.Slug), hostName)
		}
		
		if device.Platform != nil && device.Platform.Slug != nil {
			p.addToGroup(result, fmt.Sprintf("netbox_platform_%s", *device.Platform.Slug), hostName)
		}
		
		// Add tag-based groups
		for _, tag := range device.Tags {
			if tag != nil && tag.Slug != nil {
				p.addToGroup(result, fmt.Sprintf("netbox_tag_%s", *tag.Slug), hostName)
			}
		}
		
		// Apply keyed groups
		for _, keyedGroup := range config.KeyedGroups {
			if groupName := p.evaluateKeyedGroup(keyedGroup, hostVars); groupName != "" {
				p.addToGroup(result, groupName, hostName)
			}
		}
		
		// Apply custom groups
		for groupName, groupExpr := range config.Groups {
			if p.evaluateCondition(groupExpr, hostVars) {
				p.addToGroup(result, groupName, hostName)
			}
		}
	}
	
	return result, nil
}

// Helper methods
func (p *NetBoxInventoryPlugin) addToGroup(result *PluginInventoryResult, groupName, hostName string) {
	if result.Groups[groupName] == nil {
		result.Groups[groupName] = &PluginGroup{
			Hosts: []string{},
			Vars:  make(map[string]interface{}),
		}
	}
	
	// Check if host is already in group
	for _, existingHost := range result.Groups[groupName].Hosts {
		if existingHost == hostName {
			return
		}
	}
	
	result.Groups[groupName].Hosts = append(result.Groups[groupName].Hosts, hostName)
}

func (p *NetBoxInventoryPlugin) flattenMap(m map[string]interface{}, prefix string, result map[string]interface{}) {
	for key, value := range m {
		newKey := prefix + key
		if subMap, ok := value.(map[string]interface{}); ok {
			p.flattenMap(subMap, newKey+"_", result)
		} else {
			result[newKey] = value
		}
	}
}

func (p *NetBoxInventoryPlugin) evaluateExpression(expr string, vars map[string]interface{}) interface{} {
	// Simple expression evaluation - in a real implementation, you might use a template engine
	// For now, just return the variable if it exists
	if value, exists := vars[expr]; exists {
		return value
	}
	return nil
}

func (p *NetBoxInventoryPlugin) evaluateKeyedGroup(keyedGroup KeyedGroup, vars map[string]interface{}) string {
	if value, exists := vars[keyedGroup.Key]; exists && value != nil {
		groupName := fmt.Sprintf("%v", value)
		if keyedGroup.Prefix != "" {
			groupName = keyedGroup.Prefix + keyedGroup.Separator + groupName
		}
		return strings.ToLower(strings.ReplaceAll(groupName, " ", "_"))
	}
	return ""
}

func (p *NetBoxInventoryPlugin) evaluateCondition(condition string, vars map[string]interface{}) bool {
	// Simple condition evaluation - in a real implementation, you might use a more sophisticated evaluator
	// For now, just check if the variable exists and is truthy
	if value, exists := vars[condition]; exists {
		switch v := value.(type) {
		case bool:
			return v
		case string:
			return v != ""
		case int, int64:
			return v != 0
		default:
			return value != nil
		}
	}
	return false
}

// NewNetBoxPlugin creates a new NetBox inventory plugin instance
func NewNetBoxPlugin() *NetBoxInventoryPlugin {
	return &NetBoxInventoryPlugin{
		name: "netbox",
	}
}

// Export the plugin creation function
var NetBoxPlugin = NewNetBoxPlugin()
