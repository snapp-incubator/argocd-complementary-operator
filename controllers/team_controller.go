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
	teamv1 "github.com/snapp-incubator/team-operator/api/v1"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var logf = log.Log.WithName("controller_team")

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
	reqLogger := logf.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling team")
	team := &teamv1.Team{}

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
		log.Info("team is found and teamAdmin is : " + team.Spec.TeamAdmin)

	}
	r.createArgocdStaticUser(ctx, req)
	return ctrl.Result{}, nil
}
func (r *TeamReconciler) createArgocdStaticUser(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	reqLogger := logf.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling team")
	team := &teamv1.Team{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, team)

	log.Info("team is found and teamAdmin is : " + team.Spec.TeamAdmin)
	staticUser := map[string]map[string]string{
		"data": {
			"accounts." + team.Spec.Argo.Tokens.ArgocdUser: "apiKey,login",
		},
	}
	staticUserByte, _ := json.Marshal(staticUser)
	err = r.Client.Patch(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "argocd",
			Name:      "argocd-cm",
		},
	}, client.RawPatch(types.StrategicMergePatchType, staticUserByte))
	if err != nil {
		log.Error(err, "Failed to patch cm")
		return ctrl.Result{}, err
	}
	//set password to the user
	hash, _ := HashPassword(team.Spec.Argo.Tokens.ArgocdPass) // ignore error for the sake of simplicity

	encodedPass := b64.StdEncoding.EncodeToString([]byte(hash))
	staticPassword := map[string]map[string]string{
		"data": {
			"accounts." + team.Spec.Argo.Tokens.ArgocdUser + ".password": encodedPass,
		},
	}
	staticPassByte, _ := json.Marshal(staticPassword)

	err = r.Client.Patch(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "argocd",
			Name:      "argocd-secret",
		},
	}, client.RawPatch(types.StrategicMergePatchType, staticPassByte))
	if err != nil {
		log.Error(err, "Failed to patch secret")
		return ctrl.Result{}, err
	}
	r.setRBACArgoCDUser(ctx, req)

	return ctrl.Result{}, nil
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func (r *TeamReconciler)setRBACArgoCDUser(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	reqLogger := logf.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	reqLogger.Info("Reconciling team")
	team := &teamv1.Team{}
	found := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: "argocd-cm", Namespace: "argocd"}, found)

	if err != nil {
		log.Error(err, "Failed to get  cm")
		return ctrl.Result{}, err
	}
	var policies []string
	//policies := found.Data["policy.csv"]
	if found.Data["policy.csv"] != "" {
		for _, policy := range found.Data["policy.csv"] {
			policies = append(policies, policy)
			
		}
	  newPolicy :="g," +team.Spec.Argo.Tokens.ArgocdUser+"-admin,role: " +req.Name+"-admin"
	  log.Info(newPolicy)
	  policies = append(policies, newPolicy)


	}
// 	rbac := map[string]map[string]string{
// 		"data":{
// 		"policy.csv": "g," +team.Spec.Argo.Tokens.ArgocdUser+"-admin,role: " +req.Name+"-admin",
// 	},
// }
// 	rbacByte, _ := json.Marshal(rbac)
// 	log.Info(string(rbacByte))

// 	err = r.Client.Patch(context.Background(), &corev1.ConfigMap{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Namespace: "argocd",
// 			Name:      "argocd-rbac-cm",
// 		},
// 	}, client.RawPatch(types.StrategicMergePatchType, rbacByte))
// 	if err != nil {
// 		log.Error(err, "Failed to patch rbac cm")
// 		return ctrl.Result{}, err
// 	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TeamReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&teamv1.Team{}).
		Complete(r)
}
