package pkg

import (
	"github.com/AlexanderGrooff/spage/pkg/config"
)

type Closure struct {
	HostContext *HostContext
	ExtraFacts  map[string]interface{}
	Config      *config.Config
}

func (c *Closure) GetFacts() map[string]interface{} {
	context := make(map[string]interface{})
	if c == nil {
		return context
	}

	// Add the host object itself, accessible via 'host' key
	if c.HostContext != nil && c.HostContext.Host != nil {
		context["host"] = c.HostContext.Host
	}

	// Load host vars (low precedence)
	if c.HostContext != nil && c.HostContext.Host != nil && c.HostContext.Host.Vars != nil {
		for k, v := range c.HostContext.Host.Vars {
			context[k] = v
		}
	}

	// Load facts from the sync.Map, potentially overwriting host vars
	if c.HostContext != nil && c.HostContext.Facts != nil {
		c.HostContext.Facts.Range(func(key, value interface{}) bool {
			if k, ok := key.(string); ok {
				context[k] = value
			}
			return true
		})
	}

	// Load extra facts from the task (highest precedence), overwriting anything previous
	if c.ExtraFacts != nil {
		for k, v := range c.ExtraFacts {
			context[k] = v
		}
	}

	return context
}

func (c *Closure) GetFact(key string) (interface{}, bool) {
	if v, ok := c.ExtraFacts[key]; ok {
		return v, true
	}
	return c.HostContext.Facts.Load(key)
}

func (c *Closure) Clone() *Closure {
	newClosure := &Closure{
		HostContext: c.HostContext,
		ExtraFacts:  make(map[string]interface{}),
	}
	for k, v := range c.ExtraFacts {
		newClosure.ExtraFacts[k] = v
	}
	return newClosure
}
