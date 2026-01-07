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

package controller

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	"github.com/snapp-incubator/argocd-complementary-operator/pkg/nameset"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

type SafeNsCache struct {
	lock        sync.Mutex
	projects    map[string]nameset.Nameset[string]
	namespaces  map[string]nameset.Nameset[string]
	sources     map[string]nameset.Nameset[string]
	initialized bool
}

// Trust given source in the given project.
func (c *SafeNsCache) TrustSource(ns, proj string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if _, ok := c.sources[ns]; !ok {
		c.sources[ns] = nameset.New[string]()
	}

	c.sources[ns].Add(proj)
}

// UnTrust given source in the given project.
func (c *SafeNsCache) UnTrustSource(ns, proj string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.sources[ns].Remove(proj)
}

// JoinProject will add given namespace into given project.
// It will update both upward and downward edges in AppProjectNameset and NamespaceNameset.
func (c *SafeNsCache) JoinProject(ns, proj string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if _, ok := c.projects[ns]; !ok {
		c.projects[ns] = nameset.New[string]()
	}

	if _, ok := c.namespaces[proj]; !ok {
		c.namespaces[proj] = nameset.New[string]()
	}

	c.projects[ns].Add(proj)
	c.namespaces[proj].Add(ns)
}

// LeaveProject will remove given namespace from given project.
// It will update both upward and downward edges in AppProjectNameset and NamespaceNameset
func (c *SafeNsCache) LeaveProject(ns, proj string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.projects[ns].Remove(proj)
	c.namespaces[proj].Remove(ns)
}

// GetProjects will return projects for given Namespace name. It creates a copy
// from current name-set.
func (c *SafeNsCache) GetProjects(ns string) []string {
	c.lock.Lock()
	defer c.lock.Unlock()

	r := make([]string, 0)

	for k := range c.projects[ns].All() {
		r = append(r, k)
	}

	return r
}

// GetNamespaces will return namespaces for given AppProject name. It creates a copy
// from current name-set.
func (c *SafeNsCache) GetNamespaces(proj string) []string {
	c.lock.Lock()
	defer c.lock.Unlock()

	r := make([]string, 0)

	for k := range c.namespaces[proj].All() {
		r = append(r, k)
	}

	return r
}

// GetSources will return sources for given AppProject name. It creates a copy
// from current name-set.
func (c *SafeNsCache) GetSources(proj string) []string {
	c.lock.Lock()
	defer c.lock.Unlock()

	r := make([]string, 0)

	for k, v := range c.sources {
		if v.Contains(proj) {
			r = append(r, k)
		}
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

	c.namespaces = make(map[string]nameset.Nameset[string])
	c.projects = make(map[string]nameset.Nameset[string])
	c.sources = make(map[string]nameset.Nameset[string])

	if err := r.List(ctx, appProjList,
		&client.ListOptions{Namespace: userArgocdNS},
	); err != nil {
		return err
	}

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
	logger.Info("reconciling namespace", "namespace", req.NamespacedName)

	if err := NamespaceCache.InitOrPass(r, ctx); err != nil {
		return ctrl.Result{}, err
	}

	ns := &corev1.Namespace{}

	// First Fetch Phase
	if err := r.Get(ctx, req.NamespacedName, ns); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("namespace not found. ignoring since object must be deleted", "namespace", req.NamespacedName)

			oldTeams := NamespaceCache.GetProjects(req.Name)
			if len(oldTeams) > 0 {
				for _, t := range oldTeams {
					if err := r.reconcileAppProject(ctx, logger, t); err != nil {
						logger.Error(err, "failed to reconcile appproject for not found resource error recovery", "name", t)
						continue
					}
					logger.Info("successfully reconciled appproject for not found resource error recovery", "name", t)
				}
			}

			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}

		// Error reading the object - requeue the request.
		logger.Error(err, "failed to get namespace resource, requeuing the request", "namespace", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// current namespace is trusted by the teams that are mentioned in the laebl.
	sourcesToAdd := labelToProjects(
		ns.GetLabels()[SourceLabel],
	)

	currentSources := NamespaceCache.GetSources(req.Name)

	for _, s := range currentSources {
		if !sourcesToAdd.Contains(s) {
			logger.Info("namespace cannot contain applications belongs to project", "namespace", req.Name, "project", s)
			NamespaceCache.UnTrustSource(req.Name, s)
		} else {
			sourcesToAdd.Remove(s)
			logger.Info("namespace can contain applications belongs to project", "namespace", req.Name, "project", s)
			NamespaceCache.TrustSource(req.Name, s)
		}
	}

	for s := range sourcesToAdd.All() {
		logger.Info("namespace can contain applications belongs to project", "namespace", req.Name, "source", s)
		NamespaceCache.TrustSource(req.Name, s)
	}

	// current namespace can be used by these argocd projects to deploy resources.
	projectsToAdd := labelToProjects(
		ns.GetLabels()[ProjectsLabel],
	)
	projectsToRemove := make([]string, 0)
	currentProjects := NamespaceCache.GetProjects(req.Name)

	for _, t := range currentProjects {
		if !projectsToAdd.Contains(t) {
			projectsToRemove = append(projectsToRemove, t)
			logger.Info("removing namespace from project destinations", "namespace", req.Name, "project", t)
			NamespaceCache.LeaveProject(req.Name, t)
		} else {
			projectsToAdd.Remove(t)
			logger.Info("adding namespace to project destinations", "namespace", req.Name, "project", t)
			NamespaceCache.JoinProject(req.Name, t)
		}
	}

	for t := range projectsToAdd.All() {
		logger.Info("adding namespace to project destinations", "namespace", req.Name, "project", t)
		NamespaceCache.JoinProject(req.Name, t)
	}

	var reconciliationErrors *multierror.Error

	logger.Info("reconciling (by adding) projects/teams", "len", projectsToAdd.Len())

	for t := range projectsToAdd.All() {
		logger.Info("reconciling (by adding) project/team", "name", t)

		if err := r.reconcileAppProject(ctx, logger, t); err != nil {
			logger.Error(err, "error while reconciling project", "name", t)
			reconciliationErrors = multierror.Append(reconciliationErrors, err)
		}
	}

	logger.Info("reconciling (by removing) projects/teams", "len", len(projectsToRemove))

	for _, t := range projectsToRemove {
		logger.Info("reconciling (by removing) project/team", "name", t)

		if err := r.reconcileAppProject(ctx, logger, t); err != nil {
			logger.Error(err, "error while reconciling project", "name", t)
			reconciliationErrors = multierror.Append(reconciliationErrors, err)
		}
	}

	return ctrl.Result{}, reconciliationErrors.ErrorOrNil()
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}

// reconcileAppProject create an argocd project and change the current argocd project to be compatible with it.
// it is called everytime a label changed, so when you remove a policy or etc it will not be called.
func (r *NamespaceReconciler) reconcileAppProject(ctx context.Context, logger logr.Logger, team string) error {
	appProj := createAppProj(team)

	// Check if AppProject does not exist and create a new one
	found := &argov1alpha1.AppProject{}
	if err := r.Get(ctx, types.NamespacedName{Name: team, Namespace: userArgocdNS}, found); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("AppProject not found, skipping update (will be created by ArgocdUser)", "AppProject", team)
			return nil
		} else {
			return fmt.Errorf("Error getting AppProject: %w", err)
		}
	}

	// If AppProject already exist, check if it is deeply equal with desrired state on destinations and source namespaces
	if !reflect.DeepEqual(appProj.Spec.Destinations, found.Spec.Destinations) {
		logger.Info("Found AppProject Destinations is not equad to desired one, doing the upgrade", "AppProject", team)
		found.Spec.Destinations = appProj.Spec.Destinations
		if err := r.Update(ctx, found); err != nil {
			return fmt.Errorf("Error updating AppProject: %v", err)
		}
	}
	if !reflect.DeepEqual(appProj.Spec.SourceNamespaces, found.Spec.SourceNamespaces) {
		logger.Info("Founded AppProject SourceNamespaces is not equad to desired one, doing the upgrade", "AppProject", team)
		found.Spec.SourceNamespaces = appProj.Spec.SourceNamespaces
		if err := r.Update(ctx, found); err != nil {
			return fmt.Errorf("Error updating AppProject: %v", err)
		}
	}

	return nil
}

// labelToProjects will convert period separated label value to actual nameset.
func labelToProjects(l string) nameset.Nameset[string] {
	result := nameset.New[string]()

	if l == "" {
		return result
	}

	for _, s := range strings.Split(l, ".") {
		if s != "" {
			result.Add(s)
		}
	}

	return result
}
