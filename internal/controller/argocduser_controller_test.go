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
	"time"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	userv1 "github.com/openshift/api/user/v1"
	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Argocduser controller", func() {
	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		timeout      = time.Second * 20
		interval     = time.Millisecond * 30
		argocdAppsNs = "user-argocd"
		testAuName   = "test-au"
	)
	var (
		testAuAdminCIPass = "some_admin_pass"
		testAuViewCIPass  = "some_view_pass"
		testAuAdminUsers  = []string{"admin-a", "admin-b"}
		testAuviewUsers   = []string{"view-a", "view-b"}
	)
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
			Expect(clusterRoleBinding.RoleRef.Name).Should(Equal(expectedRoleRefName))
			Expect(clusterRoleBinding.Subjects).Should(ContainElements(expectedSubjects))
		})
		It("Should create & verify the corresponding Group", func() {
			Skip("OpenShift Groups require real OpenShift cluster - test in E2E")
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
			Eventually(func() error {
				return k8sClient.Get(ctx, lookup, appProj)
			}, timeout, interval).Should(Succeed())

			// TODO
			// make sure appproject has the correct spec.

		})
		It("Should update or create the `argocd-cm` ConfigMap with required accounts", func() {
			// TODO
			By("Verifying argocd-cm is updated with accounts.<ACCOUNT-admin>")
			// Eventually(func() bool {
			// 	cm := &corev1.ConfigMap{}
			// 	lookup := types.NamespacedName{Name: "argocd-cm", Namespace: argocdAppsNs}
			// 	err := k8sClient.Get(ctx, lookup, cm)
			// 	if err != nil {
			// 		return false
			// 	}

			// 	// Check for admin CI account
			// 	expectedKey := "accounts." + testAuName + "-admin-ci"
			// 	value, exists := cm.Data[expectedKey]
			// 	return exists && value == "apiKey,login"
			// }, timeout, interval).Should(BeTrue())

			// TODO
			By("Verifying argocd-cm is updated with accounts.<ACCOUNT-view>")
			// Eventually(func() bool {
			// 	cm := &corev1.ConfigMap{}
			// 	err := k8sClient.Get(ctx,
			// 		types.NamespacedName{Name: "argocd-cm", Namespace: argocdAppsNs},
			// 		cm)
			// 	if err != nil {
			// 		return false
			// 	}

			// 	// Check for view CI account
			// 	expectedKey := "accounts." + testAuName + "-view-ci"
			// 	value, exists := cm.Data[expectedKey]
			// 	return exists && value == "apiKey,login"
			// }, timeout, interval).Should(BeTrue())
		})
		It("Should update or create the `argocd-secret` Secret with required accounts", func() {
			// TODO
			By("Verifying argocd-secret is updated with accounts.<ACCOUNT-admin>.password")
			// TODO
			By("Verifying argocd-secret is updated with accounts.<ACCOUNT-view>.password")
		})
		It("Should update ConfigMaps", func() {
			// WIP TODO

			// By("Verifying argocd-rbac-cm is updated with policies")
			// Eventually(func() bool {
			// 	cm := &corev1.ConfigMap{}
			// 	err := k8sClient.Get(ctx,
			// 		types.NamespacedName{Name: "argocd-rbac-cm", Namespace: argocdAppsNs},
			// 		cm)
			// 	if err != nil {
			// 		return false
			// 	}

			// 	// Check that policy.csv contains our team
			// 	policyCSV, exists := cm.Data["policy.csv"]
			// 	if !exists {
			// 		return false
			// 	}

			// 	// Should contain role definitions
			// 	return strings.Contains(policyCSV, "role:"+testAuName+"-admin")
			// }, timeout, interval).Should(BeTrue())

		})
	})
})
