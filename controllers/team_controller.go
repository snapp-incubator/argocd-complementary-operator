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
	"strings"

	userv1 "github.com/openshift/api/user/v1"
	teamv1alpha1 "github.com/snapp-incubator/team-operator/api/v1alpha1"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	userArgocdNS          = "user-argocd"
	userArgocRbacPolicyCM = "argocd-rbac-cm"
	userArgocStaticUserCM = "argocd-cm"
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

	r.createArgocdStaticUser(ctx, req, "admin")

	return ctrl.Result{}, nil
}

func (r *TeamReconciler) createArgocdStaticUser(ctx context.Context, req ctrl.Request, roleName string) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	team := &teamv1alpha1.Team{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, team)
	if err != nil {
		log.Error(err, "Failed to get team")
		return ctrl.Result{}, err
	}
	log.Info("team is found and teamName is : " + team.Name)

	// ciPass := team.Spec.Argo.Admin.CIPass
	argoUsers := team.Spec.Argo.Admin.Users
	if roleName == "view" {
		// ciPass = team.Spec.Argo.View.CIPass
		argoUsers = team.Spec.Argo.View.Users
	}

	configMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: userArgocStaticUserCM, Namespace: userArgocdNS}, configMap)
	if err != nil {
		log.Error(err, "Failed to get configMap")
		return ctrl.Result{}, err
	}
	patch := client.MergeFrom(configMap.DeepCopy())
	configMap.Data["accounts."+req.Name+"-"+roleName+"-CI"] = "apiKey,login"
	err = r.Patch(ctx, configMap, patch)
	if err != nil {
		log.Error(err, "Failed to patch cm")
		return ctrl.Result{}, err
	}

	/// do we need to hash the password?
	//set password to the user
	// hash, _ := HashPassword(ciPass) // ignore error for the sake of simplicity
	// encodedPass := b64.StdEncoding.EncodeToString([]byte(hash))

	secret := &corev1.Secret{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: "argocd-secret", Namespace: userArgocdNS}, secret)
	if err != nil {
		log.Error(err, "Failed to get secret")
		return ctrl.Result{}, err
	}
	patch = client.MergeFrom(secret.DeepCopy())
	secret.Data["accounts."+req.Name+"-"+roleName+"-CI.password"] = []byte(ciPass)
	err = r.Patch(ctx, secret, patch)
	if err != nil {
		log.Error(err, "Failed to patch secret")
		return ctrl.Result{}, err
	}

	group := &userv1.Group{}
	groupName := req.Name + "-" + roleName
	err = r.Client.Get(ctx, types.NamespacedName{Name: groupName}, group)
	if err != nil {
		log.Error(err, "Failed get group")
		// maybe we need to create group here ???
		return ctrl.Result{}, err
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
		return ctrl.Result{}, err
	}

	found := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: userArgocRbacPolicyCM, Namespace: userArgocdNS}, found)
	if err != nil {
		log.Error(err, "Failed to get cm")
		return ctrl.Result{}, err
	}

	//add argocd rbac policy
	newPolicy := "g, " + req.Name + "-" + roleName + "-CI, role:" + req.Name + "-" + roleName
	duplicatePolicy := false
	for _, line := range strings.Split(found.Data["policy.csv"], "\n") {
		if newPolicy == line {
			duplicatePolicy = true
			log.Info("duplicate policy")
		}
		log.Info(line)
	}
	if !duplicatePolicy {
		found.Data["policy.csv"] = found.Data["policy.csv"] + "\n" + newPolicy
		errRbac := r.Client.Update(ctx, found)
		if errRbac != nil {
			log.Error(err, "error in updating argocd-rbac-cm")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
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
