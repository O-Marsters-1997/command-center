package plan

// StackDepth is branch's distance from a root along the base chain: zero for a branch with no
// recorded base or one based on main.
func StackDepth(branch string, baseByBranch map[string]string) int {
	depth := 0
	visited := map[string]bool{branch: true}
	for {
		base := baseByBranch[branch]
		if base == "" || base == DefaultBaseBranch || visited[base] {
			return depth
		}
		visited[base] = true
		depth++
		branch = base
	}
}

// WaveCount is the number of dependency waves among tickets: one more than the longest blocking
// chain. blocking maps each ticket to the tickets it waits on; blockers outside the map are
// ignored.
func WaveCount(blocking map[TicketURL][]TicketURL) int {
	depth := make(map[TicketURL]int, len(blocking))
	visiting := make(map[TicketURL]bool, len(blocking))
	var depthOf func(url TicketURL) int
	depthOf = func(url TicketURL) int {
		if d, ok := depth[url]; ok {
			return d
		}
		if visiting[url] {
			return 0
		}
		visiting[url] = true
		d := 0
		for _, blocker := range blocking[url] {
			if _, in := blocking[blocker]; in {
				d = max(d, depthOf(blocker)+1)
			}
		}
		visiting[url] = false
		depth[url] = d
		return d
	}
	waves := 0
	for url := range blocking {
		waves = max(waves, depthOf(url)+1)
	}
	return waves
}
