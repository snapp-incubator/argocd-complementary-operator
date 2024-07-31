/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	mu     sync.Mutex
}

const (
	// move a namespace into a argocd appproj using the label.
	projectsLabel = "argocd.snappcloud.io/appproj"
	// namespace can host argo application for the argocd appproj using the label.
	sourceLabel = "argocd.snappcloud.io/source"

	baseNs = "user-argocd"
)

var NamespaceCache = &SafeNsCache{
	lock:        sync.Mutex{},
	projects:    nil,
	namespaces:  nil,
	initialized: false,
}

type Nameset map[string]struct{}

type (
	SafeNsCache struct {
		lock        sync.Mutex
		projects    map[string]Nameset
		namespaces  map[string]Nameset
		sources     map[string]Nameset
		initialized bool
	}
)

// Trust given source in the given project.
func (c *SafeNsCache) TrustSource(ns, proj string) {
	if _, ok := c.sources[proj]; !ok {
		c.sources[proj] = make(Nameset)
	}

	c.sources[proj][ns] = struct{}{}
}

// UnTrust given source in the given project.
func (c *SafeNsCache) UnTrustSource(ns, proj string) {
	delete(c.sources[proj], ns)
}

// JoinProject will add given namespace into given project.
// It will update both upward and downward edges in AppProjectNameset and NamespaceNameset.
func (c *SafeNsCache) JoinProject(ns, proj string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if _, ok := c.projects[ns]; !ok {
		c.projects[ns] = make(Nameset)
	}

	if _, ok := c.namespaces[proj]; !ok {
		c.namespaces[proj] = make(Nameset)
	}

	c.projects[ns][proj] = struct{}{}
	c.namespaces[proj][ns] = struct{}{}
}

// LeaveProject will remove given namespace from given project.
// It will update both upward and downward edges in AppProjectNameset and NamespaceNameset
func (c *SafeNsCache) LeaveProject(ns, proj string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.projects[ns], proj)
	delete(c.namespaces[proj], ns)
}

// GetProjects will return name-set for given Namespace name. It creates a copy
// from current name-set.
func (c *SafeNsCache) GetProjects(ns string) Nameset {
	c.lock.Lock()
	defer c.lock.Unlock()

	r := make(Nameset)

	for k := range c.projects[ns] {
		r[k] = struct{}{}
	}

	return r
}

// GetNamespaces will return name-set for given AppProject name. It creates a copy
// from current name-set.
func (c *SafeNsCache) GetNamespaces(proj string) Nameset {
	c.lock.Lock()
	defer c.lock.Unlock()

	r := make(Nameset)

	for k := range c.namespaces[proj] {
		r[k] = struct{}{}
	}

	return r
}

// GetSources will return name-set for given AppProject name. It creates a copy
// from current name-set.
func (c *SafeNsCache) GetSources(proj string) Nameset {
	c.lock.Lock()
	defer c.lock.Unlock()

	r := make(Nameset)

	for k := range c.sources[proj] {
		r[k] = struct{}{}
	}

	return r
}

func (c *SafeNsCache) InitOrPass(r *NamespaceReconciler, ctx context.Context) error {
	if c.initialized {
		return nil
	}
	defer func() {
		c.initialized = true
	}()

	appProjList := &argov1alpha1.AppProjectList{}
	err := r.List(ctx, appProjList,
		&client.ListOptions{Namespace: baseNs},
	)
	if err != nil {
		return err
	}

	c.namespaces = make(map[string]Nameset)
	c.projects = make(map[string]Nameset)

	for _, apItem := range appProjList.Items {
		for _, dest := range apItem.Spec.Destinations {
			if apItem.Name == "default" {
				continue
			}
			c.JoinProject(dest.Namespace, apItem.Name)
		}
	}

	return nil
}

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=namespaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=namespaces/finalizers,verbs=update
//+kubebuilder:rbac:groups=argoproj.io,resources=appprojects,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Namespace object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	logger := log.FromContext(ctx)
	logger.Info("Reconciling Namespace", "Namespace", fmt.Sprint(req.NamespacedName))

	err := NamespaceCache.InitOrPass(r, ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	ns := &corev1.Namespace{}

	// First Fetch Phase
	if err := r.Get(ctx, req.NamespacedName, ns); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Namespace not found. Ignoring since object must be deleted", "Namespace", fmt.Sprint(req.NamespacedName))

			oldTeams := NamespaceCache.GetProjects(req.Name)
			if len(oldTeams) > 0 {
				for t := range oldTeams {
					if err := r.reconcileAppProject(ctx, logger, t); err != nil {
						logger.Error(err, "Failed to reconcile AppProject for not found resource error recovery", "AppProj.Name", fmt.Sprint(t))

						continue
					}

					logger.Info("Successfully reconciled AppProject for not found resource error recovery", "AppProj.Name", fmt.Sprint(t))
				}
			}

			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}

		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get Namespace Resource, Requeuing the request", "Namespace", fmt.Sprint(req.NamespacedName))
		return ctrl.Result{}, err
	}

	sourcesToAdd := labelToProjects(
		ns.GetLabels()[sourceLabel],
	)
	currentSources := NamespaceCache.GetSources(req.Name)

	for source := range currentSources {
		if _, ok := sourcesToAdd[source]; !ok {
			logger.Info("Updating Cache: Removing source from AppProject", "Namespace", req.Name, "AppProj.Name", source)
			NamespaceCache.UnTrustSource(req.Name, source)
		} else {
			delete(sourcesToAdd, source)
			logger.Info("Updating Cache: Adding source to AppProject:", "Namespace", req.Name, "AppProj.Name", source)
			NamespaceCache.TrustSource(req.Name, source)
		}
	}

	// update cache by adding new team to the cache
	for t := range sourcesToAdd {
		NamespaceCache.TrustSource(req.Name, t)
	}

	projectsToAdd := labelToProjects(
		ns.GetLabels()[projectsLabel],
	)
	projectsToRemove := make([]string, 0)
	currentProjects := NamespaceCache.GetProjects(req.Name)

	for t := range currentProjects {
		if _, ok := projectsToAdd[t]; !ok {
			projectsToRemove = append(projectsToRemove, t)
			logger.Info("Updating Cache: Removing NS from AppProject", "Namespace", req.Name, "AppProj.Name", t)
			NamespaceCache.LeaveProject(req.Name, t)
		} else {
			delete(projectsToAdd, t)
			logger.Info("Updating Cache: Adding NS to AppProject:", "Namespace", req.Name, "AppProj.Name", t)
			NamespaceCache.JoinProject(req.Name, t)
		}
	}

	// update cache: adding new team to cache
	for t := range projectsToAdd {
		NamespaceCache.JoinProject(req.Name, t)
	}

	var reconciliationErrors *multierror.Error
	// add ns to new app-projects
	logger.Info("Reconciling New Teams", "len", len(projectsToAdd))
	for t := range projectsToAdd {
		logger.Info("Reconciling AppProject to add new namespaces", "AppProj.Name", t)
		if err := r.reconcileAppProject(ctx, logger, t); err != nil {
			logger.Error(err, "Error while Reconciling AppProject", "AppProj.Name", t)
			reconciliationErrors = multierror.Append(reconciliationErrors, err)
		}
	}

	// removing ns from old projects
	logger.Info("Reconciling Old Teams", "len", len(projectsToAdd))
	for _, t := range projectsToRemove {
		logger.Info("Reconciling AppProject as on old member", "AppProj.Name", t)
		err = r.reconcileAppProject(ctx, logger, t)
		if err != nil {
			logger.Error(err, "Error while Reconciling AppProject", "AppProj.Name", t)
			reconciliationErrors = multierror.Append(reconciliationErrors, err)
		}
	}

	return ctrl.Result{}, reconciliationErrors.ErrorOrNil()
}

func (r *NamespaceReconciler) reconcileAppProject(ctx context.Context, logger logr.Logger, team string) error {
	appProj, err := r.createAppProj(team)
	if err != nil {
		return fmt.Errorf("error generating AppProj manifest: %v", err)
	}

	// Check if AppProj does not exist and create a new one
	found := &argov1alpha1.AppProject{}
	if err := r.Get(ctx, types.NamespacedName{Name: team, Namespace: baseNs}, found); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating AppProj", "AppProj.Name", team)
			if err := r.Create(ctx, appProj); err != nil {
				return fmt.Errorf("error creating AppProj: %v", err)
			}

			return nil
		} else {
			return fmt.Errorf("error getting AppProj: %v", err)
		}
	}

	appProj.Spec.SourceRepos = appendRepos(appProj.Spec.SourceRepos, found.Spec.SourceRepos)

	// If AppProj already exist, check if it is deeply equal with desrired state
	if !reflect.DeepEqual(appProj.Spec, found.Spec) {
		logger.Info("Founded AppProj is not equad to desired one, doing the upgrade", "AppProj.Name", team)

		found.Spec = appProj.Spec

		if err := r.Update(ctx, found); err != nil {
			return fmt.Errorf("error updating AppProj: %v", err)
		}
	}

	return nil
}

func (r *NamespaceReconciler) createAppProj(team string) (*argov1alpha1.AppProject, error) {
	desiredNamespaces := NamespaceCache.GetNamespaces(team)

	destinations := []argov1alpha1.ApplicationDestination{}

	for desiredNamespace := range desiredNamespaces {
		destinations = append(destinations, argov1alpha1.ApplicationDestination{
			Namespace: desiredNamespace,
			Server:    "https://kubernetes.default.svc",
		})
	}

	sources := NamespaceCache.GetSources(team)

	sourceNamespaces := []string{}

	for source := range sources {
		sourceNamespaces = append(sourceNamespaces, source)
	}

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

	appProj := &argov1alpha1.AppProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      team,
			Namespace: baseNs,
		},
		Spec: argov1alpha1.AppProjectSpec{
			SourceRepos:  repo_list,
			Destinations: destinations,
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
					},
				},
				{
					Groups: []string{team + "-view", team + "-view" + "-ci"},
					Name:   team + "-view",
					Policies: []string{
						"p, proj:" + team + ":" + team + "-view, applications, *, " + team + "/get, allow",
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

	return appProj, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}

// Compare source repos and public repos
func appendRepos(repo_list []string, found_repos []string) []string {
	check := make(map[string]bool)
	mixrepos := append(repo_list, found_repos...)
	res := make([]string, 0)
	for _, repo := range mixrepos {
		check[repo] = true
	}

	for repo := range check {
		res = append(res, repo)
	}

	return res
}

// labelToProjects will convert period separated label value to actual nameset.
func labelToProjects(l string) Nameset {
	result := make(Nameset)

	if l == "" {
		return result
	}

	for _, s := range strings.Split(l, ".") {
		if s != "" {
			result[s] = struct{}{}
		}
	}

	return result
}

func isTeamClusterAdmin(team string, clusterAdminList []string) bool {
	for _, tm := range clusterAdminList {
		if team == tm {
			return true
		}
	}
	return false
}
