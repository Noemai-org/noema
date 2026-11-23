package ownershipport

// NodesFromKinds builds Config.Nodes from the pack's ownership data: the intent node to the resource kind that is owned (the composition passes the Pack's OwnershipKinds, STM-01,
// A475 interim). The data is copied: a later edit of kinds changes nothing here. It holds no node and no kind of its own (INV-14); an empty input gives an empty map, which New refuses.
func NodesFromKinds(kinds map[string]string) map[string]Node {
	nodes := make(map[string]Node, len(kinds))
	for id, k := range kinds {
		nodes[id] = Node{ResourceKind: k}
	}
	return nodes
}
