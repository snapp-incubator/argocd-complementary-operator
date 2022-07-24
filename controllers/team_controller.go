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
	b64 "encoding/base64"
	"strings"

	userv1 "github.com/openshift/api/user/v1"
	teamv1alpha1 "github.com/snapp-incubator/team-operator/api/v1alpha1"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	userArgocdNS           = "user-argocd"
	userArgocdRbacPolicyCM = "argocd-rbac-cm"
	userArgocdStaticUserCM = "argocd-cm"
	userArgocdSecret       = "argocd-secret"
)

// TeamReconciler reconciles a Team object
type TeamReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=team.snappcloud.io,resources=teams,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=team.snappcloud.io,resources=teams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=team.snappcloud.io,resources=teams/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=user.openshift.io,resources=*,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the clus k8s.io/api closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Team object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *TeamReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	team := &teamv1alpha1.Team{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, team)
	if err != nil {
		log.Error(err, "Failed to get team")
		return ctrl.Result{}, err
	}

	r.createArgocdStaticUser(ctx, req, team, "admin", team.Spec.Argo.Admin.CIPass, team.Spec.Argo.Admin.Users)
	r.createArgocdStaticUser(ctx, req, team, "view", team.Spec.Argo.View.CIPass, team.Spec.Argo.View.Users)
	r.AddArgocdRBACPolicy(ctx, team)

	return ctrl.Result{}, nil
}

func (r *TeamReconciler) createArgocdStaticUser(ctx context.Context, req ctrl.Request, team *teamv1alpha1.Team, roleName string, ciPass string, argoUsers []string) (ctrl.Result, error) {
	err := r.UpdateUserArgocdConfig(ctx, team, roleName, ciPass)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = r.AddArgoUsersToGroup(ctx, team, roleName, argoUsers)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TeamReconciler) UpdateUserArgocdConfig(ctx context.Context, team *teamv1alpha1.Team, roleName string, ciPass string) error {
	log := log.FromContext(ctx)
	configMap := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: userArgocdStaticUserCM, Namespace: userArgocdNS}, configMap)
	if err != nil {
		log.Error(err, "Failed to get configMap")
		return err
	}
	patch := client.MergeFrom(configMap.DeepCopy())
	configMap.Data["accounts."+team.Name+"-"+roleName+"-ci"] = "apiKey,login"
	err = r.Patch(ctx, configMap, patch)
	if err != nil {
		log.Error(err, "Failed to patch cm")
		return err
	}

	hash, _ := HashPassword(ciPass) // ignore error for the sake of simplicity
	encodedPass := b64.StdEncoding.EncodeToString([]byte(hash))

	secret := &corev1.Secret{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: userArgocdSecret, Namespace: userArgocdNS}, secret)
	if err != nil {
		log.Error(err, "Failed to get secret")
		return err
	}
	patch = client.MergeFrom(secret.DeepCopy())
	secret.Data["accounts."+team.Name+"-"+roleName+"-ci.password"] = []byte(encodedPass)
	err = r.Patch(ctx, secret, patch)
	if err != nil {
		log.Error(err, "Failed to patch secret")
		return err
	}
	return nil
}

func (r *TeamReconciler) AddArgoUsersToGroup(ctx context.Context, team *teamv1alpha1.Team, roleName string, argoUsers []string) error {
	log := log.FromContext(ctx)
	group := &userv1.Group{}
	groupName := team.Name + "-" + roleName
	err := r.Client.Get(ctx, types.NamespacedName{Name: groupName}, group)
	if err != nil {
		log.Error(err, "Failed get group")
		// create group
		group = &userv1.Group{
			ObjectMeta: metav1.ObjectMeta{
				Name: groupName,
			},
		}
		err = r.Client.Create(ctx, group)
		if err != nil {
			log.Error(err, "Failed to create group")
			return err
		}
		log.Info("group is created")
	}
	//check user exist to add it to group
	argoUser := &userv1.User{}
	for _, user := range argoUsers {
		duplicateUser := false
		errUser := r.Client.Get(ctx, types.NamespacedName{Name: user}, argoUser)
		for _, groupuser := range group.Users {
			if argoUser.Name == groupuser {
				duplicateUser = true
			}
		}
		if !duplicateUser && errUser == nil {
			group.Users = append(group.Users, user)
		}
	}
	err = r.Client.Update(ctx, group)
	if err != nil {
		log.Error(err, "Failed to update group")
		return err
	}
	return nil
}

func (r *TeamReconciler) AddArgocdRBACPolicy(ctx context.Context, team *teamv1alpha1.Team) error {
	log := log.FromContext(ctx)
	found := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: userArgocdRbacPolicyCM, Namespace: userArgocdNS}, found)
	if err != nil {
		log.Error(err, "Failed to get cm")
		return err
	}

	policies := []string{
		"g, " + team.Name + "-admin-ci, role:" + team.Name + "-admin",
		"g, " + team.Name + "-view-ci, role:" + team.Name + "-view",
		"p, role:" + team.Name + "-admin, repositories, create, " + team.Name + "/*, allow",
		"p, role:" + team.Name + "-admin, repositories, delete, " + team.Name + "/*, allow",
		"p, role:" + team.Name + "-admin, repositories, update, " + team.Name + "/*, allow",
		"p, role:" + team.Name + "-view, repositories, get, " + team.Name + "/*, allow",
		"p, role:" + team.Name + "-admin, exec, create, *, allow",
		"g, " + team.Name + "-admin, role:" + team.Name + "-admin",
		"g, " + team.Name + "-admin, role:" + team.Name + "-view",
		"g, " + team.Name + "-admin, role:common",
		"g, " + team.Name + "-view, role:common",
		"g, " + team.Name + "-view, role:" + team.Name + "-view",
		"g, " + team.Name + "-view, role:" + team.Name + "-admin",
	}
	log.Info("policies are : " + strings.Join(policies, ","))

	//add argocd rbac policy
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
		errRbac := r.Client.Update(ctx, found)
		if errRbac != nil {
			log.Error(err, "error in updating argocd-rbac-cm")
			return err
		}
	}
	return nil
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

// SetupWithManager sets up the controller with the Manager.
func (r *TeamReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&teamv1alpha1.Team{}).
		Complete(r)
}
