package controller

import (
	"os"
	"strings"
	"sync"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/snapp-incubator/argocd-complementary-operator/pkg/nameset"

	"golang.org/x/crypto/bcrypt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	userArgocdNS           = "user-argocd"
	userArgocdStaticUserCM = "argocd-cm"
	userArgocdSecret       = "argocd-secret"
	argocdUserFinalizer    = "snappcloud.io/argocd-complementary-operator"

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

	destinations := []argov1alpha1.ApplicationDestination{}

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
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
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
	return result
}
