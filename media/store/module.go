package store

import (
	"sync"

	"gopkg.in/yaml.v3"
)

// Module contains attributes which describe a storage module.
type Module struct {
	// ServiceConfigSkeleton returns an instance of config used to initialize
	// the service. This skeleton contains a config structure.
	ServiceConfigSkeleton func() ServiceConfig

	// NewService create a storage service backend connection. This is
	// usually initialize the client of the object storage.
	NewService func(config ServiceConfig) (Service, error)
}

var (
	modules   = map[string]Module{}
	modulesMu sync.RWMutex
)

func ModuleNames() []string {
	modulesMu.Lock()
	defer modulesMu.Unlock()

	var names []string
	for name := range modules {
		names = append(names, name)
	}

	return names
}

func AvailableModules() map[string]Module {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	return modules
}

func NewServiceClient(
	serviceName string,
	config any,
) (Service, error) {
	if serviceName == "" {
		return nil, nil
	}

	var module Module
	modulesMu.RLock()
	module, _ = modules[serviceName]
	modulesMu.RUnlock()

	return module.NewService(config)
}

func RegisterModule(
	serviceName string,
	module Module,
) {
	modulesMu.Lock()
	defer modulesMu.Unlock()

	if _, dup := modules[serviceName]; dup {
		panic("called twice for service " + serviceName)
	}

	modules[serviceName] = module
}

func ModuleConfigSkeletons() map[string]any {
	modulesMu.RLock()
	defer modulesMu.RUnlock()

	configs := map[string]any{}
	for serviceName, mod := range modules {
		if mod.ServiceConfigSkeleton != nil {
			configs[serviceName] = mod.ServiceConfigSkeleton()
		}
	}

	return configs
}

func UnmarshalModuleConfigFromYaml(conf any) map[string]any {
	modulesMu.RLock()
	defer modulesMu.RUnlock()
	configs := map[string]any{}
	modsConf, ok := conf.(map[string]any)
	if !ok {
		return configs
	}
	for serviceName, mod := range modules {
		_, ok := modsConf[serviceName]
		if !ok {
			continue
		}
		if mod.ServiceConfigSkeleton != nil {
			modCfg := mod.ServiceConfigSkeleton()
			b, _ := yaml.Marshal(modsConf[serviceName])
			_ = yaml.Unmarshal(b, &modCfg)
			configs[serviceName] = modCfg
		}
	}

	return configs
}
