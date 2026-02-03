package controller

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/snapp-incubator/argocd-complementary-operator/pkg/nameset"

	"golang.org/x/crypto/bcrypt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	userArgocdNS           = "user-argocd"
	userArgocdStaticUserCM = "argocd-cm"
	userArgocdSecret       = "argocd-secret"
	argocdUserFinalizer    = "argocd.snappcloud.io/finalizer"

	// move a namespace into a argocd appproj using the label.
	// for example argocd.snappcloud.io/appproj: snapppay means snapppay argo project
	// can deploy resources into the labeled namespace.
	ProjectsLabel = "argocd.snappcloud.io/appproj"
	// namespace can host argo application for the argocd appproj using the label.
	// for example argocd.snappcloud.io/source: snapppay means argo applications
	// in the labeled namespace can belongs to the snapppay argo project.
	SourceLabel = "argocd.snappcloud.io/source"
)

var NamespaceCache = &SafeNsCache{
	lock:        sync.Mutex{},
	projects:    nil,
	namespaces:  nil,
	initialized: false,
}

func isCRDInstalled(gvk schema.GroupVersionKind) bool {
	cfg := ctrl.GetConfigOrDie()
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return false
	}

	resources, err := discoveryClient.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return false // Group/Version not available
	}

	for _, r := range resources.APIResources {
		if r.Kind == gvk.Kind {
			return true
		}
	}
	return false
}

func isTeamClusterAdmin(team string, clusterAdminList []string) bool {
	for _, tm := range clusterAdminList {
		if team == tm {
			return true
		}
	}
	return false
}

func createAppProj(team string) *argov1alpha1.AppProject {
	desiredNamespaces := NamespaceCache.GetNamespaces(team)
	destinations := make([]argov1alpha1.ApplicationDestination, 0, len(desiredNamespaces))

	for _, desiredNamespace := range desiredNamespaces {
		destinations = append(destinations, argov1alpha1.ApplicationDestination{
			Namespace: desiredNamespace,
			Server:    "*",
		})
	}

	sources := NamespaceCache.GetSources(team)

	// Get public repos
	repo_env := os.Getenv("PUBLIC_REPOS")
	repo_list := strings.Split(repo_env, ",")

	// Get cluster scoped teams
	team_env := os.Getenv("CLUSTER_ADMIN_TEAMS")
	team_list := strings.Split(team_env, ",")

	includeAllGroupKind := []metav1.GroupKind{
		{
			Group: "*",
			Kind:  "*",
		},
	}

	// TODO: Update to use labels instead of extracting name
	appProj := &argov1alpha1.AppProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      team,
			Namespace: userArgocdNS,
		},
		Spec: argov1alpha1.AppProjectSpec{
			SourceRepos:      repo_list,
			Destinations:     destinations,
			SourceNamespaces: sources,
			NamespaceResourceBlacklist: []metav1.GroupKind{
				{
					Group: "",
					Kind:  "LimitRange",
				},
			},
			Roles: []argov1alpha1.ProjectRole{
				{
					Groups: []string{team + "-admin", team + "-admin" + "-ci"},
					Name:   team + "-admin",
					Policies: []string{
						"p, proj:" + team + ":" + team + "-admin, applications, *, " + team + "/*, allow",
						"p, proj:" + team + ":" + team + "-admin, repositories, *, " + team + "/*, allow",
						"p, proj:" + team + ":" + team + "-admin, exec, create, " + team + "/*, allow",
					},
				},
				{
					Groups: []string{
						team + "-admin",
						team + "-admin" + "-ci",
						team + "-view",
						team + "-view" + "-ci",
					},
					Name: team + "-view",
					Policies: []string{
						"p, proj:" + team + ":" + team + "-view, applications, get, " + team + "/*, allow",
						"p, proj:" + team + ":" + team + "-view, repositories, get, " + team + "/*, allow",
						"p, proj:" + team + ":" + team + "-view, logs, get, " + team + "/*, allow",
					},
				},
			},
		},
	}

	if isTeamClusterAdmin(team, team_list) {
		appProj.Spec.ClusterResourceWhitelist = includeAllGroupKind
	} else {
		appProj.Spec.ClusterResourceBlacklist = includeAllGroupKind
	}
	return appProj
}

func hashPassword(password string) (string, error) {
	if len(password) > 72 {
		return "", fmt.Errorf("password exceeds bcrypt maximum length of 72 bytes")
	}
	// TODO: Apply this
	// if len(password) < 8 {
	// 	return "", fmt.Errorf("password must be at least 8 characters")
	// }
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		return "", fmt.Errorf("bcrypt failed: %w", err)
	}
	// Validate hash is not empty
	if string(bytes) == "" {
		return "", fmt.Errorf("password hash is empty")
	}
	return string(bytes), err
}

func mergeStringSlices(a, b []string) []string {
	ns := nameset.New[string]()

	// Add A items to the nameset
	for _, item := range a {
		ns.Add(item)
	}

	// Add B items to the nameset (duplicates are automatically handled)
	for _, item := range b {
		ns.Add(item)
	}

	// Convert back to a slice
	result := make([]string, 0, ns.Len())
	for item := range ns.All() {
		result = append(result, item)
	}
	slices.Sort(result)
	return result
}
