package controller

import (
	"sort"
	"sync"
	"testing"

	"github.com/snapp-incubator/argocd-complementary-operator/pkg/nameset"
)

func newTestCache() *SafeNsCache {
	return &SafeNsCache{
		projects:   make(map[string]nameset.Nameset[string]),
		namespaces: make(map[string]nameset.Nameset[string]),
		sources:    make(map[string]nameset.Nameset[string]),
	}
}

func TestSafeNsCache_JoinProject(t *testing.T) {
	t.Run("join adds namespace to project and project to namespace", func(t *testing.T) {
		cache := newTestCache()
		cache.JoinProject("ns-a", "proj-1")

		projects := cache.GetProjects("ns-a")
		if len(projects) != 1 || projects[0] != "proj-1" {
			t.Errorf("expected projects [proj-1], got %v", projects)
		}

		namespaces := cache.GetNamespaces("proj-1")
		if len(namespaces) != 1 || namespaces[0] != "ns-a" {
			t.Errorf("expected namespaces [ns-a], got %v", namespaces)
		}
	})

	t.Run("join multiple namespaces to same project", func(t *testing.T) {
		cache := newTestCache()
		cache.JoinProject("ns-a", "proj-1")
		cache.JoinProject("ns-b", "proj-1")
		cache.JoinProject("ns-c", "proj-1")

		namespaces := cache.GetNamespaces("proj-1")
		sort.Strings(namespaces)
		if len(namespaces) != 3 {
			t.Fatalf("expected 3 namespaces, got %d", len(namespaces))
		}
		expected := []string{"ns-a", "ns-b", "ns-c"}
		for i, ns := range expected {
			if namespaces[i] != ns {
				t.Errorf("index %d: expected %q, got %q", i, ns, namespaces[i])
			}
		}
	})

	t.Run("join same namespace to multiple projects", func(t *testing.T) {
		cache := newTestCache()
		cache.JoinProject("ns-a", "proj-1")
		cache.JoinProject("ns-a", "proj-2")

		projects := cache.GetProjects("ns-a")
		sort.Strings(projects)
		if len(projects) != 2 {
			t.Fatalf("expected 2 projects, got %d", len(projects))
		}
		expected := []string{"proj-1", "proj-2"}
		for i, p := range expected {
			if projects[i] != p {
				t.Errorf("index %d: expected %q, got %q", i, p, projects[i])
			}
		}
	})

	t.Run("duplicate join is idempotent", func(t *testing.T) {
		cache := newTestCache()
		cache.JoinProject("ns-a", "proj-1")
		cache.JoinProject("ns-a", "proj-1")

		projects := cache.GetProjects("ns-a")
		if len(projects) != 1 {
			t.Errorf("expected 1 project after duplicate join, got %d", len(projects))
		}

		namespaces := cache.GetNamespaces("proj-1")
		if len(namespaces) != 1 {
			t.Errorf("expected 1 namespace after duplicate join, got %d", len(namespaces))
		}
	})
}

func TestSafeNsCache_LeaveProject(t *testing.T) {
	t.Run("leave removes namespace from project", func(t *testing.T) {
		cache := newTestCache()
		cache.JoinProject("ns-a", "proj-1")
		cache.LeaveProject("ns-a", "proj-1")

		projects := cache.GetProjects("ns-a")
		if len(projects) != 0 {
			t.Errorf("expected 0 projects after leave, got %v", projects)
		}

		namespaces := cache.GetNamespaces("proj-1")
		if len(namespaces) != 0 {
			t.Errorf("expected 0 namespaces after leave, got %v", namespaces)
		}
	})

	t.Run("leave non-existent membership is safe", func(t *testing.T) {
		cache := newTestCache()
		// Should not panic
		cache.LeaveProject("ns-a", "proj-1")

		projects := cache.GetProjects("ns-a")
		if len(projects) != 0 {
			t.Errorf("expected 0 projects, got %v", projects)
		}
	})

	t.Run("leave only removes specified pair", func(t *testing.T) {
		cache := newTestCache()
		cache.JoinProject("ns-a", "proj-1")
		cache.JoinProject("ns-a", "proj-2")
		cache.JoinProject("ns-b", "proj-1")

		cache.LeaveProject("ns-a", "proj-1")

		// ns-a should still be in proj-2
		projects := cache.GetProjects("ns-a")
		if len(projects) != 1 || projects[0] != "proj-2" {
			t.Errorf("expected [proj-2], got %v", projects)
		}

		// proj-1 should still have ns-b
		namespaces := cache.GetNamespaces("proj-1")
		if len(namespaces) != 1 || namespaces[0] != "ns-b" {
			t.Errorf("expected [ns-b], got %v", namespaces)
		}
	})
}

func TestSafeNsCache_TrustSource(t *testing.T) {
	t.Run("trust adds source for project", func(t *testing.T) {
		cache := newTestCache()
		cache.TrustSource("source-ns", "proj-1")

		sources := cache.GetSources("proj-1")
		if len(sources) != 1 || sources[0] != "source-ns" {
			t.Errorf("expected sources [source-ns], got %v", sources)
		}
	})

	t.Run("trust multiple sources for same project", func(t *testing.T) {
		cache := newTestCache()
		cache.TrustSource("source-ns-1", "proj-1")
		cache.TrustSource("source-ns-2", "proj-1")

		sources := cache.GetSources("proj-1")
		sort.Strings(sources)
		if len(sources) != 2 {
			t.Fatalf("expected 2 sources, got %d", len(sources))
		}
		expected := []string{"source-ns-1", "source-ns-2"}
		for i, s := range expected {
			if sources[i] != s {
				t.Errorf("index %d: expected %q, got %q", i, s, sources[i])
			}
		}
	})

	t.Run("duplicate trust is idempotent", func(t *testing.T) {
		cache := newTestCache()
		cache.TrustSource("source-ns", "proj-1")
		cache.TrustSource("source-ns", "proj-1")

		sources := cache.GetSources("proj-1")
		if len(sources) != 1 {
			t.Errorf("expected 1 source after duplicate trust, got %d", len(sources))
		}
	})
}

func TestSafeNsCache_UnTrustSource(t *testing.T) {
	t.Run("untrust removes source for project", func(t *testing.T) {
		cache := newTestCache()
		cache.TrustSource("source-ns", "proj-1")
		cache.UnTrustSource("source-ns", "proj-1")

		sources := cache.GetSources("proj-1")
		if len(sources) != 0 {
			t.Errorf("expected 0 sources after untrust, got %v", sources)
		}
	})

	t.Run("untrust non-existent source is safe", func(t *testing.T) {
		cache := newTestCache()
		// Should not panic
		cache.UnTrustSource("source-ns", "proj-1")
	})

	t.Run("untrust only removes specified source-project pair", func(t *testing.T) {
		cache := newTestCache()
		cache.TrustSource("source-ns", "proj-1")
		cache.TrustSource("source-ns", "proj-2")

		cache.UnTrustSource("source-ns", "proj-1")

		sources1 := cache.GetSources("proj-1")
		if len(sources1) != 0 {
			t.Errorf("expected 0 sources for proj-1, got %v", sources1)
		}

		sources2 := cache.GetSources("proj-2")
		if len(sources2) != 1 || sources2[0] != "source-ns" {
			t.Errorf("expected [source-ns] for proj-2, got %v", sources2)
		}
	})
}

func TestSafeNsCache_GetProjects_UnknownNamespace(t *testing.T) {
	cache := newTestCache()
	projects := cache.GetProjects("unknown-ns")
	if projects == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects for unknown namespace, got %v", projects)
	}
}

func TestSafeNsCache_GetNamespaces_UnknownProject(t *testing.T) {
	cache := newTestCache()
	namespaces := cache.GetNamespaces("unknown-proj")
	if namespaces == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(namespaces) != 0 {
		t.Errorf("expected 0 namespaces for unknown project, got %v", namespaces)
	}
}

func TestSafeNsCache_GetSources_UnknownProject(t *testing.T) {
	cache := newTestCache()
	sources := cache.GetSources("unknown-proj")
	if len(sources) != 0 {
		t.Errorf("expected 0 sources for unknown project, got %v", sources)
	}
}

func TestSafeNsCache_ConcurrentAccess(t *testing.T) {
	cache := newTestCache()
	const goroutines = 50
	const operations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			ns := "ns-" + string(rune('a'+id%10))
			proj := "proj-" + string(rune('0'+id%5))

			for range operations {
				cache.JoinProject(ns, proj)
				cache.GetProjects(ns)
				cache.GetNamespaces(proj)
				cache.TrustSource(ns, proj)
				cache.GetSources(proj)
				cache.LeaveProject(ns, proj)
				cache.UnTrustSource(ns, proj)
			}
		}(i)
	}

	wg.Wait()
	// If we get here without data races or panics, the test passes.
}
