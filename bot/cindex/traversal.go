package cindex

import (
	"container/list"
)

// TraverseBFS performs breadth-first search from a starting node.
func (g *Graph) TraverseBFS(startID int64, edgeKinds []string, maxDepth int, maxNodes int) []*Node {
	if maxDepth <= 0 {
		maxDepth = 10
	}
	if maxNodes <= 0 {
		maxNodes = 100
	}

	kindSet := make(map[string]bool)
	for _, k := range edgeKinds {
		kindSet[k] = true
	}

	visited := make(map[int64]bool)
	var result []*Node

	type item struct {
		id    int64
		depth int
	}

	queue := list.New()
	queue.PushBack(item{id: startID, depth: 0})
	visited[startID] = true

	for queue.Len() > 0 && len(result) < maxNodes {
		front := queue.Front()
		queue.Remove(front)
		cur := front.Value.(item)

		if cur.depth > maxDepth {
			continue
		}

		if n, ok := g.Nodes[cur.id]; ok {
			result = append(result, n)
		}

		// Follow outgoing edges
		for _, e := range g.edgesByFrom[cur.id] {
			if len(kindSet) > 0 && !kindSet[string(e.Kind)] {
				continue
			}
			if !visited[e.ToID] {
				visited[e.ToID] = true
				queue.PushBack(item{id: e.ToID, depth: cur.depth + 1})
			}
		}

		// Follow incoming edges (reverse traversal)
		for _, e := range g.edgesByTo[cur.id] {
			if len(kindSet) > 0 && !kindSet[string(e.Kind)] {
				continue
			}
			if !visited[e.FromID] {
				visited[e.FromID] = true
				queue.PushBack(item{id: e.FromID, depth: cur.depth + 1})
			}
		}
	}

	return result
}

// TraverseDFS performs depth-first search from a starting node.
func (g *Graph) TraverseDFS(startID int64, edgeKinds []string, maxDepth int, maxNodes int) []*Node {
	if maxDepth <= 0 {
		maxDepth = 10
	}
	if maxNodes <= 0 {
		maxNodes = 100
	}

	kindSet := make(map[string]bool)
	for _, k := range edgeKinds {
		kindSet[k] = true
	}

	visited := make(map[int64]bool)
	var result []*Node

	var dfs func(id int64, depth int)
	dfs = func(id int64, depth int) {
		if len(result) >= maxNodes || depth > maxDepth || visited[id] {
			return
		}
		visited[id] = true

		if n, ok := g.Nodes[id]; ok {
			result = append(result, n)
		}

		for _, e := range g.edgesByFrom[id] {
			if len(kindSet) > 0 && !kindSet[string(e.Kind)] {
				continue
			}
			dfs(e.ToID, depth+1)
		}
		for _, e := range g.edgesByTo[id] {
			if len(kindSet) > 0 && !kindSet[string(e.Kind)] {
				continue
			}
			dfs(e.FromID, depth+1)
		}
	}

	dfs(startID, 0)
	return result
}

// GetImpactRadius returns all nodes impacted by changes to the given node.
// This includes callers (who depend on it) and callees (what it depends on).
func (g *Graph) GetImpactRadius(nodeID int64) []*Node {
	callers := g.GetCallers(nodeID)
	callees := g.GetCallees(nodeID)

	seen := make(map[int64]bool)
	var result []*Node

	if n, ok := g.Nodes[nodeID]; ok {
		result = append(result, n)
		seen[nodeID] = true
	}

	for _, n := range callers {
		if !seen[n.ID] {
			result = append(result, n)
			seen[n.ID] = true
		}
	}
	for _, n := range callees {
		if !seen[n.ID] {
			result = append(result, n)
			seen[n.ID] = true
		}
	}

	return result
}

// FindPaths finds all paths between two nodes (up to maxPaths).
func (g *Graph) FindPaths(fromID, toID int64, maxPaths int) [][]int64 {
	if maxPaths <= 0 {
		maxPaths = 5
	}

	const maxDepth = 20

	var paths [][]int64
	visited := make(map[int64]bool)

	var dfs func(current int64, path []int64, depth int)
	dfs = func(current int64, path []int64, depth int) {
		if len(paths) >= maxPaths || depth > maxDepth {
			return
		}
		if current == toID {
			fullPath := make([]int64, len(path))
			copy(fullPath, path)
			paths = append(paths, fullPath)
			return
		}

		visited[current] = true
		for _, e := range g.edgesByFrom[current] {
			if !visited[e.ToID] {
				dfs(e.ToID, append(path, e.ToID), depth+1)
			}
		}
		visited[current] = false
	}

	dfs(fromID, []int64{fromID}, 0)
	return paths
}

// GetAncestors returns all ancestor nodes (following contains edges upward).
func (g *Graph) GetAncestors(nodeID int64) []*Node {
	var result []*Node
	visited := make(map[int64]bool)

	var walk func(id int64)
	walk = func(id int64) {
		if visited[id] {
			return
		}
		visited[id] = true

		for _, e := range g.edgesByTo[id] {
			if e.Kind == EdgeContains {
				if parent, ok := g.Nodes[e.FromID]; ok {
					result = append(result, parent)
					walk(e.FromID)
				}
			}
		}
	}

	walk(nodeID)
	return result
}

// GetDescendants returns all descendant nodes (following contains edges downward).
func (g *Graph) GetDescendants(nodeID int64) []*Node {
	var result []*Node
	visited := make(map[int64]bool)

	var walk func(id int64)
	walk = func(id int64) {
		if visited[id] {
			return
		}
		visited[id] = true

		for _, e := range g.edgesByFrom[id] {
			if e.Kind == EdgeContains {
				if child, ok := g.Nodes[e.ToID]; ok {
					result = append(result, child)
					walk(e.ToID)
				}
			}
		}
	}

	walk(nodeID)
	return result
}
