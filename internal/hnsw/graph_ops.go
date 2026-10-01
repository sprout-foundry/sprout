package hnsw

// graph_ops.go — the public graph operations (Add, Search, Len,
// Delete, Lookup) split out of graph.go; the layer machinery they
// build on stays in graph.go.
// Add inserts nodes into the graph.
// If another node with the same ID exists, it is replaced.
func (g *Graph[K]) Add(nodes ...Node[K]) {
	for _, node := range nodes {
		key := node.Key
		vec := node.Value

		g.assertDims(vec)
		insertLevel := g.randomLevel()
		// Create layers that don't exist yet.
		for insertLevel >= len(g.layers) {
			g.layers = append(g.layers, &layer[K]{})
		}

		if insertLevel < 0 {
			panic("invalid level")
		}

		var elevator *K

		preLen := g.Len()

		// Insert node at each layer, beginning with the highest.
		for i := len(g.layers) - 1; i >= 0; i-- {
			layer := g.layers[i]
			newNode := &layerNode[K]{
				Node: Node[K]{
					Key:   key,
					Value: vec,
				},
			}

			// Insert the new node into the layer.
			if layer.entry() == nil {
				layer.nodes = map[K]*layerNode[K]{key: newNode}
				continue
			}

			// Now at the highest layer with more than one node, so we can begin
			// searching for the best way to enter the graph.
			searchPoint := layer.entry()

			// On subsequent layers, we use the elevator node to enter the graph
			// at the best point. The HNSW invariant says any node at a higher
			// layer also exists at every lower layer, so this lookup should
			// always succeed — but prior Delete / load-from-disk paths have
			// been observed to leave the layer set in a state where the
			// elevator key is missing from the current layer (see commit
			// 2513a566 for the same class of dangling-reference bug in
			// isolate()). Fall back to the layer's entry node instead of
			// dereferencing nil, which manifests as a SIGSEGV at addr=0x10
			// (the offset of layerNode.Value) inside search().
			if elevator != nil {
				if sp := layer.nodes[*elevator]; sp != nil {
					searchPoint = sp
				}
			}

			if g.Distance == nil {
				panic("(*Graph).Distance must be set")
			}

			neighborhood := searchPoint.search(g.M, g.EfSearch, vec, g.Distance)
			if len(neighborhood) == 0 {
				// This should never happen because the searchPoint itself
				// should be in the result set.
				panic("no nodes found")
			}

			// Re-set the elevator node for the next layer.
			elevator = ptr(neighborhood[0].node.Key)

			if insertLevel >= i {
				if _, ok := layer.nodes[key]; ok {
					g.Delete(key)
				}
				// Insert the new node into the layer.
				layer.nodes[key] = newNode
				for _, node := range neighborhood {
					// Create a bi-directional edge between the new node and the best node.
					node.node.addNeighbor(newNode, g.M, g.Distance)
					newNode.addNeighbor(node.node, g.M, g.Distance)
				}
			}
		}

		// Invariant check: the node should have been added to the graph.
		// Backported from upstream: replaced panic with graceful empty-layer
		// cleanup. The panic could fire after Delete+Add sequences when empty
		// layers weren't properly removed.
		if g.Len() != preLen+1 {
			if len(g.layers) > 0 && g.layers[len(g.layers)-1].entry() == nil {
				g.layers = g.layers[:len(g.layers)-1]
			}
		}
	}
}

// Search finds the k nearest neighbors from the target node.
func (h *Graph[K]) Search(near Vector, k int) []Node[K] {
	h.assertDims(near)
	if len(h.layers) == 0 {
		return nil
	}

	var (
		efSearch = h.EfSearch

		elevator *K
	)

	for layer := len(h.layers) - 1; layer >= 0; layer-- {
		searchPoint := h.layers[layer].entry()
		// See the same guard in Add(): a stale elevator key absent from
		// this layer would otherwise nil-dereference inside search().
		if elevator != nil {
			if sp := h.layers[layer].nodes[*elevator]; sp != nil {
				searchPoint = sp
			}
		}
		if searchPoint == nil {
			continue
		}

		// Descending hierarchies
		if layer > 0 {
			nodes := searchPoint.search(1, efSearch, near, h.Distance)
			elevator = ptr(nodes[0].node.Key)
			continue
		}

		nodes := searchPoint.search(k, efSearch, near, h.Distance)
		out := make([]Node[K], 0, len(nodes))

		for _, node := range nodes {
			out = append(out, node.node.Node)
		}

		return out
	}

	panic("unreachable")
}

// Len returns the number of nodes in the graph.
func (h *Graph[K]) Len() int {
	if len(h.layers) == 0 {
		return 0
	}
	return h.layers[0].size()
}

// Delete removes a node from the graph by key.
// It tries to preserve the clustering properties of the graph by
// replenishing connectivity in the affected neighborhoods.
// Backported from upstream: empty layers are now removed after deletion
// to prevent degenerate graph state that causes panics on subsequent Add().
func (h *Graph[K]) Delete(key K) bool {
	if len(h.layers) == 0 {
		return false
	}

	deleteLayer := map[int]struct{}{}
	var deleted bool
	for i, layer := range h.layers {
		node, ok := layer.nodes[key]
		if !ok {
			continue
		}
		delete(layer.nodes, key)
		if len(layer.nodes) == 0 {
			deleteLayer[i] = struct{}{}
		}
		node.isolate(h.M, h.Distance)
		deleted = true
	}

	if len(deleteLayer) > 0 {
		newLayers := make([]*layer[K], 0, len(h.layers)-len(deleteLayer))
		for i, layer := range h.layers {
			if _, ok := deleteLayer[i]; ok {
				continue
			}
			newLayers = append(newLayers, layer)
		}

		h.layers = newLayers
	}

	return deleted
}

// Lookup returns the vector with the given key.
func (h *Graph[K]) Lookup(key K) (Vector, bool) {
	if len(h.layers) == 0 {
		return nil, false
	}

	node, ok := h.layers[0].nodes[key]
	if !ok {
		return nil, false
	}
	return node.Value, ok
}
