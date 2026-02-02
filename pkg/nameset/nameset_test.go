package nameset_test

import (
	"testing"

	"github.com/snapp-incubator/argocd-complementary-operator/pkg/nameset"
)

func TestNew(t *testing.T) {
	t.Run("Creates non-nil nameset", func(t *testing.T) {
		nm := nameset.New[string]()
		_ = nm.Len()
	})

	t.Run("Creates empty nameset", func(t *testing.T) {
		nm := nameset.New[string]()
		if nm.Len() != 0 {
			t.Errorf("New() should create empty nameset, got length = %d", nm.Len())
		}
		if nm.Contains("anything") {
			t.Error("New() nameset should not contain any items")
		}
	})

	t.Run("Initializes with different types", func(t *testing.T) {
		// Test with string
		stringSet := nameset.New[string]()
		if stringSet.Len() != 0 {
			t.Error("New[string]() should create empty set")
		}

		// Test with int
		intSet := nameset.New[int]()
		if intSet.Len() != 0 {
			t.Error("New[int]() should create empty set")
		}

		// Test with bool
		boolSet := nameset.New[bool]()
		if boolSet.Len() != 0 {
			t.Error("New[bool]() should create empty set")
		}

		// Advanced: Test with custom type
		type UserID string
		userSet := nameset.New[UserID]()
		if userSet.Len() != 0 {
			t.Error("New[UserID]() should create empty set")
		}
	})

	t.Run("Can add items immediately", func(t *testing.T) {
		nm := nameset.New[string]()

		// This tests that the internal map is initialized
		// If the map was nil, this would panic
		nm.Add("item")

		if !nm.Contains("item") {
			t.Error("Should be able to add items to new nameset")
		}

		if nm.Len() != 1 {
			t.Errorf("After adding one item, length should be 1, got %d", nm.Len())
		}
	})

}
func TestAdd(t *testing.T) {
	nm := nameset.New[string]()
	nm.Add("foo")
	nm.Add("foo")
	if !nm.Contains("foo") {
		t.Fatal("Add should add element to nameset")
	}
	if nm.Len() != 1 {
		t.Fatal("Only one item should exists for duplicate items in elements")
	}
	nm.Add("bar")
	if nm.Len() != 2 {
		t.Fatal("Adding multiple values failed")
	}
}
func TestRemove(t *testing.T) {
	nm := nameset.New[string]()
	nm.Add("foo")
	nm.Remove("foo")
	if nm.Contains("foo") {
		t.Fatal("Remove didn't remove element")
	}
	if nm.Len() != 0 {
		t.Fatal("Nameset items should be zero")
	}
	nm.Remove("bar")
	if nm.Len() != 0 {
		t.Fatal("Nameset items should be zero")
	}
	emptySet := nameset.New[string]()
	emptySet.Remove("anything")
	if emptySet.Len() != 0 {
		t.Fatal("Empty set should remain empty after Remove")
	}

}
func TestLen(t *testing.T) {
	nm := nameset.New[string]()

	// Empty set
	if got := nm.Len(); got != 0 {
		t.Fatalf("Nameset length = %d, but want 0", got)
	}

	nm.Add("foo") // After adding items
	if got := nm.Len(); got != 1 {
		t.Fatalf("Nameset length = %d, but want 1", got)
	}

	nm.Add("bar") // Update length after Add
	if got := nm.Len(); got != 2 {
		t.Fatalf("Nameset length = %d, but want 2", got)
	}

	nm.Add("bar") // Length doesn't increase on duplicate adds
	if got := nm.Len(); got != 2 {
		t.Fatalf("Nameset length = %d, but want 2", got)
	}

	nm.Remove("foo") // Already exists
	if got := nm.Len(); got != 1 {
		t.Fatalf("Nameset length = %d, but want 1", got)
	}
}
func TestContains(t *testing.T) {
	nm := nameset.New[string]()

	if nm.Contains("foo") {
		t.Fatal("Empty set should not contain anything")
	}

	nm.Add("foo")
	if !nm.Contains("foo") {
		t.Fatal("The set should contain foo after adding it")
	}

	if nm.Contains("bar") {
		t.Fatal("The set should not contain bar which never added")
	}

	nm.Remove("foo")
	if nm.Contains("foo") {
		t.Fatal("The set should not contain foo after removing it")
	}
}
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
func TestAll_EarlyBreak(t *testing.T) {
	nm := nameset.New[int]()

	elements := []int{1, 2, 3, 4, 5}
	for _, i := range elements {
		nm.Add(i)
	}

	validation := make(map[int]bool)
	for i := range nm.All() {
		validation[i] = true
		if len(validation) == 2 {
			break
		}
	}

	if len(validation) != 2 {
		t.Errorf("Expected to iterate 2 times, got %d", len(validation))
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
func TestFilter_EarlyBreak(t *testing.T) {
	nm := nameset.New[int]()

	// Add elements
	elements := []int{2, 4, 6, 8, 10} // All even
	for _, i := range elements {
		nm.Add(i)
	}

	// Filter for even numbers and break early
	filter := func(i int) bool {
		return i%2 == 0 // Only even numbers
	}

	count := 0
	for range nm.Filter(filter) {
		count++
		if count == 2 {
			break // This makes yield return false, for coverage
		}
	}

	if count != 2 {
		t.Errorf("Expected to iterate 2 times, got %d", count)
	}
}
