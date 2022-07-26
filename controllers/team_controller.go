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
	"encoding/json"
	"fmt"
	"strings"

	userv1 "github.com/openshift/api/user/v1"
	teamv1alpha1 "github.com/snapp-incubator/team-operator/api/v1alpha1"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	yaml "sigs.k8s.io/yaml"
)

const (
	userArgocdNS          = "user-argocd"
	userArgocRbacPolicyCM = "argocd-rbac-cm"
	userArgocStaticUserCM = "argocd-cm"
)

//var logf = log.Log.WithName("controller_team")

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
	reqLogger := log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling team")
	team := &teamv1alpha1.Team{}

	err := r.Client.Get(context.TODO(), req.NamespacedName, team)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	} else {
		log.Info("team is found and teamName is : " + team.Name)

	}
	r.createArgocdStaticAdminUser(ctx, req)
	r.createArgocdStaticViewUser(ctx, req)

	return ctrl.Result{}, nil
}
func (r *TeamReconciler) createArgocdStaticAdminUser(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	reqLogger := log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling team")
	team := &teamv1alpha1.Team{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, team)
	if err != nil {
		log.Error(err, "Failed to get team")
		return ctrl.Result{}, err
	}

	log.Info("team is found and teamName is : " + team.Name)
	//create static admin CI user
	staticAdminUser := map[string]map[string]string{
		"data": {
			"accounts." + req.Name + "-Admin-CI": "apiKey,login",
		},
	}
	staticAdminUserByte, _ := json.Marshal(staticAdminUser)
	err = r.Client.Patch(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: userArgocdNS,
			Name:      userArgocStaticUserCM,
		},
	}, client.RawPatch(types.StrategicMergePatchType, staticAdminUserByte))
	if err != nil {
		log.Error(err, "Failed to patch cm")
		return ctrl.Result{}, err
	}
	//set password to the user
	hash, _ := HashPassword(team.Spec.Argo.Admin.CIPass) // ignore error for the sake of simplicity

	encodedPass := b64.StdEncoding.EncodeToString([]byte(hash))
	staticPassword := map[string]map[string]string{
		"data": {
			"accounts." + req.Name + "-Admin-CI.password": encodedPass,
		},
	}
	staticPassByte, _ := json.Marshal(staticPassword)

	err = r.Client.Patch(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: userArgocdNS,
			Name:      "argocd-secret",
		},
	}, client.RawPatch(types.StrategicMergePatchType, staticPassByte))
	if err != nil {
		log.Error(err, "Failed to patch secret")
		return ctrl.Result{}, err
	}
	r.setRBACArgoCDAdminUser(ctx, req)

	return ctrl.Result{}, nil
}

func (r *TeamReconciler) createArgocdStaticViewUser(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	reqLogger := log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling team")
	team := &teamv1alpha1.Team{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, team)
	if err != nil {
		log.Error(err, "Failed to get team")
		return ctrl.Result{}, err
	}

	log.Info("team is found and teamName is : " + team.Name)
	staticViewUser := map[string]map[string]string{
		"data": {
			"accounts." + req.Name + "-View-CI": "apiKey,login",
		},
	}
	staticViewUserByte, _ := json.Marshal(staticViewUser)
	err = r.Client.Patch(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: userArgocdNS,
			Name:      userArgocStaticUserCM,
		},
	}, client.RawPatch(types.StrategicMergePatchType, staticViewUserByte))
	if err != nil {
		log.Error(err, "Failed to patch cm")
		return ctrl.Result{}, err
	}
	//set password to the user
	hash, _ := HashPassword(team.Spec.Argo.View.CIPass) // ignore error for the sake of simplicity

	encodedPass := b64.StdEncoding.EncodeToString([]byte(hash))

	staticPassword := map[string]map[string]string{
		"data": {
			"accounts." + req.Name + "-View-CI" + ".password": encodedPass,
		},
	}
	staticPassByte, _ := json.Marshal(staticPassword)

	err = r.Client.Patch(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: userArgocdNS,
			Name:      "argocd-secret",
		},
	}, client.RawPatch(types.StrategicMergePatchType, staticPassByte))
	if err != nil {
		log.Error(err, "Failed to patch secret")
		return ctrl.Result{}, err
	}
	r.setRBACArgoCDViewUser(ctx, req)

	return ctrl.Result{}, nil
}
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func (r *TeamReconciler) setRBACArgoCDAdminUser(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	reqLogger := log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling team")
	team := &teamv1alpha1.Team{}
	found := &corev1.ConfigMap{}
	err1 := r.Client.Get(context.TODO(), req.NamespacedName, team)
	if err1 != nil {
		log.Error(err1, "Failed to get team")
		return ctrl.Result{}, err1
	}
	err := r.Client.Get(ctx, types.NamespacedName{Name: userArgocRbacPolicyCM, Namespace: userArgocdNS}, found)
	if err != nil {
		log.Error(err, "Failed to get cm")
		return ctrl.Result{}, err
	}
	log.Info("users")
	for _, user := range team.Spec.Argo.Admin.Users {
		log.Info(user)
	}
	group := &userv1.Group{}
	groupName := req.Name + "-admin"
	err2 := r.Client.Get(ctx, types.NamespacedName{Name: groupName, Namespace: ""}, group)
	if err2 != nil {
		log.Error(err2, "Failed get group")
		return ctrl.Result{}, err
	}
	//check user exit to add it to group
	for _, user := range team.Spec.Argo.Admin.Users {
		duplicateUser := false
		user1 := &userv1.User{}
		errUser := r.Client.Get(ctx, types.NamespacedName{Name: user, Namespace: ""}, user1)
		for _, grpuser := range group.Users {
			if user == grpuser {
				duplicateUser = true
			}
		}
		if !duplicateUser && errUser == nil {
			group.Users = append(group.Users, user)
		}
	}
	err3 := r.Client.Update(ctx, group)
	if err3 != nil {
		log.Error(err3, "group doesnt exist")
		return ctrl.Result{}, err
	}
	//add argocd rbac policy
	log.Info("in setRBACArgoCDUser")
	log.Info(req.Name + "-Admin-CI")
	newPolicy := "g," + req.Name + "-Admin-CI,role: : " + req.Name + "-admin"
	duplicatePolicy := false
	for _, line := range strings.Split(found.Data["policy.csv"], "\n") {
		if newPolicy == line {
			duplicatePolicy = true
		}
		log.Info(line)
	}
	if foundYaml, err := yaml.Marshal(found); err != nil {
		log.Error(err, "marshal failed")
	} else {
		log.Info("--------------------------------------")
		log.Info(fmt.Sprintf("%+v", foundYaml))
		log.Info("--------------------------------------")
		log.Info(fmt.Sprintf("%s", string(foundYaml)))
	}
	if !duplicatePolicy {
		found.Data["policy.csv"] = found.Data["policy.csv"] + "\n" + newPolicy
		errRbac := r.Client.Update(ctx, found)
		if errRbac != nil {
			log.Error(err3, "error in updating argocd-rbac-cm")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
func (r *TeamReconciler) setRBACArgoCDViewUser(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	reqLogger := log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling team")
	team := &teamv1alpha1.Team{}
	found := &corev1.ConfigMap{}
	err1 := r.Client.Get(context.TODO(), req.NamespacedName, team)
	if err1 != nil {
		log.Error(err1, "Failed to get  team")
		return ctrl.Result{}, err1
	}
	err := r.Client.Get(ctx, types.NamespacedName{Name: userArgocRbacPolicyCM, Namespace: userArgocdNS}, found)
	if err != nil {
		log.Error(err, "Failed to get  cm")
		return ctrl.Result{}, err
	}
	log.Info("users")
	for _, user := range team.Spec.Argo.View.Users {
		log.Info(user)
	}
	group := &userv1.Group{}
	groupName := req.Name + "-view"
	err2 := r.Client.Get(ctx, types.NamespacedName{Name: groupName, Namespace: ""}, group)
	if err2 != nil {
		log.Error(err2, "Failed get group")
		return ctrl.Result{}, err
	}
	//check user exit to add it to group
	for _, user := range team.Spec.Argo.View.Users {
		duplicateUser := false
		user1 := &userv1.User{}
		errUser := r.Client.Get(ctx, types.NamespacedName{Name: user, Namespace: ""}, user1)
		for _, grpuser := range group.Users {
			if user == grpuser {
				duplicateUser = true
			}
		}
		if !duplicateUser && errUser == nil {
			group.Users = append(group.Users, user)
		}
	}
	err3 := r.Client.Update(ctx, group)
	if err3 != nil {
		log.Error(err3, "group doesnt exist")
		return ctrl.Result{}, err
	}
	//add argocd rbac policy
	log.Info("in setRBACArgoCDUser")
	log.Info(req.Name + "-View-CI")
	newPolicy := "g," + req.Name + "-View-CI,role: " + req.Name + "-view "
	duplicatePolicy := false
	for _, line := range strings.Split(found.Data["policy.csv"], "\n") {
		if newPolicy == line {
			duplicatePolicy = true
		}
		log.Info(line)
	}
	if !duplicatePolicy {
		found.Data["policy.csv"] = fmt.Sprintf("\n%v\n%s", string(found.Data["policy.csv"]), newPolicy)
		//found.Data["policy.csv"] = found.Data["policy.csv"] + "\n" + newPolicy
		errRbac := r.Client.Update(ctx, found)
		if errRbac != nil {
			log.Error(err3, "error in updating argocd-rbac-cm")
			return ctrl.Result{}, err
		}
	}

	foundYaml, err := yaml.Marshal(found)
	if err != nil {
		log.Error(err, "marshal failed")
	}
	found.Data["policy.csv"] = fmt.Sprintf("%s%v  '  '", string(foundYaml), '\n')
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TeamReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&teamv1alpha1.Team{}).
		Complete(r)
}
