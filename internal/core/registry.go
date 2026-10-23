package core

import (
	"fmt"
	"sort"
	"strings"
)

var registry = map[string]func() Source{}

func Register(name string, factory func() Source) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("источник %q уже зарегистрирован", name))
	}
	registry[name] = factory
}

func Get(name string) (Source, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("неизвестный источник %q (доступные: %s)", name, strings.Join(Names(), ", "))
	}
	return f(), nil
}

func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func All() []Source {
	result := make([]Source, 0, len(registry))
	for _, f := range registry {
		result = append(result, f())
	}
	return result
}
