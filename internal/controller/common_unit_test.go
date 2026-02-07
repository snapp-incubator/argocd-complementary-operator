package controller

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/snapp-incubator/argocd-complementary-operator/pkg/nameset"
)

func TestHashPassword(t *testing.T) {
	t.Run("valid password returns verifiable hash", func(t *testing.T) {
		hash, err := hashPassword("mysecretpass")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash == "" {
			t.Fatal("hash should not be empty")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("mysecretpass")); err != nil {
			t.Errorf("hash does not match original password: %v", err)
		}
	})

	t.Run("wrong password does not match hash", func(t *testing.T) {
		hash, err := hashPassword("correctpassword")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrongpassword")); err == nil {
			t.Error("wrong password should not match the hash")
		}
	})

	t.Run("password exceeding 72 bytes returns error", func(t *testing.T) {
		longPass := strings.Repeat("a", 73)
		_, err := hashPassword(longPass)
		if err == nil {
			t.Error("expected error for password exceeding 72 bytes")
		}
		if !strings.Contains(err.Error(), "72 bytes") {
			t.Errorf("error message should mention 72 bytes limit, got: %v", err)
		}
	})

	t.Run("exactly 72 bytes password succeeds", func(t *testing.T) {
		pass := strings.Repeat("b", 72)
		hash, err := hashPassword(pass)
		if err != nil {
			t.Fatalf("unexpected error for 72-byte password: %v", err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)); err != nil {
			t.Errorf("hash does not match 72-byte password: %v", err)
		}
	})

	t.Run("empty password succeeds", func(t *testing.T) {
		hash, err := hashPassword("")
		if err != nil {
			t.Fatalf("unexpected error for empty password: %v", err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("")); err != nil {
			t.Errorf("hash does not match empty password: %v", err)
		}
	})

	t.Run("different passwords produce different hashes", func(t *testing.T) {
		hash1, err := hashPassword("pass1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		hash2, err := hashPassword("pass2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash1 == hash2 {
			t.Error("different passwords should produce different hashes")
		}
	})
}

func TestMergeStringSlices(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []string
		expected []string
	}{
		{
			name:     "non-overlapping slices",
			a:        []string{"b", "a"},
			b:        []string{"d", "c"},
			expected: []string{"a", "b", "c", "d"},
		},
		{
			name:     "overlapping slices are deduplicated",
			a:        []string{"a", "b", "c"},
			b:        []string{"b", "c", "d"},
			expected: []string{"a", "b", "c", "d"},
		},
		{
			name:     "identical slices",
			a:        []string{"x", "y"},
			b:        []string{"x", "y"},
			expected: []string{"x", "y"},
		},
		{
			name:     "first empty",
			a:        []string{},
			b:        []string{"a", "b"},
			expected: []string{"a", "b"},
		},
		{
			name:     "second empty",
			a:        []string{"a", "b"},
			b:        []string{},
			expected: []string{"a", "b"},
		},
		{
			name:     "both empty",
			a:        []string{},
			b:        []string{},
			expected: []string{},
		},
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: []string{},
		},
		{
			name:     "result is sorted",
			a:        []string{"z", "m"},
			b:        []string{"a", "f"},
			expected: []string{"a", "f", "m", "z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeStringSlices(tt.a, tt.b)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected length %d, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("index %d: expected %q, got %q", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestIsTeamClusterAdmin(t *testing.T) {
	tests := []struct {
		name     string
		team     string
		list     []string
		expected bool
	}{
		{
			name:     "team is in the list",
			team:     "team-a",
			list:     []string{"team-a", "team-b"},
			expected: true,
		},
		{
			name:     "team is not in the list",
			team:     "team-c",
			list:     []string{"team-a", "team-b"},
			expected: false,
		},
		{
			name:     "empty list",
			team:     "team-a",
			list:     []string{},
			expected: false,
		},
		{
			name:     "nil list",
			team:     "team-a",
			list:     nil,
			expected: false,
		},
		{
			name:     "empty team name",
			team:     "",
			list:     []string{"team-a"},
			expected: false,
		},
		{
			name:     "single item list matching",
			team:     "team-a",
			list:     []string{"team-a"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTeamClusterAdmin(tt.team, tt.list)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCreateAppProj(t *testing.T) {
	// Save and restore global NamespaceCache
	origCache := NamespaceCache
	defer func() { NamespaceCache = origCache }()

	newCache := func() *SafeNsCache {
		return &SafeNsCache{
			projects:   make(map[string]nameset.Nameset[string]),
			namespaces: make(map[string]nameset.Nameset[string]),
			sources:    make(map[string]nameset.Nameset[string]),
		}
	}

	t.Run("basic structure", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("my-team")

		if proj.Name != "my-team" {
			t.Errorf("expected name 'my-team', got %q", proj.Name)
		}
		if proj.Namespace != userArgocdNS {
			t.Errorf("expected namespace %q, got %q", userArgocdNS, proj.Namespace)
		}
		if len(proj.Spec.Roles) != 2 {
			t.Fatalf("expected 2 roles, got %d", len(proj.Spec.Roles))
		}
		if proj.Spec.Roles[0].Name != "my-team-admin" {
			t.Errorf("expected admin role name 'my-team-admin', got %q", proj.Spec.Roles[0].Name)
		}
		if proj.Spec.Roles[1].Name != "my-team-view" {
			t.Errorf("expected view role name 'my-team-view', got %q", proj.Spec.Roles[1].Name)
		}
	})

	t.Run("admin role policies", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		adminPolicies := proj.Spec.Roles[0].Policies

		expectedPolicies := []string{
			"p, proj:team-x:team-x-admin, applications, *, team-x/*, allow",
			"p, proj:team-x:team-x-admin, repositories, *, team-x/*, allow",
			"p, proj:team-x:team-x-admin, exec, create, team-x/*, allow",
		}
		if len(adminPolicies) != len(expectedPolicies) {
			t.Fatalf("expected %d admin policies, got %d", len(expectedPolicies), len(adminPolicies))
		}
		for i, p := range expectedPolicies {
			if adminPolicies[i] != p {
				t.Errorf("admin policy %d: expected %q, got %q", i, p, adminPolicies[i])
			}
		}
	})

	t.Run("view role policies", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		viewPolicies := proj.Spec.Roles[1].Policies

		expectedPolicies := []string{
			"p, proj:team-x:team-x-view, applications, get, team-x/*, allow",
			"p, proj:team-x:team-x-view, repositories, get, team-x/*, allow",
			"p, proj:team-x:team-x-view, logs, get, team-x/*, allow",
		}
		if len(viewPolicies) != len(expectedPolicies) {
			t.Fatalf("expected %d view policies, got %d", len(expectedPolicies), len(viewPolicies))
		}
		for i, p := range expectedPolicies {
			if viewPolicies[i] != p {
				t.Errorf("view policy %d: expected %q, got %q", i, p, viewPolicies[i])
			}
		}
	})

	t.Run("admin role groups", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		adminGroups := proj.Spec.Roles[0].Groups
		expectedGroups := []string{"team-x-admin", "team-x-admin-ci"}
		if len(adminGroups) != len(expectedGroups) {
			t.Fatalf("expected %d admin groups, got %d", len(expectedGroups), len(adminGroups))
		}
		for i, g := range expectedGroups {
			if adminGroups[i] != g {
				t.Errorf("admin group %d: expected %q, got %q", i, g, adminGroups[i])
			}
		}
	})

	t.Run("view role groups include admin groups", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		viewGroups := proj.Spec.Roles[1].Groups
		expectedGroups := []string{"team-x-admin", "team-x-admin-ci", "team-x-view", "team-x-view-ci"}
		if len(viewGroups) != len(expectedGroups) {
			t.Fatalf("expected %d view groups, got %d", len(expectedGroups), len(viewGroups))
		}
		for i, g := range expectedGroups {
			if viewGroups[i] != g {
				t.Errorf("view group %d: expected %q, got %q", i, g, viewGroups[i])
			}
		}
	})

	t.Run("namespace blacklist always contains LimitRange", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		blacklist := proj.Spec.NamespaceResourceBlacklist
		if len(blacklist) != 1 {
			t.Fatalf("expected 1 blacklisted resource, got %d", len(blacklist))
		}
		if blacklist[0].Group != "" || blacklist[0].Kind != "LimitRange" {
			t.Errorf("expected LimitRange in core group, got Group=%q Kind=%q", blacklist[0].Group, blacklist[0].Kind)
		}
	})

	t.Run("non-admin team gets ClusterResourceBlacklist", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("regular-team")
		if len(proj.Spec.ClusterResourceBlacklist) != 1 {
			t.Fatalf("expected 1 ClusterResourceBlacklist entry, got %d", len(proj.Spec.ClusterResourceBlacklist))
		}
		if proj.Spec.ClusterResourceBlacklist[0].Group != "*" || proj.Spec.ClusterResourceBlacklist[0].Kind != "*" {
			t.Error("expected wildcard ClusterResourceBlacklist")
		}
		if len(proj.Spec.ClusterResourceWhitelist) != 0 {
			t.Error("non-admin team should not have ClusterResourceWhitelist")
		}
	})

	t.Run("admin team gets ClusterResourceWhitelist", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		t.Setenv("CLUSTER_ADMIN_TEAMS", "admin-team,other-team")

		proj := createAppProj("admin-team")
		if len(proj.Spec.ClusterResourceWhitelist) != 1 {
			t.Fatalf("expected 1 ClusterResourceWhitelist entry, got %d", len(proj.Spec.ClusterResourceWhitelist))
		}
		if proj.Spec.ClusterResourceWhitelist[0].Group != "*" || proj.Spec.ClusterResourceWhitelist[0].Kind != "*" {
			t.Error("expected wildcard ClusterResourceWhitelist")
		}
		if len(proj.Spec.ClusterResourceBlacklist) != 0 {
			t.Error("admin team should not have ClusterResourceBlacklist")
		}
	})

	t.Run("PUBLIC_REPOS populates SourceRepos", func(t *testing.T) {
		NamespaceCache = newCache()
		t.Setenv("PUBLIC_REPOS", "https://github.com/org/repo1,https://github.com/org/repo2")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		if len(proj.Spec.SourceRepos) != 2 {
			t.Fatalf("expected 2 source repos, got %d", len(proj.Spec.SourceRepos))
		}
		if proj.Spec.SourceRepos[0] != "https://github.com/org/repo1" {
			t.Errorf("expected first repo 'https://github.com/org/repo1', got %q", proj.Spec.SourceRepos[0])
		}
		if proj.Spec.SourceRepos[1] != "https://github.com/org/repo2" {
			t.Errorf("expected second repo 'https://github.com/org/repo2', got %q", proj.Spec.SourceRepos[1])
		}
	})

	t.Run("empty PUBLIC_REPOS results in nil SourceRepos", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		if proj.Spec.SourceRepos != nil {
			t.Errorf("expected nil SourceRepos, got %v", proj.Spec.SourceRepos)
		}
	})

	t.Run("destinations come from NamespaceCache", func(t *testing.T) {
		NamespaceCache = newCache()
		NamespaceCache.JoinProject("ns-a", "team-x")
		NamespaceCache.JoinProject("ns-b", "team-x")
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		if len(proj.Spec.Destinations) != 2 {
			t.Fatalf("expected 2 destinations, got %d", len(proj.Spec.Destinations))
		}
		nsList := make(map[string]bool)
		for _, d := range proj.Spec.Destinations {
			nsList[d.Namespace] = true
			if d.Server != "*" {
				t.Errorf("expected server '*', got %q", d.Server)
			}
		}
		if !nsList["ns-a"] || !nsList["ns-b"] {
			t.Errorf("expected destinations to contain ns-a and ns-b, got %v", nsList)
		}
	})

	t.Run("source namespaces come from NamespaceCache", func(t *testing.T) {
		NamespaceCache = newCache()
		NamespaceCache.TrustSource("source-ns", "team-x")
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		if len(proj.Spec.SourceNamespaces) != 1 {
			t.Fatalf("expected 1 source namespace, got %d", len(proj.Spec.SourceNamespaces))
		}
		if proj.Spec.SourceNamespaces[0] != "source-ns" {
			t.Errorf("expected source namespace 'source-ns', got %q", proj.Spec.SourceNamespaces[0])
		}
	})

	t.Run("no destinations when cache is empty for team", func(t *testing.T) {
		NamespaceCache = newCache()
		os.Unsetenv("PUBLIC_REPOS")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		if len(proj.Spec.Destinations) != 0 {
			t.Errorf("expected 0 destinations, got %d", len(proj.Spec.Destinations))
		}
	})

	t.Run("PUBLIC_REPOS with spaces are trimmed", func(t *testing.T) {
		NamespaceCache = newCache()
		t.Setenv("PUBLIC_REPOS", " repo1 , repo2 ")
		os.Unsetenv("CLUSTER_ADMIN_TEAMS")

		proj := createAppProj("team-x")
		if len(proj.Spec.SourceRepos) != 2 {
			t.Fatalf("expected 2 source repos, got %d", len(proj.Spec.SourceRepos))
		}
		if proj.Spec.SourceRepos[0] != "repo1" {
			t.Errorf("expected trimmed 'repo1', got %q", proj.Spec.SourceRepos[0])
		}
		if proj.Spec.SourceRepos[1] != "repo2" {
			t.Errorf("expected trimmed 'repo2', got %q", proj.Spec.SourceRepos[1])
		}
	})
}

func TestLabelToProjects(t *testing.T) {
	tests := []struct {
		name     string
		label    string
		expected []string
	}{
		{
			name:     "single project",
			label:    "team-a",
			expected: []string{"team-a"},
		},
		{
			name:     "multiple projects separated by dots",
			label:    "team-a.team-b.team-c",
			expected: []string{"team-a", "team-b", "team-c"},
		},
		{
			name:     "empty string",
			label:    "",
			expected: []string{},
		},
		{
			name:     "trailing dots ignored",
			label:    "team-a.",
			expected: []string{"team-a"},
		},
		{
			name:     "leading dots ignored",
			label:    ".team-a",
			expected: []string{"team-a"},
		},
		{
			name:     "consecutive dots ignored",
			label:    "team-a..team-b",
			expected: []string{"team-a", "team-b"},
		},
		{
			name:     "only dots",
			label:    "...",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := labelToProjects(tt.label)
			if result.Len() != len(tt.expected) {
				t.Fatalf("expected %d projects, got %d", len(tt.expected), result.Len())
			}
			for _, exp := range tt.expected {
				if !result.Contains(exp) {
					t.Errorf("expected result to contain %q", exp)
				}
			}
		})
	}
}
