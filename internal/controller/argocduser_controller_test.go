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

package controller_test

import (
	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	userv1 "github.com/openshift/api/user/v1"
	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var ()

var _ = Describe("Argocduser controller", func() {
	logger := logf.FromContext(ctx)
	// Creating AppProj as soon as we create an argocduser object
	Context("When creating argocduser", func() {
		var (
			// Argocduser controller `Create`` tests variables
			testAuName        = "test-au"
			testAuAdminCIPass = "some_admin_pass"
			testAuViewCIPass  = "some_view_pass"
			testAuAdminUsers  = []string{"admin-a", "admin-b"}
			testAuviewUsers   = []string{"view-a", "view-b"}
		)
		It("Should create & verify the ArgocdUser is created", func() {
			By("Verifying Argocduser named test-au exists or created")
			_ = ensureArgocdUserExists(ctx, testAuName, argocduserv1alpha1.ArgocdUserSpec{
				Admin: argocduserv1alpha1.ArgocdCIAdmin{
					CIPass: testAuAdminCIPass,
					Users:  testAuAdminUsers,
				},
				View: argocduserv1alpha1.ArgocdCIView{
					CIPass: testAuViewCIPass,
					Users:  testAuviewUsers,
				},
			})
		})
		It("Should create & verify the corresponding ClusterRole", func() {
			By("Verifying test-au-argocduser-clusterrole ClusterRole is created")
			expectedRules := []rbacv1.PolicyRule{{
				Verbs:         []string{"get", "patch", "update", "edit"},
				Resources:     []string{"argocdusers"},
				APIGroups:     []string{"argocd.snappcloud.io"},
				ResourceNames: []string{testAuName},
			}}
			clusterRole := &rbacv1.ClusterRole{}
			lookup := types.NamespacedName{Name: testAuName + "-argocduser-clusterrole"}
			Eventually(func() error {
				return k8sClient.Get(ctx, lookup, clusterRole)
			}, timeout, interval).Should(Succeed())

			By("Verify test-au-argocduser-clusterrole ClusterRole has correct spec")
			Expect(clusterRole.ObjectMeta.OwnerReferences[0].APIVersion).Should(Equal("argocd.snappcloud.io/v1alpha1"))
			Expect(clusterRole.ObjectMeta.OwnerReferences[0].Kind).Should(Equal("ArgocdUser"))
			Expect(clusterRole.ObjectMeta.OwnerReferences[0].Name).Should(Equal(testAuName))
			Expect(clusterRole.Rules).Should(ConsistOf(expectedRules))
		})
		It("Should create & verify the corresponding ClusterRoleBinding", func() {
			By("Verifying test-au-argocduser-clusterrolebinding ClusterRoleBinding is created")
			expectedRoleRefName := testAuName + "-argocduser-clusterrole"
			expectedSubjects := []rbacv1.Subject{}
			for _, user := range testAuAdminUsers {
				expectedSubjects = append(expectedSubjects, rbacv1.Subject{
					Kind:     "User",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     user,
				})
			}

			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			lookup := types.NamespacedName{Name: testAuName + "-argocduser-clusterrolebinding"}
			Eventually(func() error {
				return k8sClient.Get(ctx, lookup, clusterRoleBinding)
			}, timeout, interval).Should(Succeed())

			By("Verify test-au-argocduser-clusterrolebinding ClusterRoleBinding has correct spec")
			Expect(clusterRoleBinding.ObjectMeta.OwnerReferences[0].APIVersion).Should(Equal("argocd.snappcloud.io/v1alpha1"))
			Expect(clusterRoleBinding.ObjectMeta.OwnerReferences[0].Kind).Should(Equal("ArgocdUser"))
			Expect(clusterRoleBinding.ObjectMeta.OwnerReferences[0].Name).Should(Equal(testAuName))
			Expect(clusterRoleBinding.RoleRef.Name).Should(Equal(expectedRoleRefName))
			Expect(clusterRoleBinding.Subjects).Should(ContainElements(expectedSubjects))
		})
		It("Should create & verify the corresponding Group", func() {
			// Check if Group CRD is registered in scheme
			if !k8sClient.Scheme().Recognizes(userv1.GroupVersion.WithKind("Group")) {
				Skip("OpenShift Group kind is not registered in scheme. Test them in E2E tests - skipping Group tests")
			}
			By("Verifying admin Group is created")
			adminGroup := &userv1.Group{}
			adminGroupLookup := types.NamespacedName{Name: testAuName + "-admin"}
			Eventually(func() error {
				return k8sClient.Get(ctx, adminGroupLookup, adminGroup)
			}, timeout, interval).Should(Succeed())

			By("Verify admin group has correct users")
			Expect(adminGroup.Users).Should(ContainElements(testAuAdminUsers))

			By("Verifying view Group is created")
			viewGroup := &userv1.Group{}
			viewGroupLookup := types.NamespacedName{Name: testAuName + "-view"}
			Eventually(func() error {
				return k8sClient.Get(ctx, viewGroupLookup, viewGroup)
			}, timeout, interval).Should(Succeed())

			By("Verify view group has correct users")
			Expect(viewGroup.Users).Should(ContainElements(testAuviewUsers))
		})
		It("Should create & verify the corresponding AppProject", func() {
			appProj := &argov1alpha1.AppProject{}
			lookup := types.NamespacedName{Name: testAuName, Namespace: argocdAppsNs}
			expectedAdminPolicies := []string{
				"p, proj:" + testAuName + ":" + testAuName + "-admin, applications, *, " + testAuName + "/*, allow",
				"p, proj:" + testAuName + ":" + testAuName + "-admin, repositories, *, " + testAuName + "/*, allow",
				"p, proj:" + testAuName + ":" + testAuName + "-admin, exec, create, " + testAuName + "/*, allow",
			}
			expectedAdminGroups := []string{
				"test-au-admin",
				"test-au-admin-ci",
			}
			expectedViewPolicies := []string{
				"p, proj:" + testAuName + ":" + testAuName + "-view, applications, get, " + testAuName + "/*, allow",
				"p, proj:" + testAuName + ":" + testAuName + "-view, repositories, get, " + testAuName + "/*, allow",
				"p, proj:" + testAuName + ":" + testAuName + "-view, logs, get, " + testAuName + "/*, allow",
			}
			expectedViewGroups := []string{
				"test-au-admin",
				"test-au-admin-ci",
				"test-au-view",
				"test-au-view-ci",
			}
			Eventually(func() error {
				return k8sClient.Get(ctx, lookup, appProj)
			}, timeout, interval).Should(Succeed())
			Expect(appProj.Spec.Roles[0].Name).To(Equal(testAuName + "-admin"))
			Expect(appProj.Spec.Roles[0].Policies).To(ContainElements(expectedAdminPolicies))
			Expect(appProj.Spec.Roles[0].Groups).To(ContainElements(expectedAdminGroups))
			Expect(appProj.Spec.Roles[1].Name).To(Equal(testAuName + "-view"))
			Expect(appProj.Spec.Roles[1].Policies).To(ContainElements(expectedViewPolicies))
			Expect(appProj.Spec.Roles[1].Groups).To(ContainElements(expectedViewGroups))
		})
		It("Should update the `argocd-cm` ConfigMap and create required accounts", func() {
			var expectedKey string
			cm := &corev1.ConfigMap{}
			lookup := types.NamespacedName{Name: "argocd-cm", Namespace: argocdAppsNs}

			// TODO: Enhance this
			By("Verifying argocd-cm is updated with accounts.<ACCOUNT-admin>")
			expectedKey = "accounts." + testAuName + "-admin-ci"
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookup, cm)
				if err != nil {
					logger.Error(err, "ConfigMap not found yet")
					return false
				}
				// Check for admin CI account
				value, exists := cm.Data[expectedKey]
				return exists && value == "apiKey,login"
			}, timeout, interval).Should(BeTrue())

			// TODO: Enhance this
			By("Verifying argocd-cm is updated with accounts.<ACCOUNT-view>")
			expectedKey = "accounts." + testAuName + "-view-ci"
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookup, cm)
				if err != nil {
					logger.Error(err, "ConfigMap not found yet")
					return false
				}
				// Check for admin CI account
				value, exists := cm.Data[expectedKey]
				return exists && value == "apiKey,login"
			}, timeout, interval).Should(BeTrue())
		})
		It("Should update the `argocd-secret` Secret and create required accounts", func() {
			var expectedKey string
			sec := &corev1.Secret{}
			lookup := types.NamespacedName{Name: "argocd-secret", Namespace: argocdAppsNs}

			// Check for admin CI account
			By("Verifying argocd-secret is updated with accounts.<ACCOUNT-admin>.password")
			expectedKey = "accounts." + testAuName + "-admin-ci.password"
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookup, sec)
				if err != nil {
					logger.Error(err, "Secret not found yet")
					return false
				}
				value, exists := sec.Data[expectedKey]
				if exists {
					// sec.Data already contains decoded bytes (Kubernetes auto-decodes base64)
					// value is the bcrypt hash as bytes, ready to use directly
					err = bcrypt.CompareHashAndPassword(value, []byte(testAuAdminCIPass))
					if err != nil {
						logger.Error(err, "Password hash mismatch for 'admin' user")
						return false
					}
				}
				return exists
			}, timeout, interval).Should(BeTrue())

			// Check for view CI account
			By("Verifying argocd-secret is updated with accounts.<ACCOUNT-view>.password")
			expectedKey = "accounts." + testAuName + "-view-ci.password"
			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookup, sec)
				if err != nil {
					logger.Error(err, "Secret not found yet")
					return false
				}
				value, exists := sec.Data[expectedKey]
				if exists {
					// sec.Data already contains decoded bytes (Kubernetes auto-decodes base64)
					// value is the bcrypt hash as bytes, ready to use directly
					err = bcrypt.CompareHashAndPassword(value, []byte(testAuViewCIPass))
					if err != nil {
						logger.Error(err, "Password hash mismatch for 'view' user")
						return false
					}
				}
				return exists
			}, timeout, interval).Should(BeTrue())
		})
	})
	Context("When deleting argocduser", Ordered, func() {
		// Use a separate ArgocdUser for deletion tests to avoid affecting other tests
		var deleteAuName = "delete-test-au"
		var deleteAu *argocduserv1alpha1.ArgocdUser

		BeforeAll(func() {
			By("Creating ArgocdUser for deletion tests")
			deleteAu = ensureArgocdUserExists(ctx, deleteAuName, argocduserv1alpha1.ArgocdUserSpec{
				Admin: argocduserv1alpha1.ArgocdCIAdmin{
					CIPass: "delete-admin-pass",
					Users:  []string{"delete-admin-user"},
				},
				View: argocduserv1alpha1.ArgocdCIView{
					CIPass: "delete-view-pass",
					Users:  []string{"delete-view-user"},
				},
			})
			By("Waiting for all resources to be created")

			// Wait for ClusterRole to exist
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name: deleteAuName + "-argocduser-clusterrole",
				}, &rbacv1.ClusterRole{})
			}, timeout, interval).Should(Succeed())

			// Wait for ClusterRoleBinding to exist
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name: deleteAuName + "-argocduser-clusterrolebinding",
				}, &rbacv1.ClusterRoleBinding{})
			}, timeout, interval).Should(Succeed())

			// Check if Group CRD is registered in scheme and wait for groups to be created
			if k8sClient.Scheme().Recognizes(userv1.GroupVersion.WithKind("Group")) {
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: deleteAuName + "-view"}, &userv1.Group{})).To(Succeed())
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: deleteAuName + "-admin"}, &userv1.Group{})).To(Succeed())
				}, timeout, interval).Should(Succeed())
			} else {
				logger.Info("Group CRD is not registered, no need to wait for groups")
			}

			// Wait for AppProject to exist
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      deleteAuName,
					Namespace: argocdAppsNs,
				}, &argov1alpha1.AppProject{})
			}, timeout, interval).Should(Succeed())
		})
		It("Should delete & verify the ArgocdUser is delete", func() {
			By("Deleting ArgocdUser")
			Expect(k8sClient.Delete(ctx, deleteAu)).Should(Succeed())

			By("Verifying ArgocdUser is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: deleteAuName}, &argocduserv1alpha1.ArgocdUser{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
		It("Should delete & verify the corresponding ClusterRole", func() {
			By("Verifying ClusterRole is deleted")
			// Check if the ClusterRole has correct OwnerReferences
			clusterRole := &rbacv1.ClusterRole{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: deleteAuName + "-argocduser-clusterrole",
			}, clusterRole)
			Expect(err).To(Succeed())
			Expect(clusterRole.ObjectMeta.OwnerReferences[0].APIVersion).Should(Equal("argocd.snappcloud.io/v1alpha1"))
			Expect(clusterRole.ObjectMeta.OwnerReferences[0].Kind).Should(Equal("ArgocdUser"))
			Expect(clusterRole.ObjectMeta.OwnerReferences[0].Name).Should(Equal(deleteAuName))

			// As there is no controller-manager in envtest, we can't do the following
			// Eventually(func() bool {
			// 	err := k8sClient.Get(ctx, types.NamespacedName{
			// 		Name: deleteAuName + "-argocduser-clusterrole",
			// 	}, &rbacv1.ClusterRole{})
			// 	return errors.IsNotFound(err)
			// }, timeout, interval).Should(BeTrue())

		})
		It("Should delete & verify the corresponding ClusterRoleBinding", func() {
			By("Verifying ClusterRoleBinding is deleted")
			// Check if the ClusterRoleBinding has correct OwnerReferences
			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: deleteAuName + "-argocduser-clusterrolebinding",
			}, clusterRoleBinding)
			Expect(err).To(Succeed())
			Expect(clusterRoleBinding.ObjectMeta.OwnerReferences[0].APIVersion).Should(Equal("argocd.snappcloud.io/v1alpha1"))
			Expect(clusterRoleBinding.ObjectMeta.OwnerReferences[0].Kind).Should(Equal("ArgocdUser"))
			Expect(clusterRoleBinding.ObjectMeta.OwnerReferences[0].Name).Should(Equal(deleteAuName))

			// As there is no controller-manager in envtest, we can't do the following
			// Eventually(func() bool {
			// 	err := k8sClient.Get(ctx, types.NamespacedName{
			// 		Name: deleteAuName + "-argocduser-clusterrolebinding",
			// 	}, &rbacv1.ClusterRoleBinding{})
			// 	return errors.IsNotFound(err)
			// }, timeout, interval).Should(BeTrue())
		})
		It("Should delete & verify the corresponding Group", func() {
			if !k8sClient.Scheme().Recognizes(userv1.GroupVersion.WithKind("Group")) {
				Skip("OpenShift Group kind is not registered in scheme - skipping Group deletion test")
			}
			By("Verifying admin Group is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: deleteAuName + "-admin",
				}, &userv1.Group{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("Verifying view Group is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: deleteAuName + "-view",
				}, &userv1.Group{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
		It("Should delete & verify the corresponding AppProject", func() {
			// TODO: implement finalizer in future
			By("Verifying AppProject is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      deleteAuName,
					Namespace: argocdAppsNs,
				}, &argov1alpha1.AppProject{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
		It("Should update the `argocd-cm` ConfigMap and delete required accounts", func() {
			By("Verifying admin account is removed from argocd-cm")
			cm := &corev1.ConfigMap{}
			lookup := types.NamespacedName{Name: "argocd-cm", Namespace: argocdAppsNs}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lookup, cm)).To(Succeed())
				_, adminExists := cm.Data["accounts."+deleteAuName+"-admin-ci"]
				_, viewExists := cm.Data["accounts."+deleteAuName+"-view-ci"]
				g.Expect(adminExists).To(BeFalse(), "admin account should be removed")
				g.Expect(viewExists).To(BeFalse(), "view account should be removed")
			}, timeout, interval).Should(Succeed())
		})
		It("Should update the `argocd-secret` Secret and delete required accounts", func() {
			By("Verifying passwords are removed from argocd-secret")
			sec := &corev1.Secret{}
			lookup := types.NamespacedName{Name: "argocd-secret", Namespace: argocdAppsNs}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lookup, sec)).To(Succeed())
				_, adminExists := sec.Data["accounts."+deleteAuName+"-admin-ci.password"]
				_, viewExists := sec.Data["accounts."+deleteAuName+"-view-ci.password"]
				g.Expect(adminExists).To(BeFalse(), "admin password should be removed")
				g.Expect(viewExists).To(BeFalse(), "view password should be removed")
			}, timeout, interval).Should(Succeed())
		})
	})
	Context("When updating argocduser", Ordered, func() {
		var (
			// Argocduser controller `Update`` tests variables
			updateAuName    = "update-test-au"
			initAdminUsers  = []string{"original-admin-user"}
			initViewUsers   = []string{"original-view-user"}
			initAdminCIPass = "original-admin-pass"
			initViewCIPass  = "original-view-pass"
			newAdminUsers   = []string{"new-admin-user-1", "new-admin-user-2"}
			newViewUsers    = []string{"new-view-user-1"}
			newAdminCIPass  = "new-admin-ci-pass"
			newViewCIPass   = "new-view-ci-pass"
		)

		BeforeAll(func() {
			By("Creating ArgocdUser for update tests")
			_ = ensureArgocdUserExists(ctx, updateAuName, argocduserv1alpha1.ArgocdUserSpec{
				Admin: argocduserv1alpha1.ArgocdCIAdmin{
					CIPass: initAdminCIPass,
					Users:  initAdminUsers,
				},
				View: argocduserv1alpha1.ArgocdCIView{
					CIPass: initViewCIPass,
					Users:  initViewUsers,
				},
			})

			By("Waiting for all resources to be created")
			// Wait for ClusterRole to exist
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name: updateAuName + "-argocduser-clusterrole",
				}, &rbacv1.ClusterRole{})
			}, timeout, interval).Should(Succeed())

			// Wait for ClusterRoleBinding to exist
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name: updateAuName + "-argocduser-clusterrolebinding",
				}, &rbacv1.ClusterRoleBinding{})
			}, timeout, interval).Should(Succeed())

			// Check if Group CRD is registered in scheme and wait for groups to be created
			if k8sClient.Scheme().Recognizes(userv1.GroupVersion.WithKind("Group")) {
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: updateAuName + "-view"}, &userv1.Group{})).To(Succeed())
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: updateAuName + "-admin"}, &userv1.Group{})).To(Succeed())
				}, timeout, interval).Should(Succeed())
			} else {
				logger.Info("Group CRD is not registered, no need to wait for groups")
			}

			// Wait for AppProject to exist
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      updateAuName,
					Namespace: argocdAppsNs,
				}, &argov1alpha1.AppProject{})
			}, timeout, interval).Should(Succeed())
		})
		It("Should update & verify the ArgocdUser is updated", func() {
			By("Fetching the latest ArgocdUser")
			au := &argocduserv1alpha1.ArgocdUser{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: updateAuName}, au)).To(Succeed())

			By("Updating ArgocdUser with new users and passwords")
			au.Spec.Admin.Users = newAdminUsers
			au.Spec.Admin.CIPass = newAdminCIPass
			au.Spec.View.Users = newViewUsers
			au.Spec.View.CIPass = newViewCIPass
			Expect(k8sClient.Update(ctx, au)).To(Succeed())

			By("Verifying ArgocdUser is updated")
			Eventually(func(g Gomega) {
				updated := &argocduserv1alpha1.ArgocdUser{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: updateAuName}, updated)).To(Succeed())
				g.Expect(updated.Spec.Admin.Users).To(ConsistOf(newAdminUsers))
				g.Expect(updated.Spec.View.Users).To(ConsistOf(newViewUsers))
			}, timeout, interval).Should(Succeed())
		})
		It("Should update & verify the corresponding ClusterRole", func() {
			By("Verifying ClusterRole still exists with correct rules")
			clusterRole := &rbacv1.ClusterRole{}
			lookup := types.NamespacedName{Name: updateAuName + "-argocduser-clusterrole"}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lookup, clusterRole)).To(Succeed())
				g.Expect(clusterRole.Rules).To(HaveLen(1))
				g.Expect(clusterRole.Rules[0].ResourceNames).To(ContainElement(updateAuName))
			}, timeout, interval).Should(Succeed())
		})
		It("Should update & verify the corresponding ClusterRoleBinding", func() {
			By("Verifying ClusterRoleBinding has updated subjects")
			crb := &rbacv1.ClusterRoleBinding{}
			lookup := types.NamespacedName{Name: updateAuName + "-argocduser-clusterrolebinding"}

			expectedSubjects := []rbacv1.Subject{}
			for _, user := range newAdminUsers {
				expectedSubjects = append(expectedSubjects, rbacv1.Subject{
					Kind:     "User",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     user,
				})
			}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lookup, crb)).To(Succeed())
				g.Expect(crb.Subjects).To(ConsistOf(expectedSubjects))
			}, timeout, interval).Should(Succeed())

		})
		It("Should update & verify the corresponding Group", func() {
			// Check if Group CRD is registered in scheme
			if !k8sClient.Scheme().Recognizes(userv1.GroupVersion.WithKind("Group")) {
				Skip("OpenShift Group kind is not registered in scheme. Test them in E2E tests - skipping Group tests")
			}
			By("Verifying admin Group has updated users")
			Eventually(func(g Gomega) {
				adminGroup := &userv1.Group{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: updateAuName + "-admin",
				}, adminGroup)).To(Succeed())
				g.Expect(adminGroup.Users).To(ContainElements(newAdminUsers))
			}, timeout, interval).Should(Succeed())

			By("Verifying view Group has updated users")
			Eventually(func(g Gomega) {
				viewGroup := &userv1.Group{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: updateAuName + "-view",
				}, viewGroup)).To(Succeed())
				g.Expect(viewGroup.Users).To(ContainElements(newViewUsers))
			}, timeout, interval).Should(Succeed())
		})
		It("Should update & verify the corresponding AppProject", func() {
			By("Verifying AppProject roles are updated")
			appProj := &argov1alpha1.AppProject{}
			lookup := types.NamespacedName{Name: updateAuName, Namespace: argocdAppsNs}
			expectedAdminPolicies := []string{
				"p, proj:" + updateAuName + ":" + updateAuName + "-admin, applications, *, " + updateAuName + "/*, allow",
				"p, proj:" + updateAuName + ":" + updateAuName + "-admin, repositories, *, " + updateAuName + "/*, allow",
				"p, proj:" + updateAuName + ":" + updateAuName + "-admin, exec, create, " + updateAuName + "/*, allow",
			}
			expectedViewPolicies := []string{
				"p, proj:" + updateAuName + ":" + updateAuName + "-view, applications, get, " + updateAuName + "/*, allow",
				"p, proj:" + updateAuName + ":" + updateAuName + "-view, repositories, get, " + updateAuName + "/*, allow",
				"p, proj:" + updateAuName + ":" + updateAuName + "-view, logs, get, " + updateAuName + "/*, allow",
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lookup, appProj)).To(Succeed())
				g.Expect(appProj.Spec.Roles).NotTo(BeEmpty())
				// Verify admin role groups
				g.Expect(appProj.Spec.Roles[0].Groups).To(ContainElement(updateAuName + "-admin"))
				g.Expect(appProj.Spec.Roles[0].Groups).To(ContainElement(updateAuName + "-admin-ci"))
				g.Expect(appProj.Spec.Roles[0].Policies).To(ContainElements(expectedAdminPolicies))
				// Verify view role groups
				g.Expect(appProj.Spec.Roles[1].Groups).To(ContainElement(updateAuName + "-view"))
				g.Expect(appProj.Spec.Roles[1].Groups).To(ContainElement(updateAuName + "-view-ci"))
				g.Expect(appProj.Spec.Roles[1].Policies).To(ContainElements(expectedViewPolicies))
			}, timeout, interval).Should(Succeed())
		})
		It("Should update the `argocd-cm` ConfigMap and update required accounts", func() {
			By("Verifying accounts still exist in argocd-cm")
			cm := &corev1.ConfigMap{}
			lookup := types.NamespacedName{Name: "argocd-cm", Namespace: argocdAppsNs}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lookup, cm)).To(Succeed())
				adminValue, adminExists := cm.Data["accounts."+updateAuName+"-admin-ci"]
				viewValue, viewExists := cm.Data["accounts."+updateAuName+"-view-ci"]
				g.Expect(adminExists).To(BeTrue(), "admin account should exist")
				g.Expect(adminValue).To(Equal("apiKey,login"))
				g.Expect(viewExists).To(BeTrue(), "view account should exist")
				g.Expect(viewValue).To(Equal("apiKey,login"))
			}, timeout, interval).Should(Succeed())
		})
		It("Should update the `argocd-secret` Secret and update required accounts", func() {
			By("Verifying passwords are updated in argocd-secret")
			sec := &corev1.Secret{}
			lookup := types.NamespacedName{Name: "argocd-secret", Namespace: argocdAppsNs}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, lookup, sec)).To(Succeed())

				// Verify admin password hash matches new password
				adminPassHash, adminExists := sec.Data["accounts."+updateAuName+"-admin-ci.password"]
				g.Expect(adminExists).To(BeTrue(), "admin password should exist")
				err := bcrypt.CompareHashAndPassword(adminPassHash, []byte(newAdminCIPass))
				g.Expect(err).NotTo(HaveOccurred(), "admin password hash should match new password")

				// Verify view password hash matches new password
				viewPassHash, viewExists := sec.Data["accounts."+updateAuName+"-view-ci.password"]
				g.Expect(viewExists).To(BeTrue(), "view password should exist")
				err = bcrypt.CompareHashAndPassword(viewPassHash, []byte(newViewCIPass))
				g.Expect(err).NotTo(HaveOccurred(), "view password hash should match new password")
			}, timeout, interval).Should(Succeed())
		})
	})
})
