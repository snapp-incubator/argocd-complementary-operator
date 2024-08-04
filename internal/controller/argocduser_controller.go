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
	"strings"

	userv1 "github.com/openshift/api/user/v1"
	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"
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

const (
	userArgocdNS           = "user-argocd"
	userArgocdRbacPolicyCM = "argocd-rbac-cm"
	userArgocdStaticUserCM = "argocd-cm"
	userArgocdSecret       = "argocd-secret"
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
//+kubebuilder:rbac:groups=user.openshift.io,resources=*,verbs=get;list;watch;create;update;patch;delete

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
		log.Error(err, "Failed to get argocduser")
		return ctrl.Result{}, err
	}

	_, err = r.createArgocdStaticUser(ctx, req, argocduser, "admin", argocduser.Spec.Admin.CIPass, argocduser.Spec.Admin.Users)
	if err != nil {
		log.Error(err, "Failed create argocd static user admin")
		return ctrl.Result{}, err
	}
	_, err = r.createArgocdStaticUser(ctx, req, argocduser, "view", argocduser.Spec.View.CIPass, argocduser.Spec.View.Users)
	if err != nil {
		log.Error(err, "Failed create argocd static user view")
		return ctrl.Result{}, err
	}
	err = r.AddArgocdRBACPolicy(ctx, argocduser)
	if err != nil {
		log.Error(err, "Failed to add argocd rbac policy")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ArgocdUserReconciler) createArgocdStaticUser(ctx context.Context, _ ctrl.Request, argocduser *argocduserv1alpha1.ArgocdUser, roleName string, ciPass string, argoUsers []string) (ctrl.Result, error) {
	err := r.UpdateUserArgocdConfig(ctx, argocduser, roleName, ciPass)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = r.AddArgoUsersToGroup(ctx, argocduser, roleName, argoUsers)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
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
				},
			}
			err = r.Create(ctx, group)
			if err != nil {
				log.Error(err, "Failed to create group")
				return err
			}
		}
	}
	group.Users = argoUsers
	err = r.Update(ctx, group)
	if err != nil {
		log.Error(err, "Failed to update group")
		return err
	}
	log.Info("Successfully added users to group")
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

	policies := []string{
		"g, " + argocduser.Name + "-admin-ci, role:" + argocduser.Name + "-admin",
		"g, " + argocduser.Name + "-admin-ci, role:" + argocduser.Name + "-view",
		"g, " + argocduser.Name + "-admin-ci, role:common",
		"g, " + argocduser.Name + "-view-ci, role:common",
		"g, " + argocduser.Name + "-view-ci, role:" + argocduser.Name + "-view",
		"g, " + argocduser.Name + "-admin, role:" + argocduser.Name + "-admin",
		"g, " + argocduser.Name + "-admin, role:" + argocduser.Name + "-view",
		"g, " + argocduser.Name + "-admin, role:common",
		"g, " + argocduser.Name + "-view, role:common",
		"g, " + argocduser.Name + "-view, role:" + argocduser.Name + "-view",
		"p, role:" + argocduser.Name + "-admin, repositories, create, " + argocduser.Name + "/*, allow",
		"p, role:" + argocduser.Name + "-admin, repositories, delete, " + argocduser.Name + "/*, allow",
		"p, role:" + argocduser.Name + "-admin, repositories, update, " + argocduser.Name + "/*, allow",
		"p, role:" + argocduser.Name + "-view, repositories, get, " + argocduser.Name + "/*, allow",
		"p, role:" + argocduser.Name + "-view, applications, get, " + argocduser.Name + "/*, allow",
		"p, role:" + argocduser.Name + "-admin, exec, create, " + argocduser.Name + "/*, allow",
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

// SetupWithManager sets up the controller with the Manager.
func (r *ArgocdUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&argocduserv1alpha1.ArgocdUser{}).
		Complete(r)
}
