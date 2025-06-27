package pkg

import (
	"log"

	"github.com/AlexanderGrooff/jinja-go"
)

type Closure struct {
	HostContext *HostContext
	ExtraFacts  map[string]interface{}
}

func ConstructClosure(c *HostContext, t Task) *Closure {
	closure := Closure{
		HostContext: c,
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

func TempClosureForHost(h *Host) *Closure {
	hostContext, err := InitializeHostContext(h)
	if err != nil {
		log.Fatalf("Failed to initialize host context: %v", err)
	}
	closure := Closure{
		HostContext: hostContext,
		ExtraFacts:  make(map[string]interface{}),
	}
	return &closure
}

func (c *Closure) GetFacts() map[string]interface{} {
	facts := make(map[string]interface{})
	c.HostContext.Facts.Range(func(key, value interface{}) bool {
		facts[key.(string)] = value
		return true
	})
	// Extra facts take precedence over host context facts
	for k, v := range c.ExtraFacts {
		facts[k] = v
	}
	return facts
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
