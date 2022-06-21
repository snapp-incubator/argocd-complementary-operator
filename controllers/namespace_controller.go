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
	"reflect"
	"sync"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-logr/logr"
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
}

const (
	teamLabel = "snappcloud.io/team"
	baseNs    = "user-argocd"
)

var safeNsCache = &SafeNsCache{m: map[string]string{}}

type SafeNsCache struct {
	mu sync.Mutex
	m  map[string]string
}

// Inc increments the counter for the given key.
func (c *SafeNsCache) Set(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Lock so only one goroutine at a time can access the map c.m.
	c.m[k] = v

}

func (c *SafeNsCache) Load(k string) (v string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok = c.m[k]
	return v, ok
}

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=namespaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=namespaces/finalizers,verbs=update

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
	logger := log.FromContext(ctx)
	logger.Info(fmt.Sprint(req.NamespacedName))

	ns := &corev1.Namespace{}
	err := r.Get(ctx, req.NamespacedName, ns)
	if err != nil {
		if errors.IsNotFound(err) {

			oldTeam, _ := safeNsCache.Load(req.Name)
			fmt.Println("oldteam", oldTeam)
			if oldTeam != "" {
				err = r.reconcileAppProject(ctx, logger, oldTeam)
				if err != nil {
					return ctrl.Result{}, err
				}
			}
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("Resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get Namespace")
		return ctrl.Result{}, err
	}

	team := ns.GetLabels()[teamLabel]
	oldTeam, _ := safeNsCache.Load(req.Name)
	fmt.Println("oldteam", oldTeam)
	safeNsCache.Set(req.Name, team)

	if team == "snappcloud" {
		return ctrl.Result{}, nil

	}

	if team == "" {

		// newly created ns, without any team
		if oldTeam == "" {
			return ctrl.Result{}, nil
		}

		// unlabeled ns, but it had an old team
		if oldTeam != "" {
			err = r.reconcileAppProject(ctx, logger, oldTeam)
			if err != nil {
				return ctrl.Result{}, err
			}
		}

	}

	// add ns to new team
	err = r.reconcileAppProject(ctx, logger, team)
	if err != nil {
		return ctrl.Result{}, err
	}

	if oldTeam == "" || oldTeam == team {
		return ctrl.Result{}, nil
	}

	// also reconicle oldTeam to remove ns from oldTeam
	err = r.reconcileAppProject(ctx, logger, oldTeam)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NamespaceReconciler) reconcileAppProject(ctx context.Context, logger logr.Logger, team string) error {
	appProj, err := r.createAppProj(ctx, team)
	if err != nil {
		return fmt.Errorf("Error generating AppProj manifest: %v", err)
	}

	// Check if AppProj does not exist and create a new one
	found := &argov1alpha1.AppProject{}
	err = r.Get(ctx, types.NamespacedName{Name: team, Namespace: baseNs}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Creating AppProj", "AppProj.Name", team)
		err = r.Create(ctx, appProj)
		if err != nil {
			return fmt.Errorf("Error creating AppProj: %v", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("Error getting AppProj: %v", err)
	}

	// If AppProj already exist, check if it is deeply equal with desrired state
	if !reflect.DeepEqual(appProj.Spec, found.Spec) {
		logger.Info("Updating AppProj", "AppProj.Name", team)
		found.Spec = appProj.Spec
		err := r.Update(ctx, found)
		if err != nil {
			return fmt.Errorf("Error updating AppProj: %v", err)
		}
	}
	return nil
}

func (r *NamespaceReconciler) createAppProj(ctx context.Context, team string) (*argov1alpha1.AppProject, error) {
	fmt.Println("run reconcile on ", team)
	listOpts := []client.ListOption{
		client.MatchingLabels(map[string]string{
			teamLabel: team,
		}),
	}
	nsList := &corev1.NamespaceList{}
	err := r.List(ctx, nsList, listOpts...)
	if err != nil {
		return nil, err
	}

	destList := []argov1alpha1.ApplicationDestination{}

	for _, nsItem := range nsList.Items {
		destList = append(destList, argov1alpha1.ApplicationDestination{
			Namespace: nsItem.Name,
			Server:    "https://kubernetes.default.svc",
		})

	}

	appProj := &argov1alpha1.AppProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      team,
			Namespace: baseNs,
		},
		Spec: argov1alpha1.AppProjectSpec{
			SourceRepos:  []string{""},
			Destinations: destList,
			ClusterResourceBlacklist: []metav1.GroupKind{
				{
					Group: "*",
					Kind:  "*",
				},
			},
			NamespaceResourceBlacklist: []metav1.GroupKind{
				{
					Group: "",
					Kind:  "ResourceQuota",
				},
				{
					Group: "",
					Kind:  "LimitRange",
				},
			},
			Roles: []argov1alpha1.ProjectRole{
				{
					Groups: []string{team + "-admin"},
					Name:   team + "-admin",
					Policies: []string{
						"p, proj:" + team + ":" + team + "-admin, applications, *, " + team + "/*, allow",
					},
				},
				{
					Groups: []string{team + "-view"},
					Name:   team + "-view",
					Policies: []string{
						"p, proj:" + team + ":" + team + "-view, applications, *, " + team + "/get, allow",
					},
				},
			},
		},
	}

	return appProj, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}
