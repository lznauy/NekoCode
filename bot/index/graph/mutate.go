package graph

// AddNode adds a node to the graph and returns its ID.
func (g *Graph) AddNode(n *Node) int64 {
	if n.ID == 0 {
		g.nextNodeID++
		n.ID = g.nextNodeID
	}
	g.Nodes[n.ID] = n
	g.nodesByName[n.Name] = append(g.nodesByName[n.Name], n)
	g.nodesByFile[n.File] = append(g.nodesByFile[n.File], n)
	if n.ID >= g.nextNodeID {
		g.nextNodeID = n.ID
	}
	return n.ID
}

// AddEdge adds an edge to the graph and returns its ID.
func (g *Graph) AddEdge(e *Edge) int64 {
	if e.ID == 0 {
		g.nextEdgeID++
		e.ID = g.nextEdgeID
	}
	g.Edges[e.ID] = e
	g.edgesByFrom[e.FromID] = append(g.edgesByFrom[e.FromID], e)
	g.edgesByTo[e.ToID] = append(g.edgesByTo[e.ToID], e)
	if e.ID >= g.nextEdgeID {
		g.nextEdgeID = e.ID
	}
	return e.ID
}

// RemoveNode removes a node and all its edges.
func (g *Graph) RemoveNode(id int64) {
	n, ok := g.Nodes[id]
	if !ok {
		return
	}

	nodes := g.nodesByName[n.Name]
	for i, nd := range nodes {
		if nd.ID == id {
			g.nodesByName[n.Name] = append(nodes[:i], nodes[i+1:]...)
			break
		}
	}
	nodes = g.nodesByFile[n.File]
	for i, nd := range nodes {
		if nd.ID == id {
			g.nodesByFile[n.File] = append(nodes[:i], nodes[i+1:]...)
			break
		}
	}

	for _, e := range g.edgesByFrom[id] {
		delete(g.Edges, e.ID)
		toEdges := g.edgesByTo[e.ToID]
		for i, edge := range toEdges {
			if edge.ID == e.ID {
				g.edgesByTo[e.ToID] = append(toEdges[:i], toEdges[i+1:]...)
				break
			}
		}
	}
	for _, e := range g.edgesByTo[id] {
		delete(g.Edges, e.ID)
		fromEdges := g.edgesByFrom[e.FromID]
		for i, edge := range fromEdges {
			if edge.ID == e.ID {
				g.edgesByFrom[e.FromID] = append(fromEdges[:i], fromEdges[i+1:]...)
				break
			}
		}
	}
	delete(g.edgesByFrom, id)
	delete(g.edgesByTo, id)
	delete(g.Nodes, id)
}

// RemoveFileNodes removes all nodes and edges from a specific file.
func (g *Graph) RemoveFileNodes(file string) {
	nodes := append([]*Node(nil), g.nodesByFile[file]...)
	for _, n := range nodes {
		g.RemoveNode(n.ID)
	}
	delete(g.nodesByFile, file)
	delete(g.files, file)
}

// SetFileInfo records indexed file metadata in the graph.
func (g *Graph) SetFileInfo(path, hash, lang string) {
	g.files[path] = &FileInfo{Path: path, ContentHash: hash, Language: lang}
}

// ReindexEdgeTo updates the incoming-edge index after an edge target changes.
func (g *Graph) ReindexEdgeTo(e *Edge, oldToID int64) {
	oldSlice := g.edgesByTo[oldToID]
	for idx, edge := range oldSlice {
		if edge.ID == e.ID {
			g.edgesByTo[oldToID] = append(oldSlice[:idx], oldSlice[idx+1:]...)
			break
		}
	}
	g.edgesByTo[e.ToID] = append(g.edgesByTo[e.ToID], e)
}
