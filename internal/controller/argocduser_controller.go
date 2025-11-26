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
	b64 "encoding/base64"
	"encoding/json"
	"strings"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	userv1 "github.com/openshift/api/user/v1"
	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	userArgocdNS           = "user-argocd"
	userArgocdRbacPolicyCM = "argocd-rbac-cm"
	userArgocdStaticUserCM = "argocd-cm"
	userArgocdSecret       = "argocd-secret"
	argocdUserFinalizer    = "argocd.snappcloud.io/finalizer"
)

// ArgocdUserReconciler reconciles a ArgocdUser object
type ArgocdUserReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=argocd.snappcloud.io,resources=argocdusers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=argocd.snappcloud.io,resources=argocdusers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=argocd.snappcloud.io,resources=argocdusers/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups=user.openshift.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=argoproj.io,resources=appprojects,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the clus k8s.io/api closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ArgocdUser object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *ArgocdUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	argocduser := &argocduserv1alpha1.ArgocdUser{}
	err := r.Get(context.TODO(), req.NamespacedName, argocduser)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("ArgocdUser resource not found, might be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get argocduser")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if argocduser.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(argocduser, argocdUserFinalizer) {
			log.Info("ArgocdUser is being deleted, checking for active namespaces")

			// Check if there are namespaces still referencing this ArgocdUser
			hasActive, err := r.hasActiveNamespaces(ctx, argocduser.Name)
			if err != nil {
				log.Error(err, "Failed to check for active namespaces")
				return ctrl.Result{}, err
			}

			if hasActive {
				log.Info("Cannot delete ArgocdUser: namespaces still reference it", "argocduser", argocduser.Name)
				// Requeue to check again later
				return ctrl.Result{Requeue: true}, nil
			}

			// No active namespaces, proceed with cleanup
			log.Info("No active namespaces found, proceeding with cleanup")
			if err := r.cleanupArgocdUserResources(ctx, argocduser); err != nil {
				log.Error(err, "Failed to cleanup ArgocdUser resources")
				return ctrl.Result{}, err
			}

			// Remove finalizer
			controllerutil.RemoveFinalizer(argocduser, argocdUserFinalizer)
			if err := r.Update(ctx, argocduser); err != nil {
				log.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
			log.Info("Successfully removed finalizer and cleaned up resources")
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(argocduser, argocdUserFinalizer) {
		log.Info("Adding finalizer to ArgocdUser")
		controllerutil.AddFinalizer(argocduser, argocdUserFinalizer)
		if err := r.Update(ctx, argocduser); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		log.Info("Successfully added finalizer")
		return ctrl.Result{}, nil
	}

	if err := r.createArgocdStaticUser(ctx, req, argocduser, "admin", argocduser.Spec.Admin.CIPass, argocduser.Spec.Admin.Users); err != nil {
		log.Error(err, "Failed create argocd static user admin")
		return ctrl.Result{}, err
	}

	if err := r.createArgocdStaticUser(ctx, req, argocduser, "view", argocduser.Spec.View.CIPass, argocduser.Spec.View.Users); err != nil {
		log.Error(err, "Failed create argocd static user view")
		return ctrl.Result{}, err
	}

	err = r.AddArgocdRBACPolicy(ctx, argocduser)
	if err != nil {
		log.Error(err, "Failed to add argocd rbac policy")
		return ctrl.Result{}, err
	}

	// Create or update AppProject with RBAC configuration
	if err := r.ReconcileAppProject(ctx, argocduser); err != nil {
		log.Error(err, "Failed to reconcile AppProject")
		return ctrl.Result{}, err
	}

	// Manage OpenShift groups for admin users (both admin and view groups)
	if err := r.AddArgoUsersToGroup(ctx, argocduser, "admin", argocduser.Spec.Admin.Users); err != nil {
		log.Error(err, "Failed to add admin users to admin group")
		return ctrl.Result{}, err
	}
	// Admin users also need to be in view group for role aggregation
	if err := r.AddArgoUsersToGroup(ctx, argocduser, "view", argocduser.Spec.Admin.Users); err != nil {
		log.Error(err, "Failed to add admin users to view group")
		return ctrl.Result{}, err
	}

	// Manage OpenShift groups for view users (only view group)
	if err := r.AddArgoUsersToGroup(ctx, argocduser, "view", argocduser.Spec.View.Users); err != nil {
		log.Error(err, "Failed to add view users to view group")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ArgocdUserReconciler) createArgocdStaticUser(
	ctx context.Context,
	_ ctrl.Request,
	argocduser *argocduserv1alpha1.ArgocdUser,
	roleName string,
	ciPass string,
	argoUsers []string,
) error {
	if err := r.UpdateUserArgocdConfig(ctx, argocduser, roleName, ciPass); err != nil {
		return err
	}

	if err := r.AddArgoUsersToGroup(ctx, argocduser, roleName, argoUsers); err != nil {
		return err
	}

	return nil
}

func (r *ArgocdUserReconciler) UpdateUserArgocdConfig(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser, roleName string, ciPass string) error {
	log := log.FromContext(ctx)
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: userArgocdStaticUserCM, Namespace: userArgocdNS}, configMap)
	if err != nil {
		log.Error(err, "Failed to get configMap")
		return err
	}
	patch := client.MergeFrom(configMap.DeepCopy())
	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data["accounts."+argocduser.Name+"-"+roleName+"-ci"] = "apiKey,login"
	err = r.Patch(ctx, configMap, patch)
	if err != nil {
		log.Error(err, "Failed to patch cm")
		return err
	}

	hash, _ := HashPassword(ciPass) // ignore error for the sake of simplicity
	encodedPass := b64.StdEncoding.EncodeToString([]byte(hash))

	staticPassword := map[string]map[string]string{
		"data": {
			"accounts." + argocduser.Name + "-" + roleName + "-ci.password": encodedPass,
		},
	}
	staticPassByte, _ := json.Marshal(staticPassword)

	err = r.Patch(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: userArgocdNS,
			Name:      userArgocdSecret,
		},
	}, client.RawPatch(types.StrategicMergePatchType, staticPassByte))
	if err != nil {
		log.Error(err, "Failed to patch secret")
		return err
	}
	return nil
}

func (r *ArgocdUserReconciler) AddArgoUsersToGroup(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser, roleName string, argoUsers []string) error {
	log := log.FromContext(ctx)
	group := &userv1.Group{}
	groupName := argocduser.Name + "-" + roleName
	err := r.Get(ctx, types.NamespacedName{Name: groupName}, group)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Failed get group, going to create group")
			group = &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{
					Name: groupName,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "argocd.snappcloud.io/v1alpha1",
							Kind:               "ArgocdUser",
							Name:               argocduser.Name,
							UID:                argocduser.UID,
							Controller:         ptr.To(true),
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
				Users: argoUsers,
			}
			err = r.Create(ctx, group)
			if err != nil {
				log.Error(err, "Failed to create group")
				return err
			}
			log.Info("Successfully created group with users")
			return nil
		}
		return err
	}

	// Merge users: keep existing users and add new ones
	existingUsers := make(map[string]bool)
	for _, user := range group.Users {
		existingUsers[user] = true
	}
	for _, user := range argoUsers {
		if !existingUsers[user] {
			group.Users = append(group.Users, user)
		}
	}

	err = r.Update(ctx, group)
	if err != nil {
		log.Error(err, "Failed to update group")
		return err
	}
	log.Info("Successfully updated group with users")
	return nil
}

// ReconcileAppProject creates or updates the AppProject with RBAC configuration
func (r *ArgocdUserReconciler) ReconcileAppProject(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser) error {
	log := log.FromContext(ctx)
	teamName := argocduser.Name

	// Create the desired AppProject
	appProj := &argov1alpha1.AppProject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      teamName,
			Namespace: userArgocdNS,
		},
		Spec: argov1alpha1.AppProjectSpec{
			// Note: Destinations, SourceNamespaces, and SourceRepos are managed by NamespaceReconciler
			Destinations:     []argov1alpha1.ApplicationDestination{},
			SourceNamespaces: []string{},
			SourceRepos:      []string{},
			Roles: []argov1alpha1.ProjectRole{
				{
					Groups: []string{teamName + "-admin", teamName + "-admin-ci"},
					Name:   teamName + "-admin",
					Policies: []string{
						"p, proj:" + teamName + ":" + teamName + "-admin, applications, *, " + teamName + "/*, allow",
						"p, proj:" + teamName + ":" + teamName + "-admin, repositories, *, " + teamName + "/*, allow",
						"p, proj:" + teamName + ":" + teamName + "-admin, exec, create, " + teamName + "/*, allow",
					},
				},
				{
					// View role includes both admin and view groups for role aggregation
					Groups: []string{teamName + "-admin", teamName + "-admin-ci", teamName + "-view", teamName + "-view-ci"},
					Name:   teamName + "-view",
					Policies: []string{
						"p, proj:" + teamName + ":" + teamName + "-view, applications, get, " + teamName + "/*, allow",
						"p, proj:" + teamName + ":" + teamName + "-view, repositories, get, " + teamName + "/*, allow",
						"p, proj:" + teamName + ":" + teamName + "-view, logs, get, " + teamName + "/*, allow",
					},
				},
			},
		},
	}

	// Set OwnerReference to enable garbage collection
	if err := controllerutil.SetControllerReference(argocduser, appProj, r.Scheme); err != nil {
		log.Error(err, "Failed to set OwnerReference on AppProject")
		return err
	}

	// Check if AppProject already exists
	found := &argov1alpha1.AppProject{}
	err := r.Get(ctx, types.NamespacedName{Name: teamName, Namespace: userArgocdNS}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating new AppProject", "name", teamName)
			if err := r.Create(ctx, appProj); err != nil {
				log.Error(err, "Failed to create AppProject")
				return err
			}
			log.Info("Successfully created AppProject")
			return nil
		}
		log.Error(err, "Failed to get AppProject")
		return err
	}

	// Update existing AppProject - only update Roles, preserve Destinations, SourceNamespaces, and SourceRepos
	// These fields are managed by NamespaceReconciler
	found.Spec.Roles = appProj.Spec.Roles
	if err := r.Update(ctx, found); err != nil {
		log.Error(err, "Failed to update AppProject")
		return err
	}
	log.Info("Successfully updated AppProject RBAC")
	return nil
}

func (r *ArgocdUserReconciler) AddArgocdRBACPolicy(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser) error {
	log := log.FromContext(ctx)
	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: userArgocdRbacPolicyCM, Namespace: userArgocdNS}, found)
	if err != nil {
		log.Error(err, "Failed to get cm")
		return err
	}

	// TODO: Enhance this, for example adding view roles to the admin not works!
	policies := []string{
		"p, role:common, clusters, get, *, allow",
		"g, " + argocduser.Name + "-admin-ci, role:common",
		"g, " + argocduser.Name + "-view-ci, role:common",
		"g, " + argocduser.Name + "-admin, role:common",
		"g, " + argocduser.Name + "-view, role:common",
	}

	// add argocd rbac policy
	is_changed := false
	for _, policy := range policies {
		duplicatePolicy := false
		for _, line := range strings.Split(found.Data["policy.csv"], "\n") {
			if policy == line {
				duplicatePolicy = true
			}
		}
		if !duplicatePolicy {
			found.Data["policy.csv"] = found.Data["policy.csv"] + "\n" + policy
			is_changed = true
		}
	}
	if is_changed {
		errRbac := r.Update(ctx, found)
		if errRbac != nil {
			log.Error(err, "error in updating argocd-rbac-cm")
			return err
		}
	}
	log.Info("Successfully added argocd rbac policy")
	return nil
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

// hasActiveNamespaces checks if any namespaces have labels referencing the ArgocdUser
func (r *ArgocdUserReconciler) hasActiveNamespaces(ctx context.Context, argocdUserName string) (bool, error) {
	log := log.FromContext(ctx)

	// List all namespaces
	namespaceList := &corev1.NamespaceList{}
	if err := r.List(ctx, namespaceList); err != nil {
		log.Error(err, "Failed to list namespaces")
		return false, err
	}

	// Check each namespace for labels referencing this ArgocdUser
	for _, ns := range namespaceList.Items {
		if appProjLabel, exists := ns.Labels["argocd.snappcloud.io/appproj"]; exists {
			// Parse multi-team labels (e.g., "team-a.team-b")
			teams := strings.Split(appProjLabel, ".")
			for _, team := range teams {
				if team == argocdUserName {
					log.Info("Found namespace referencing ArgocdUser", "namespace", ns.Name, "label", appProjLabel)
					return true, nil
				}
			}
		}
	}

	log.Info("No namespaces found referencing ArgocdUser", "argocduser", argocdUserName)
	return false, nil
}

// cleanupArgocdUserResources removes RBAC policies and static accounts for the ArgocdUser
func (r *ArgocdUserReconciler) cleanupArgocdUserResources(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser) error {
	log := log.FromContext(ctx)

	teamName := argocduser.Name

	// Remove RBAC policies from argocd-rbac-cm
	log.Info("Removing RBAC policies from argocd-rbac-cm")
	rbacConfigMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: userArgocdRbacPolicyCM, Namespace: userArgocdNS}, rbacConfigMap); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to get argocd-rbac-cm")
			return err
		}
		log.Info("argocd-rbac-cm not found, skipping RBAC cleanup")
	} else {
		// Remove all policies related to this team
		policyCsv := rbacConfigMap.Data["policy.csv"]
		lines := strings.Split(policyCsv, "\n")
		var newLines []string
		for _, line := range lines {
			// Keep lines that don't reference this team
			if !strings.Contains(line, teamName+"-admin-ci") &&
				!strings.Contains(line, teamName+"-view-ci") &&
				!strings.Contains(line, teamName+"-admin") &&
				!strings.Contains(line, teamName+"-view") {
				newLines = append(newLines, line)
			}
		}
		rbacConfigMap.Data["policy.csv"] = strings.Join(newLines, "\n")
		if err := r.Update(ctx, rbacConfigMap); err != nil {
			log.Error(err, "Failed to update argocd-rbac-cm")
			return err
		}
		log.Info("Successfully removed RBAC policies")
	}

	// Remove static accounts from argocd-cm
	log.Info("Removing static accounts from argocd-cm")
	argocdConfigMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: userArgocdStaticUserCM, Namespace: userArgocdNS}, argocdConfigMap); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to get argocd-cm")
			return err
		}
		log.Info("argocd-cm not found, skipping account cleanup")
	} else {
		delete(argocdConfigMap.Data, "accounts."+teamName+"-admin-ci")
		delete(argocdConfigMap.Data, "accounts."+teamName+"-view-ci")
		if err := r.Update(ctx, argocdConfigMap); err != nil {
			log.Error(err, "Failed to update argocd-cm")
			return err
		}
		log.Info("Successfully removed static accounts")
	}

	// Remove passwords from argocd-secret
	log.Info("Removing passwords from argocd-secret")
	argocdSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: userArgocdSecret, Namespace: userArgocdNS}, argocdSecret); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to get argocd-secret")
			return err
		}
		log.Info("argocd-secret not found, skipping password cleanup")
	} else {
		delete(argocdSecret.Data, "accounts."+teamName+"-admin-ci.password")
		delete(argocdSecret.Data, "accounts."+teamName+"-view-ci.password")
		if err := r.Update(ctx, argocdSecret); err != nil {
			log.Error(err, "Failed to update argocd-secret")
			return err
		}
		log.Info("Successfully removed passwords")
	}

	log.Info("Successfully cleaned up all ArgocdUser resources")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ArgocdUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&argocduserv1alpha1.ArgocdUser{}).
		Complete(r)
}
