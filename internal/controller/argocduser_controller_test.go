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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("Argocduser controller", func() {
	logger := logf.FromContext(ctx)
	// Creating AppProj as soon as we create an argocduser object
	Context("When creating argocduser", func() {
		It("Should create & verify the ArgocdUser is created", func() {
			By("Creating Argocduser named test-au")
			// create test-au argocduser
			TestAu := &argocduserv1alpha1.ArgocdUser{
				ObjectMeta: metav1.ObjectMeta{
					Name: testAuName,
				},
				Spec: argocduserv1alpha1.ArgocdUserSpec{
					Admin: argocduserv1alpha1.ArgocdCIAdmin{
						CIPass: testAuAdminCIPass,
						Users:  testAuAdminUsers,
					},
					View: argocduserv1alpha1.ArgocdCIView{
						CIPass: testAuViewCIPass,
						Users:  testAuviewUsers,
					},
				},
			}
			Expect(k8sClient.Create(ctx, TestAu)).Should(Succeed())

			By("Verifying Argocduser named test-au")
			lookup := types.NamespacedName{Name: testAuName}
			// Also can use lookup := client.ObjectKey{Name: testAuName}

			au := &argocduserv1alpha1.ArgocdUser{}
			Expect(k8sClient.Get(ctx, lookup, au)).Should(Succeed())
			Expect(au.Spec).Should(Equal(TestAu.Spec))
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
		It("Should update or create the `argocd-cm` ConfigMap with required accounts", func() {
			var expectedKey string
			cm := &corev1.ConfigMap{}
			lookup := types.NamespacedName{Name: "argocd-cm", Namespace: argocdAppsNs}

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
		It("Should update or create the `argocd-secret` Secret with required accounts", func() {
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
})
