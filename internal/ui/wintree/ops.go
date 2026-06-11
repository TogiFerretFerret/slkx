package wintree

// Split divides window id in the given direction within bounds. The
// new window clones the source's channel, is placed after (below /
// right of) the source, and its id is returned. Returns ErrNoRoom
// when the resulting windows would violate MinWidth/MinHeight at the
// current bounds, ErrNotFound for an unknown id.
func (t *Tree) Split(id LeafID, dir Dir, bounds Rect) (LeafID, error) {
	leaf, parent := t.findLeaf(id)
	if leaf == nil {
		return 0, ErrNotFound
	}

	min := MinWidth
	if dir == SplitStacked {
		min = MinHeight
	}

	// Refusal check. Inserting a sibling into a same-direction parent
	// re-divides the PARENT's extent among k+1 children; otherwise the
	// leaf's own rect divides in two.
	sameDirParent := parent != nil && parent.dir == dir
	if sameDirParent {
		pr, ok := t.nodeRect(parent, bounds)
		if !ok {
			return 0, ErrNotFound
		}
		extent := pr.W
		if dir == SplitStacked {
			extent = pr.H
		}
		if extent/(len(parent.children)+1) < min {
			return 0, ErrNoRoom
		}
	} else {
		lr, ok := t.nodeRect(leaf, bounds)
		if !ok {
			return 0, ErrNotFound
		}
		extent := lr.W
		if dir == SplitStacked {
			extent = lr.H
		}
		if extent/2 < min {
			return 0, ErrNoRoom
		}
	}

	nid := t.next
	t.next++
	newLeaf := &node{id: nid, ch: leaf.ch}

	if sameDirParent {
		idx := childIndex(parent, leaf)
		parent.children = append(parent.children, nil)
		copy(parent.children[idx+2:], parent.children[idx+1:])
		parent.children[idx+1] = newLeaf
	} else {
		// Replace the leaf in place with a split node so the parent's
		// child pointer stays valid: the old window moves into child 0.
		old := &node{id: leaf.id, ch: leaf.ch}
		leaf.id = 0
		leaf.ch = Channel{}
		leaf.dir = dir
		leaf.children = []*node{old, newLeaf}
	}
	return nid, nil
}

// Close removes window id, hands its space to its siblings, and
// returns the window that should receive focus (the previous sibling
// subtree's first leaf, or the new first sibling's). Returns
// ErrLastWindow when id is the only window.
func (t *Tree) Close(id LeafID) (LeafID, error) {
	leaf, parent := t.findLeaf(id)
	if leaf == nil {
		return 0, ErrNotFound
	}
	if parent == nil {
		return 0, ErrLastWindow
	}
	idx := childIndex(parent, leaf)
	parent.children = append(parent.children[:idx], parent.children[idx+1:]...)

	var focusNode *node
	if idx > 0 {
		focusNode = parent.children[idx-1]
	} else {
		focusNode = parent.children[0]
	}
	focusID := firstLeaf(focusNode).id

	// A split with one child dissolves: the child takes its place.
	if len(parent.children) == 1 {
		only := parent.children[0]
		parent.id = only.id
		parent.ch = only.ch
		parent.dir = only.dir
		parent.children = only.children
	}
	return focusID, nil
}

// Only collapses the tree to just window id (vim ctrl+w o / :only).
func (t *Tree) Only(id LeafID) error {
	leaf, _ := t.findLeaf(id)
	if leaf == nil {
		return ErrNotFound
	}
	t.root = &node{id: leaf.id, ch: leaf.ch}
	return nil
}

// Cycle returns the window delta steps from id in tree order,
// wrapping (ctrl+w w). Unknown ids return id unchanged.
func (t *Tree) Cycle(id LeafID, delta int) LeafID {
	ls := t.Leaves()
	for i, l := range ls {
		if l == id {
			n := len(ls)
			return ls[((i+delta)%n+n)%n]
		}
	}
	return id
}

func childIndex(parent, child *node) int {
	for i, c := range parent.children {
		if c == child {
			return i
		}
	}
	return -1
}
