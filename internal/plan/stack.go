package plan

// StackDepth is branch's distance from a root along the base chain: zero for a branch with no
// recorded base, or one based on main; one more than its own base's depth otherwise
// (docs/prds/prd-command-centre.md § The page → stack order).
func StackDepth(branch string, baseByBranch map[string]string) int {
	depth := 0
	visited := map[string]bool{branch: true}
	for {
		base := baseByBranch[branch]
		if base == "" || base == defaultBranch || visited[base] {
			return depth
		}
		visited[base] = true
		depth++
		branch = base
	}
}
