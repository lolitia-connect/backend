package adapter

import (
	"fmt"
	"sort"
)

func uniqueProxyNames(proxies []Proxy) []string {
	names := make([]string, len(proxies))
	counts := make(map[string]int, len(proxies))
	order := make([]int, len(proxies))
	for i, proxy := range proxies {
		names[i] = proxy.Name
		counts[proxy.Name]++
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return proxies[order[i]].Sort < proxies[order[j]].Sort
	})

	next := make(map[string]int)
	for _, i := range order {
		name := proxies[i].Name
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
			names[i] = candidate
			counts[candidate] = 1
			break
		}
	}
	return names
}
