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
	"golang.org/x/crypto/bcrypt"

	userv1 "github.com/openshift/api/user/v1"
	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var gvk = userv1.GroupVersion.WithKind("Group")

// ArgocdUserReconciler reconciles a ArgocdUser object
type ArgocdUserReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	mu                sync.Mutex
	GroupCRDInstalled bool
	// GroupMembershipAuthoritative is the cluster-wide default for whether the
	// ArgocdUser spec is the sole source of truth for the OpenShift Groups this
	// operator manages. Resolved from the GROUP_MEMBERSHIP_AUTHORITATIVE env var
	// in SetupWithManager, and overridable per ArgocdUser through the
	// argocd.snappcloud.io/authoritative-groups annotation. See
	// groupMembershipAuthoritative for what it changes.
	GroupMembershipAuthoritative bool
}

// eventf records an Event on the ArgocdUser, tolerating a nil recorder so the
// reconciler stays usable when constructed directly (as the tests do).
func (r *ArgocdUserReconciler) eventf(argocduser *argocduserv1alpha1.ArgocdUser, eventType, reason, messageFmt string, args ...interface{}) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(argocduser, eventType, reason, messageFmt, args...)
}

//+kubebuilder:rbac:groups=argocd.snappcloud.io,resources=argocdusers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=argocd.snappcloud.io,resources=argocdusers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=argocd.snappcloud.io,resources=argocdusers/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// ArgocdUser is cluster-scoped, so its Events are recorded in the "default"
// namespace — hence the cluster-wide grant rather than one scoped to the
// operator's own namespace.
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=user.openshift.io,resources=groups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
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
	logger := log.FromContext(ctx)
	logger.Info("Reconciling ArgocdUser", "request", req.NamespacedName)

	argocduserName := req.Name
	argocduser := &argocduserv1alpha1.ArgocdUser{}
	if err := r.Get(ctx, req.NamespacedName, argocduser); err != nil {
		if errors.IsNotFound(err) {
			logger.Error(err, "ArgocdUser not found", "request", req.NamespacedName)
			return ctrl.Result{}, nil
		} else {
			logger.Error(err, "Failed to get ArgocdUser")
			return ctrl.Result{}, err
		}
	}

	// Check if the ArgocdUser is being deleted
	if !argocduser.DeletionTimestamp.IsZero() {
		// ArgocdUser is being deleted
		if controllerutil.ContainsFinalizer(argocduser, argocdUserFinalizer) {
			// Run cleanup logic
			if err := r.cleanupResources(ctx, argocduser); err != nil {
				logger.Error(err, "Failed to cleanup resources")
				return ctrl.Result{}, err
			}

			// Remove the finalizer
			controllerutil.RemoveFinalizer(argocduser, argocdUserFinalizer)
			if err := r.Update(ctx, argocduser); err != nil {
				logger.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present (for new resources)
	if !controllerutil.ContainsFinalizer(argocduser, argocdUserFinalizer) {
		controllerutil.AddFinalizer(argocduser, argocdUserFinalizer)
		if err := r.Update(ctx, argocduser); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileClusterRole(ctx, argocduser); err != nil {
		logger.Error(err, "Failed to reconcile ClusterRole", "ArgocdUser", argocduser)
		return ctrl.Result{}, err
	}

	if err := r.reconcileClusterRoleBinding(ctx, argocduser); err != nil {
		logger.Error(err, "Failed to reconcile ClusterRoleBinding", "ArgocdUser", argocduser)
		return ctrl.Result{}, err
	}

	if err := r.reconcileAppProject(ctx, argocduser); err != nil {
		logger.Error(err, "Failed to reconcile AppProject", "Argocduser/AppProject", argocduserName)
		return ctrl.Result{}, err
	}

	if err := r.reconcileArgocdStaticUser(ctx, argocduser); err != nil {
		logger.Error(err, "Failed create argocd static user admin")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ArgocdUserReconciler) reconcileClusterRole(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser) error {
	logger := log.FromContext(ctx)
	clusterRoleName := argocduser.Name + "-argocduser-clusterrole"

	// TODO: Update to use specific label with the corresponding `Argocduser` name, for watching and tracking
	// Define the desired ClusterRole
	desiredClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"argocd.snappcloud.io"},
				Resources:     []string{"argocdusers"},
				ResourceNames: []string{argocduser.Name},
				Verbs:         []string{"get", "list", "watch", "patch", "update"},
			},
		},
	}
	// Set ArgocdUser as the owner of ClusterRole
	if err := controllerutil.SetControllerReference(argocduser, desiredClusterRole, r.Scheme); err != nil {
		logger.Error(err, "Failed to set Argocduser as owner reference on ClusterRole ", "ArgocdUser", argocduser.Name, "ClusterRole", clusterRoleName)
		return err
	}
	// Try to get existing ClusterRole
	existingClusterRole := &rbacv1.ClusterRole{}
	err := r.Get(ctx, types.NamespacedName{Name: clusterRoleName}, existingClusterRole)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ClusterRole
			logger.Info("Creating ClusterRole", "ClusterRole", clusterRoleName)
			if err := r.Create(ctx, desiredClusterRole); err != nil {
				logger.Error(err, "Failed to create ClusterRole")
				return err
			}
			logger.Info("Successfully created ClusterRole", "ClusterRole", clusterRoleName)
			return nil
		} else {
			logger.Error(err, "Failed to get ClusterRole", "ClusterRole", clusterRoleName)
			return err
		}
	}

	needsUpdate := false
	// Update existing ClusterRole if OwnerReferences differs
	if !reflect.DeepEqual(existingClusterRole.OwnerReferences, desiredClusterRole.OwnerReferences) {
		logger.Info("Updating OwnerReferences of ClusterRole", "ClusterRole", clusterRoleName)
		existingClusterRole.OwnerReferences = desiredClusterRole.OwnerReferences
		needsUpdate = true
	}
	// Update existing ClusterRole if rules differ
	if !reflect.DeepEqual(existingClusterRole.Rules, desiredClusterRole.Rules) {
		logger.Info("Updating Rules of ClusterRole", "ClusterRole", clusterRoleName)
		existingClusterRole.Rules = desiredClusterRole.Rules
		needsUpdate = true
	}
	// Only update if something changed
	if needsUpdate {
		logger.Info("Updating ClusterRole", "ClusterRole", clusterRoleName)
		if err := r.Update(ctx, existingClusterRole); err != nil {
			logger.Error(err, "Failed to update ClusterRole", "ClusterRole", clusterRoleName)
			return err
		}
		logger.Info("Successfully updated ClusterRole", "ClusterRole", clusterRoleName)
	}
	return nil
}

func (r *ArgocdUserReconciler) reconcileClusterRoleBinding(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser) error {
	logger := log.FromContext(ctx)
	clusterRoleBindingName := argocduser.Name + "-argocduser-clusterrolebinding"
	clusterRoleName := argocduser.Name + "-argocduser-clusterrole"

	// TODO: Update to use specific label with the corresponding `Argocduser` name, for watching and tracking
	// Build subjects from admin users
	subjects := make([]rbacv1.Subject, 0, len(argocduser.Spec.Admin.Users))
	for _, user := range argocduser.Spec.Admin.Users {
		subjects = append(subjects, rbacv1.Subject{
			Kind:     "User",
			APIGroup: "rbac.authorization.k8s.io",
			Name:     user,
		})
	}

	// Define the desired ClusterRoleBinding
	desiredClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleBindingName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
		Subjects: subjects,
	}

	// Set ArgocdUser as the owner of ClusterRoleBinding
	if err := controllerutil.SetControllerReference(argocduser, desiredClusterRoleBinding, r.Scheme); err != nil {
		logger.Error(err, "Failed to set Argocduser as owner reference on ClusterRoleBinding ", "ArgocdUser", argocduser.Name, "ClusterRoleBinding", clusterRoleBindingName)
		return err
	}

	// Try to get existing ClusterRoleBinding
	existingClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: clusterRoleBindingName}, existingClusterRoleBinding)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ClusterRoleBinding
			logger.Info("Creating ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
			if err := r.Create(ctx, desiredClusterRoleBinding); err != nil {
				logger.Error(err, "Failed to create ClusterRoleBinding")
				return err
			}
			logger.Info("Successfully created ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
			return nil
		}
		logger.Error(err, "Failed to get ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
		return err
	}

	needsUpdate := false
	needsRecreate := false
	// Update existing ClusterRoleBinding if OwnerReferences differ
	if !reflect.DeepEqual(existingClusterRoleBinding.OwnerReferences, desiredClusterRoleBinding.OwnerReferences) {
		logger.Info("Updating OwnerReferences of ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
		existingClusterRoleBinding.OwnerReferences = desiredClusterRoleBinding.OwnerReferences
		needsUpdate = true
	}
	// Update existing ClusterRoleBinding if subjects differ
	if !reflect.DeepEqual(existingClusterRoleBinding.Subjects, desiredClusterRoleBinding.Subjects) {
		logger.Info("Updating ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
		existingClusterRoleBinding.Subjects = desiredClusterRoleBinding.Subjects
		needsUpdate = true
	}
	// Update existing ClusterRoleBinding if RoleRef differ
	// Kubernetes does not allow updating RoleRef on ClusterRoleBindings. It should be recreated.
	if !reflect.DeepEqual(existingClusterRoleBinding.RoleRef, desiredClusterRoleBinding.RoleRef) {
		logger.Info("RoleRef changed, ClusterRoleBinding should be recreated", "ClusterRoleBinding", clusterRoleBindingName)
		needsRecreate = true
	}

	if needsRecreate {
		logger.Info("Recreating ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
		if err := r.Delete(ctx, existingClusterRoleBinding); err != nil {
			logger.Error(err, "Failed to delete ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
			return err
		}
		if err := r.Create(ctx, desiredClusterRoleBinding); err != nil {
			logger.Error(err, "Failed to recreate ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
			return err
		}
		logger.Info("Successfully recreated ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
	} else if needsUpdate {
		logger.Info("Updating ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
		if err := r.Update(ctx, existingClusterRoleBinding); err != nil {
			logger.Error(err, "Failed to update ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
			return err
		}
		logger.Info("Successfully updated ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)
	}
	return nil
}

// reconcileAppProject create an argocd project and change the current argocd project to be compatible with it.
// it is called everytime a label changed, so when you remove a policy or etc it will not be called.
func (r *ArgocdUserReconciler) reconcileAppProject(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser) error {
	logger := log.FromContext(ctx)
	appProjectName := argocduser.Name

	desiredAppProject := createAppProj(appProjectName)

	// Check if AppProject does not exist and create a new one
	found := &argov1alpha1.AppProject{}
	err := r.Get(ctx, types.NamespacedName{Name: appProjectName, Namespace: UserArgocdNS}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating the AppProject", "AppProject", appProjectName)
			if err = r.Create(ctx, desiredAppProject); err != nil {
				logger.Error(err, "Failed to create AppProject", "AppProject", appProjectName)
				return err
			}
			logger.Info("Successfully created AppProject", "AppProject", appProjectName)
			return nil
		} else {
			logger.Error(err, "Failed to get AppProject", "AppProject", appProjectName)
			return err
		}
	}

	desiredAppProject.Spec.SourceRepos = mergeStringSlices(desiredAppProject.Spec.SourceRepos, found.Spec.SourceRepos)

	needsUpdate := false
	var bothEmpty bool
	// 1. Fix Destinations (source of truth: NamespaceCache)
	// First, we check length to prevent considering nil and [] as not equal in reflect.DeepEqual
	bothEmpty = (len(found.Spec.Destinations) == 0 && len(desiredAppProject.Spec.Destinations) == 0)
	// Then check with reflect.DeepEqual
	if !bothEmpty && !reflect.DeepEqual(found.Spec.Destinations, desiredAppProject.Spec.Destinations) {
		logger.Info("Updating AppProject Destinations from Namespaces", "AppProject", appProjectName,
			"existingCount", len(found.Spec.Destinations), "desiredCount", len(desiredAppProject.Spec.Destinations))
		found.Spec.Destinations = desiredAppProject.Spec.Destinations
		needsUpdate = true
	}
	// 2. Fix SourceNamespaces (source of truth: NamespaceCache)
	// First, we check length to prevent considering nil and [] as not equal in reflect.DeepEqual
	bothEmpty = (len(found.Spec.SourceNamespaces) == 0 && len(desiredAppProject.Spec.SourceNamespaces) == 0)
	// Then check with reflect.DeepEqual
	if !bothEmpty && !reflect.DeepEqual(found.Spec.SourceNamespaces, desiredAppProject.Spec.SourceNamespaces) {
		logger.Info("Updating AppProject SourceNamespaces from NamespaceCache", "AppProject", appProjectName,
			"existingCount", len(found.Spec.SourceNamespaces), "desiredCount", len(desiredAppProject.Spec.SourceNamespaces))
		found.Spec.SourceNamespaces = desiredAppProject.Spec.SourceNamespaces
		needsUpdate = true
	}
	// 3. Fix Roles (managed by ArgocdUser)
	// First, we check length to prevent considering nil and [] as not equal in reflect.DeepEqual
	bothEmpty = (len(found.Spec.Roles) == 0 && len(desiredAppProject.Spec.Roles) == 0)
	// Then check with reflect.DeepEqual
	if !bothEmpty && !reflect.DeepEqual(found.Spec.Roles, desiredAppProject.Spec.Roles) {
		logger.Info("Updating AppProject Roles", "AppProject", desiredAppProject)
		found.Spec.Roles = desiredAppProject.Spec.Roles
		needsUpdate = true
	}
	// 4. Merge SourceRepos (additive - preserve manually added repos)
	// First, we check length to prevent considering nil and [] as not equal in reflect.DeepEqual
	bothEmpty = (len(found.Spec.SourceRepos) == 0 && len(desiredAppProject.Spec.SourceRepos) == 0)
	// Then check with reflect.DeepEqual
	if !bothEmpty && !reflect.DeepEqual(found.Spec.SourceRepos, desiredAppProject.Spec.SourceRepos) {
		logger.Info("Updating AppProject SourceRepos", "AppProject", appProjectName,
			"existingCount", len(desiredAppProject.Spec.SourceRepos), "newCount", len(desiredAppProject.Spec.SourceRepos))
		found.Spec.SourceRepos = desiredAppProject.Spec.SourceRepos
		needsUpdate = true
	}

	// Only update if something changed
	if needsUpdate {
		logger.Info("Updating AppProject", "AppProject", appProjectName)
		if err := r.Update(ctx, found); err != nil {
			logger.Error(err, "Failed to update AppProject", "AppProject", appProjectName)
			return err
		}
	} else {
		logger.Info("AppProject is up to date, no changes needed", "AppProject", appProjectName)
	}

	return nil
}

func (r *ArgocdUserReconciler) reconcileArgocdStaticUser(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser) error {
	logger := log.FromContext(ctx)
	// For admin
	if err := r.UpdateUserArgocdConfig(ctx, argocduser, "admin", argocduser.Spec.Admin.CIPass); err != nil {
		logger.Error(err, "Failed to create argocd static user configs", "ArgocdUser", argocduser.Name, "role", "admin")
		return err
	}
	if err := r.ReconcileArgoUsersGroup(ctx, argocduser, "admin", argocduser.Spec.Admin.Users); err != nil {
		logger.Error(err, "Failed to update OpenShift groups for Argocduser", "ArgocdUser", argocduser.Name, "role", "admin")
		return err
	}

	// For view role
	if err := r.UpdateUserArgocdConfig(ctx, argocduser, "view", argocduser.Spec.View.CIPass); err != nil {
		logger.Error(err, "Failed to create argocd static user configs", "ArgocdUser", argocduser.Name, "role", "view")
		return err
	}
	if err := r.ReconcileArgoUsersGroup(ctx, argocduser, "view", argocduser.Spec.View.Users); err != nil {
		logger.Error(err, "Failed to update OpenShift groups for Argocduser", "ArgocdUser", argocduser.Name, "role", "view")
		return err
	}

	// For sync role (view + applications,sync)
	if err := r.UpdateUserArgocdConfig(ctx, argocduser, "sync", argocduser.Spec.Sync.CIPass); err != nil {
		logger.Error(err, "Failed to create argocd static user configs", "ArgocdUser", argocduser.Name, "role", "sync")
		return err
	}
	if err := r.ReconcileArgoUsersGroup(ctx, argocduser, "sync", argocduser.Spec.Sync.Users); err != nil {
		logger.Error(err, "Failed to update OpenShift groups for Argocduser", "ArgocdUser", argocduser.Name, "role", "sync")
		return err
	}

	return nil
}

func (r *ArgocdUserReconciler) UpdateUserArgocdConfig(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser, roleName string, ciPass string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	logger := log.FromContext(ctx)

	// Reconcile argocd-cm
	accountKey := "accounts." + argocduser.Name + "-" + roleName + "-ci"
	expectedValue := "apiKey,login"
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: userArgocdStaticUserCM, Namespace: UserArgocdNS}, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Error(err, "Argocd ConfigMap not found", "ConfigMap", userArgocdStaticUserCM)
			return err
		} else {
			logger.Error(err, "Failed to get ConfigMap")
			return err
		}
	}
	// Only patch if value differs
	if configMap.Data[accountKey] != expectedValue {
		patch := client.MergeFrom(configMap.DeepCopy())
		configMap.Data[accountKey] = expectedValue
		if err := r.Patch(ctx, configMap, patch); err != nil {
			logger.Error(err, "Failed to patch ConfigMap")
			return err
		}
		logger.Info("Updated ConfigMap account", "key", accountKey)
	}

	// Reconcile argocd-secret
	passwordKey := "accounts." + argocduser.Name + "-" + roleName + "-ci.password"

	// Helper function for updting password secret
	patchWithHashFunc := func(secret *corev1.Secret) error {
		// Generate and set password only when key doesn't exist
		hash, err := hashPassword(ciPass)
		if err != nil {
			logger.Error(err, "Failed to hash ciPassword", "Argocduser-role", argocduser.Name+roleName)
			return fmt.Errorf("password hashing failed: %w", err)
		}
		patch := client.MergeFrom(secret.DeepCopy())
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		secret.Data[passwordKey] = []byte(hash)
		if err := r.Patch(ctx, secret, patch); err != nil {
			logger.Error(err, "Failed to patch Secret", "Argocduser-role", argocduser.Name+"-"+roleName)
			return err
		}
		logger.Info("Updated Secret password", "key", passwordKey)
		return nil
	}

	secret := &corev1.Secret{}
	if err = r.Get(ctx, types.NamespacedName{Name: userArgocdSecret, Namespace: UserArgocdNS}, secret); err != nil {
		if errors.IsNotFound(err) {
			logger.Error(err, "Argocd Secret not found", "Secret", userArgocdSecret)
			return err
		} else {
			logger.Error(err, "Failed to get Secret")
			return err
		}
	}
	// Only patch if value differs or doesn't exists
	if value, exists := secret.Data[passwordKey]; !exists {
		return patchWithHashFunc(secret)
	} else if err = bcrypt.CompareHashAndPassword(value, []byte(ciPass)); exists && err != nil {
		// If password exists but is not equal to the desired one
		logger.Info("Going to update password. Password hash mismatch for user", "Argocduser-role", argocduser.Name+"-"+roleName)
		return patchWithHashFunc(secret)
	}
	return nil
}

// ReconcileArgoUsersGroup brings the OpenShift Group backing one ArgocdUser role
// in line with the users declared on the spec. Undeclared members are always
// reported; whether they are removed depends on GroupMembershipAuthoritative.
func (r *ArgocdUserReconciler) ReconcileArgoUsersGroup(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser, roleName string, argoUsers []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	logger := log.FromContext(ctx)

	// Check if Group CRD is registered in the scheme, to ignore this in integration tests
	if !r.GroupCRDInstalled {
		logger.Info("Group CRD not registered in scheme, skipping group management")
		return nil
	}

	groupName := argocduser.Name + "-" + roleName
	group := &userv1.Group{}
	err := r.Get(ctx, types.NamespacedName{Name: groupName}, group)
	if err != nil {
		if errors.IsNotFound(err) {
			desiredGroup := &userv1.Group{
				ObjectMeta: metav1.ObjectMeta{Name: groupName},
				Users:      argoUsers,
			}
			logger.Info("Creating the group", "Group", groupName)
			if err = r.Create(ctx, desiredGroup); err != nil {
				logger.Error(err, "Failed to create group", "Group", groupName)
				return err
			}
			undeclaredGroupMembers.WithLabelValues(argocduser.Name, roleName, groupName).Set(0)
			logger.Info("Successfully created Group", "Group", groupName)
			return nil
		} else {
			logger.Error(err, "Failed to get group", "Group", groupName)
			return err
		}
	}

	authoritative := groupMembershipAuthoritativeFor(argocduser.Annotations, r.GroupMembershipAuthoritative)
	desiredUsers, undeclaredUsers := desiredGroupUsers(group.Users, argoUsers, authoritative)
	undeclaredGroupMembers.WithLabelValues(argocduser.Name, roleName, groupName).Set(float64(len(undeclaredUsers)))

	if len(undeclaredUsers) > 0 {
		if authoritative {
			logger.Info("Removing members that the ArgocdUser does not declare",
				"Group", groupName, "prunedCount", len(undeclaredUsers), "pruned", undeclaredUsers)
			r.eventf(argocduser, corev1.EventTypeNormal, "GroupMembersPruned",
				"Removed %d undeclared member(s) from Group %s: %s",
				len(undeclaredUsers), groupName, summarizeUsers(undeclaredUsers, eventUserSampleSize))
		} else {
			logger.Info("Group holds members the ArgocdUser does not declare; keeping them because group membership is not authoritative here",
				"Group", groupName, "undeclaredCount", len(undeclaredUsers), "undeclared", undeclaredUsers)
			r.eventf(argocduser, corev1.EventTypeWarning, "UndeclaredGroupMembers",
				"Group %s holds %d member(s) this ArgocdUser does not declare: %s. They keep the %s role until %s=true or the %s annotation is set.",
				groupName, len(undeclaredUsers), summarizeUsers(undeclaredUsers, eventUserSampleSize),
				roleName, groupMembershipAuthoritativeEnv, GroupMembershipAuthoritativeAnnotation)
		}
	}

	// Only update if the users changed
	bothEmpty := (len(group.Users) == 0 && len(desiredUsers) == 0)
	if !bothEmpty && !reflect.DeepEqual(group.Users, desiredUsers) {
		logger.Info("Updating group users", "Group", groupName, "existingCount", len(group.Users), "newCount", len(desiredUsers))
		logger.Info("Users differ", "existing", group.Users, "desired", desiredUsers)
		group.Users = desiredUsers
		err = r.Update(ctx, group)
		if err != nil {
			logger.Error(err, "Failed to update group", "Group", groupName)
			return err
		}
		logger.Info("Successfully updated group users", "Group", groupName)
	} else {
		logger.Info("Group users already up to date, skipping update", "Group", groupName)
	}
	return nil
}

func (r *ArgocdUserReconciler) cleanupResources(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser) error {
	logger := log.FromContext(ctx)
	name := argocduser.Name

	// 1. Delete AppProject
	if err := r.deleteAppProject(ctx, name); err != nil {
		return err
	}

	// 2. Remove ConfigMap entries
	if err := r.removeConfigMapEntries(ctx, name); err != nil {
		return err
	}

	// 3. Remove Secret entries
	if err := r.removeSecretEntries(ctx, name); err != nil {
		return err
	}

	// 4. Delete Groups (only when this operator exclusively owns them)
	if err := r.deleteGroups(ctx, argocduser); err != nil {
		return err
	}

	logger.Info("Successfully cleaned up all resources", "ArgocdUser", name)
	return nil
}

// deleteGroups removes the OpenShift Groups backing a deleted ArgocdUser.
//
// Only safe when the operator owns Group membership outright: with merged
// membership a Group can hold people this ArgocdUser never declared, and
// deleting it would revoke access the operator never granted. So this is a
// no-op unless membership is authoritative for this ArgocdUser, which is also
// why the Groups have historically been left behind.
func (r *ArgocdUserReconciler) deleteGroups(ctx context.Context, argocduser *argocduserv1alpha1.ArgocdUser) error {
	logger := log.FromContext(ctx)
	name := argocduser.Name

	if !r.GroupCRDInstalled {
		logger.Info("Group CRD not registered in scheme, skipping group cleanup")
		return nil
	}
	if !groupMembershipAuthoritativeFor(argocduser.Annotations, r.GroupMembershipAuthoritative) {
		logger.Info("Leaving Groups in place because group membership is not authoritative here", "ArgocdUser", name)
		return nil
	}

	for _, roleName := range groupRoles {
		groupName := name + "-" + roleName
		group := &userv1.Group{ObjectMeta: metav1.ObjectMeta{Name: groupName}}
		if err := r.Delete(ctx, group); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			logger.Error(err, "Failed to delete Group", "Group", groupName)
			return err
		}
		undeclaredGroupMembers.DeleteLabelValues(name, roleName, groupName)
		logger.Info("Removed Group", "Group", groupName)
	}
	return nil
}

func (r *ArgocdUserReconciler) deleteAppProject(ctx context.Context, name string) error {
	logger := log.FromContext(ctx)
	appProj := &argov1alpha1.AppProject{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: UserArgocdNS}, appProj)
	if err != nil {
		if errors.IsNotFound(err) {
			// AppProj doesn't exist, nothing to clean up
			return nil
		}
		return err
	}
	// Already being deleted, nothing to do
	if !appProj.DeletionTimestamp.IsZero() {
		logger.Info("AppProject already being deleted", "AppProject", name)
		return nil
	}

	if err := r.Delete(ctx, appProj); err != nil {
		if errors.IsNotFound(err) {
			// Deleted between Get and Delete - that's fine
			return nil
		}
		logger.Error(err, "Failed to remove AppProject", "AppProject", name)
		return err
	}
	logger.Info("Removed AppProject", "AppProject", name)
	return nil
}

func (r *ArgocdUserReconciler) removeConfigMapEntries(ctx context.Context, name string) error {
	logger := log.FromContext(ctx)
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: userArgocdStaticUserCM, Namespace: UserArgocdNS}, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap doesn't exist, nothing to clean up
			return nil
		}
		return err
	}

	patch := client.MergeFrom(configMap.DeepCopy())
	// Delete admin, view and sync accounts
	delete(configMap.Data, "accounts."+name+"-admin-ci")
	delete(configMap.Data, "accounts."+name+"-view-ci")
	delete(configMap.Data, "accounts."+name+"-sync-ci")

	if err := r.Patch(ctx, configMap, patch); err != nil {
		logger.Error(err, "Failed to remove accounts from ConfigMap", "ArgocdUser", name)
		return err
	}
	logger.Info("Removed accounts from ConfigMap", "ArgocdUser", name)
	return nil
}

func (r *ArgocdUserReconciler) removeSecretEntries(ctx context.Context, name string) error {
	logger := log.FromContext(ctx)
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: userArgocdSecret, Namespace: UserArgocdNS}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	secretPatch := client.MergeFrom(secret.DeepCopy())
	delete(secret.Data, "accounts."+name+"-admin-ci.password")
	delete(secret.Data, "accounts."+name+"-view-ci.password")
	delete(secret.Data, "accounts."+name+"-sync-ci.password")

	if err := r.Patch(ctx, secret, secretPatch); err != nil {
		logger.Error(err, "Failed to remove passwords from Secret", "ArgocdUser", name)
		return err
	}
	logger.Info("Removed passwords from Secret", "ArgocdUser", name)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ArgocdUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.GroupCRDInstalled = isCRDInstalled(mgr.GetConfig(), gvk)
	r.GroupMembershipAuthoritative = groupMembershipAuthoritative()
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("argocduser-controller")
	}
	mgr.GetLogger().Info("Group membership mode resolved",
		"authoritative", r.GroupMembershipAuthoritative, "env", groupMembershipAuthoritativeEnv)

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&argocduserv1alpha1.ArgocdUser{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				if obj.GetNamespace() != UserArgocdNS || obj.GetName() != userArgocdStaticUserCM {
					return nil
				}
				argocdUserList := &argocduserv1alpha1.ArgocdUserList{}
				if err := r.List(ctx, argocdUserList); err != nil {
					return nil
				}
				requests := make([]reconcile.Request, len(argocdUserList.Items))
				for i, item := range argocdUserList.Items {
					requests[i] = reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name: item.Name,
						},
					}
				}
				return requests
			}),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				if obj.GetNamespace() != UserArgocdNS || obj.GetName() != userArgocdSecret {
					return nil
				}
				argocdUserList := &argocduserv1alpha1.ArgocdUserList{}
				if err := r.List(ctx, argocdUserList); err != nil {
					return nil
				}
				requests := make([]reconcile.Request, len(argocdUserList.Items))
				for i, item := range argocdUserList.Items {
					requests[i] = reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name: item.Name,
						},
					}
				}
				return requests
			}),
		).
		Watches(
			&argov1alpha1.AppProject{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// AppProject changed - reconcile the ArgocdUser with the same name
				appProjName := obj.GetName()

				// Only watch AppProjects in the user-argocd namespace
				if obj.GetNamespace() != UserArgocdNS {
					return nil
				}

				// TODO: Update to use labels instead of extracting name
				// Check if an ArgocdUser with this name exists
				argocdUser := &argocduserv1alpha1.ArgocdUser{}
				if err := r.Get(ctx, types.NamespacedName{Name: appProjName}, argocdUser); err != nil {
					// ArgocdUser doesn't exist - this AppProject is not managed by an ArgocdUser
					return nil
				}

				// Reconcile the corresponding ArgocdUser
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{Name: appProjName},
				}}
			}),
		)

	// Conditionally add Group watch only if the CRD is registered
	if r.GroupCRDInstalled {
		builder = builder.Watches(
			&userv1.Group{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// TODO: Update to use labels instead of extracting name with suffix

				// Check if this Group is managed by an ArgocdUser
				// Group naming: <argocduser-name>-admin, <argocduser-name>-view or <argocduser-name>-sync
				groupName := obj.GetName()

				// Extract ArgocdUser name from Group name
				var argocduserName string
				if strings.HasSuffix(groupName, "-admin") {
					argocduserName = strings.TrimSuffix(groupName, "-admin")
				} else if strings.HasSuffix(groupName, "-view") {
					argocduserName = strings.TrimSuffix(groupName, "-view")
				} else if strings.HasSuffix(groupName, "-sync") {
					argocduserName = strings.TrimSuffix(groupName, "-sync")
				} else {
					// Not a Group we manage
					return nil
				}

				// Reconcile the corresponding ArgocdUser
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Name: argocduserName,
					},
				}}
			}),
		)
	}

	return builder.Complete(r)
}
