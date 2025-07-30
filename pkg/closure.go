package pkg

import (
	"log"

	"github.com/AlexanderGrooff/jinja-go"
	"github.com/AlexanderGrooff/spage/pkg/config"
)

type Closure struct {
	HostContext *HostContext
	ExtraFacts  map[string]interface{}
	Config      *config.Config
}

func ConstructClosure(c *HostContext, t Task, cfg *config.Config) *Closure {
	closure := Closure{
		HostContext: c,
		Config:      cfg,
		ExtraFacts:  make(map[string]interface{}),
	}

	if t.Loop != nil {
		closure.ExtraFacts["item"] = t.Loop
	}
	if t.Vars != nil {
		switch t.Vars.(type) {
		case string:
			vars, err := jinja.ParseVariables(t.Vars.(string))
			if err != nil {
				log.Fatalf("Failed to parse vars: %v", err)
			}
			for _, v := range vars {
				closure.ExtraFacts[v] = t.Vars
			}
		case map[string]interface{}:
			for k, v := range t.Vars.(map[string]interface{}) {
				closure.ExtraFacts[k] = v
			}
		default:
			log.Fatalf("Invalid vars type: %T", t.Vars)
		}
	}

	return &closure
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
