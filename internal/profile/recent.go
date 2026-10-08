package profile

import "sort"

func Recent(profiles []Profile, n int) []Profile {
	var used []Profile
	for _, p := range profiles {
		if !p.LastUsedAt.IsZero() {
			used = append(used, p)
		}
	}
	sort.SliceStable(used, func(i, j int) bool {
		return used[i].LastUsedAt.After(used[j].LastUsedAt)
	})
	if len(used) > n {
		used = used[:n]
	}
	return used
}
