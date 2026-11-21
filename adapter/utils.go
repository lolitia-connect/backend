package adapter

import (
	"fmt"
	"slices"
	"sort"
)

func prepareProxies(proxies []Proxy) []Proxy {
	result := slices.Clone(proxies)
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Sort < result[j].Sort
	})

	counts := make(map[string]int, len(result))
	for _, proxy := range result {
		counts[proxy.Name]++
	}

	next := make(map[string]int)
	for i, proxy := range result {
		name := proxy.Name
		if counts[name] < 2 {
			continue
		}
		for {
			next[name]++
			candidate := fmt.Sprintf("%s #%d", name, next[name])
			// Reserve original names as well as generated names to avoid suffix collisions.
			if counts[candidate] != 0 {
				continue
			}
			result[i].Name = candidate
			counts[candidate] = 1
			break
		}
	}
	return result
}
