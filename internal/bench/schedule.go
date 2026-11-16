package bench

// Item is one workflow run to dispatch.
type Item struct {
	Iteration int
	Workflow  string
	Runner    string
	// Warmup runs are recorded but left out of summaries.
	Warmup bool
}

// Schedule returns warmup+runs iterations of every workflow on every runner, cycling through the runners
// innermost so slow periods on GitHub's side spread over all runner types instead of hitting one.
func Schedule(workflows, runners []string, warmup, runs int) []Item {
	items := make([]Item, 0, (warmup+runs)*len(workflows)*len(runners))
	for i := range warmup + runs {
		for _, w := range workflows {
			for _, r := range runners {
				items = append(items, Item{Iteration: i, Workflow: w, Runner: r, Warmup: i < warmup})
			}
		}
	}
	return items
}
