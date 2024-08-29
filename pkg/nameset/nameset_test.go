package nameset_test

import (
	"testing"

	"github.com/snapp-incubator/argocd-complementary-operator/pkg/nameset"
)

func TestAll(t *testing.T) {
	nm := nameset.New[int]()

	elements := []int{10, 11, 12}

	for _, i := range elements {
		nm.Add(i)
	}

	validation := make(map[int]bool)
	for i := range nm.All() {
		validation[i] = true
	}

	if len(validation) != len(elements) {
		t.Fatalf("all should iterate over all (%d) elements in the nameset", len(elements))
	}
	for _, i := range elements {
		if !validation[i] {
			t.Fatalf("all should iterate over all elements in the nameset, but it does not contain %d", i)
		}
	}
}

func TestFilter(t *testing.T) {
	nm := nameset.New[int]()

	elements := []int{10, 11, 12}
	filter := func(i int) bool {
		return i%2 == 0
	}
	elements_filtered := make([]int, 0)

	for _, i := range elements {
		if filter(i) {
			elements_filtered = append(elements_filtered, i)
		}
	}

	for _, i := range elements {
		nm.Add(i)
	}

	validation := make(map[int]bool)
	for i := range nm.Filter(filter) {
		validation[i] = true
	}

	if len(validation) != len(elements_filtered) {
		t.Fatalf("filter should iterate over all (%d) filtered elements in the nameset", len(elements_filtered))
	}
	for _, i := range elements_filtered {
		if !validation[i] {
			t.Fatalf("filter should iterate over all filtered elements in the nameset, but it does not contain %d", i)
		}
	}
}
