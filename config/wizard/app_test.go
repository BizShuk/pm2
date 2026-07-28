package wizard

import "testing"

func TestNamespacesReturnsCopy(t *testing.T) {
	first := Namespaces()
	second := Namespaces()
	if len(first) == 0 {
		t.Fatal("Namespaces() returned no choices")
	}

	first[0] = "changed"
	if second[0] == first[0] {
		t.Fatal("Namespaces() exposed mutable package state")
	}
}
