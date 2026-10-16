package core

import (
	"fmt"
	"sort"
)

var registry = map[string]func() Source{}

func Register(name string, factory func() Source) {
	registry[name] = factory
}

func Get(name string) (Source, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("неизвестный источник %q (доступные: %v)", name, List())
	}
	return f(), nil
}

func List() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
