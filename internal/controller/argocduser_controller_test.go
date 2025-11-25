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
	"context"
	"strings"
	"time"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	userv1 "github.com/openshift/api/user/v1"
	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("ArgocdUser controller RBAC policy generation", Ordered, func() {
	const (
		timeout  = time.Second * 20
		interval = time.Millisecond * 30
	)

	ctx := context.Background()

	BeforeAll(func() {
		By("Ensuring user-argocd namespace exists")
		userArgocdNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "user-argocd",
			},
		}
		err := k8sClient.Create(ctx, userArgocdNS)
		if err != nil {
			// Namespace might already exist
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "user-argocd"}, userArgocdNS)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Context("When creating an ArgocdUser resource", func() {
		It("Should generate correct RBAC policies in ConfigMap", func() {
			By("Creating the argocd-rbac-cm ConfigMap if it doesn't exist")
			rbacConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-rbac-cm",
					Namespace: "user-argocd",
				},
				Data: map[string]string{
					"policy.csv": "",
				},
			}
			err := k8sClient.Create(ctx, rbacConfigMap)
			if err != nil {
				// ConfigMap might already exist from previous tests
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd-rbac-cm",
					Namespace: "user-argocd",
				}, rbacConfigMap)
				Expect(err).NotTo(HaveOccurred())
			}

			By("Creating the argocd-cm ConfigMap if it doesn't exist")
			argocdConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-cm",
					Namespace: "user-argocd",
				},
				Data: map[string]string{},
			}
			err = k8sClient.Create(ctx, argocdConfigMap)
			if err != nil {
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd-cm",
					Namespace: "user-argocd",
				}, argocdConfigMap)
				Expect(err).NotTo(HaveOccurred())
			}

			By("Creating the argocd-secret Secret if it doesn't exist")
			argocdSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-secret",
					Namespace: "user-argocd",
				},
				Data: map[string][]byte{},
			}
			err = k8sClient.Create(ctx, argocdSecret)
			if err != nil {
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "argocd-secret",
					Namespace: "user-argocd",
				}, argocdSecret)
				Expect(err).NotTo(HaveOccurred())
			}

			By("Creating an ArgocdUser resource")
			argocdUser := &argocduserv1alpha1.ArgocdUser{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-team",
				},
				Spec: argocduserv1alpha1.ArgocdUserSpec{
					Admin: argocduserv1alpha1.ArgocdCIAdmin{
						CIPass: "admin-password",
						Users:  []string{"admin-user1", "admin-user2"},
					},
					View: argocduserv1alpha1.ArgocdCIView{
						CIPass: "view-password",
						Users:  []string{"view-user1"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, argocdUser)).Should(Succeed())

			By("Verifying the ArgocdUser was created")
			argocdUserLookup := types.NamespacedName{Name: "test-team"}
			Expect(k8sClient.Get(ctx, argocdUserLookup, argocdUser)).Should(Succeed())

			By("Waiting for RBAC policies to be added to ConfigMap")
			updatedConfigMap := &corev1.ConfigMap{}
			configMapLookup := types.NamespacedName{
				Name:      "argocd-rbac-cm",
				Namespace: "user-argocd",
			}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookup, updatedConfigMap)
				if err != nil {
					return false
				}
				// Check if at least one expected policy exists
				return strings.Contains(updatedConfigMap.Data["policy.csv"], "test-team-admin-ci")
			}, timeout, interval).Should(BeTrue())

			By("Verifying global common role definition is present")
			policyCsv := updatedConfigMap.Data["policy.csv"]

			// Common role definition - allows all users to get clusters
			Expect(policyCsv).To(ContainSubstring("p, role:common, clusters, get, *, allow"),
				"Should define common role with clusters get permission")

			By("Verifying group bindings to common role are present")
			// All groups should be bound to common role
			Expect(policyCsv).To(ContainSubstring("g, test-team-admin-ci, role:common"),
				"Should assign admin-ci to common role")
			Expect(policyCsv).To(ContainSubstring("g, test-team-view-ci, role:common"),
				"Should assign view-ci to common role")
			Expect(policyCsv).To(ContainSubstring("g, test-team-admin, role:common"),
				"Should assign admin to common role")
			Expect(policyCsv).To(ContainSubstring("g, test-team-view, role:common"),
				"Should assign view to common role")

			By("Verifying fine-grained policies are NOT in policy.csv (they belong in AppProject)")
			// Fine-grained policies should NOT be in global config
			Expect(policyCsv).NotTo(ContainSubstring("role:test-team-admin, repositories"),
				"Repository policies should be in AppProject, not global config")
			Expect(policyCsv).NotTo(ContainSubstring("role:test-team-view, applications"),
				"Application policies should be in AppProject, not global config")
		})

		It("Should create static users in argocd-cm ConfigMap", func() {
			By("Verifying admin-ci and view-ci accounts are created")
			configMap := &corev1.ConfigMap{}
			configMapLookup := types.NamespacedName{
				Name:      "argocd-cm",
				Namespace: "user-argocd",
			}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, configMapLookup, configMap)
				if err != nil {
					return false
				}
				// Check if accounts are created
				_, adminExists := configMap.Data["accounts.test-team-admin-ci"]
				_, viewExists := configMap.Data["accounts.test-team-view-ci"]
				return adminExists && viewExists
			}, timeout, interval).Should(BeTrue())

			// Verify the account capabilities
			Expect(configMap.Data["accounts.test-team-admin-ci"]).To(Equal("apiKey,login"))
			Expect(configMap.Data["accounts.test-team-view-ci"]).To(Equal("apiKey,login"))
		})
	})

	Context("When updating an ArgocdUser resource", func() {
		It("Should not duplicate RBAC policies on reconciliation", func() {
			By("Getting the existing ArgocdUser")
			argocdUser := &argocduserv1alpha1.ArgocdUser{}
			argocdUserLookup := types.NamespacedName{Name: "test-team"}
			Expect(k8sClient.Get(ctx, argocdUserLookup, argocdUser)).Should(Succeed())

			By("Updating the ArgocdUser spec")
			argocdUser.Spec.Admin.Users = []string{"admin-user1", "admin-user2", "admin-user3"}
			Expect(k8sClient.Update(ctx, argocdUser)).Should(Succeed())

			By("Waiting for reconciliation")
			time.Sleep(2 * time.Second)

			By("Verifying policies are not duplicated")
			configMap := &corev1.ConfigMap{}
			configMapLookup := types.NamespacedName{
				Name:      "argocd-rbac-cm",
				Namespace: "user-argocd",
			}
			Expect(k8sClient.Get(ctx, configMapLookup, configMap)).Should(Succeed())

			policyCsv := configMap.Data["policy.csv"]

			// Count occurrences of a specific policy to ensure no duplicates
			testPolicy := "g, test-team-admin-ci, role:common"
			occurrences := strings.Count(policyCsv, testPolicy)
			Expect(occurrences).To(Equal(1), "Policy should appear exactly once, not be duplicated")

			// Also verify the common role definition appears only once
			commonRolePolicy := "p, role:common, clusters, get, *, allow"
			commonRoleOccurrences := strings.Count(policyCsv, commonRolePolicy)
			Expect(commonRoleOccurrences).To(Equal(1), "Common role definition should appear exactly once")
		})
	})

	Context("When creating an ArgocdUser with AppProject management", func() {
		It("Should create AppProject with correct RBAC roles", func() {
			By("Creating a new ArgocdUser")
			newUser := &argocduserv1alpha1.ArgocdUser{
				ObjectMeta: metav1.ObjectMeta{
					Name: "project-team",
				},
				Spec: argocduserv1alpha1.ArgocdUserSpec{
					Admin: argocduserv1alpha1.ArgocdCIAdmin{
						CIPass: "admin-pass",
						Users:  []string{"admin1"},
					},
					View: argocduserv1alpha1.ArgocdCIView{
						CIPass: "view-pass",
						Users:  []string{"viewer1"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, newUser)).Should(Succeed())

			By("Waiting for AppProject to be created")
			appProj := &argov1alpha1.AppProject{}
			appProjLookup := types.NamespacedName{Name: "project-team", Namespace: "user-argocd"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, appProjLookup, appProj)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying AppProject has correct admin role")
			adminRoleFound := false
			for _, role := range appProj.Spec.Roles {
				if role.Name == "project-team-admin" {
					adminRoleFound = true
					Expect(role.Groups).To(ContainElement("project-team-admin"))
					Expect(role.Groups).To(ContainElement("project-team-admin-ci"))
					Expect(role.Groups).To(HaveLen(2), "Admin role should only have admin groups")

					// Verify admin policies
					Expect(role.Policies).To(ContainElement(ContainSubstring("applications, *, project-team/*, allow")))
					Expect(role.Policies).To(ContainElement(ContainSubstring("repositories, *, project-team/*, allow")))
					Expect(role.Policies).To(ContainElement(ContainSubstring("exec, create, project-team/*, allow")))
				}
			}
			Expect(adminRoleFound).To(BeTrue(), "Admin role should exist in AppProject")

			By("Verifying AppProject has correct view role with role aggregation")
			viewRoleFound := false
			for _, role := range appProj.Spec.Roles {
				if role.Name == "project-team-view" {
					viewRoleFound = true
					// View role should include ALL groups for role aggregation
					Expect(role.Groups).To(ContainElement("project-team-view"))
					Expect(role.Groups).To(ContainElement("project-team-view-ci"))
					Expect(role.Groups).To(ContainElement("project-team-admin"))
					Expect(role.Groups).To(ContainElement("project-team-admin-ci"))
					Expect(role.Groups).To(HaveLen(4), "View role should have all groups for role aggregation")

					// Verify view policies
					Expect(role.Policies).To(ContainElement(ContainSubstring("applications, get, project-team/*, allow")))
					Expect(role.Policies).To(ContainElement(ContainSubstring("repositories, get, project-team/*, allow")))
					Expect(role.Policies).To(ContainElement(ContainSubstring("logs, get, project-team/*, allow")))
				}
			}
			Expect(viewRoleFound).To(BeTrue(), "View role should exist in AppProject")
		})
	})

	Context("When managing OpenShift Groups", func() {
		It("Should add admin users to both admin and view groups", func() {
			By("Verifying admin group contains admin users")
			adminGroup := &userv1.Group{}
			adminGroupLookup := types.NamespacedName{Name: "project-team-admin"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, adminGroupLookup, adminGroup)
				return err == nil && len(adminGroup.Users) > 0
			}, timeout, interval).Should(BeTrue())

			Expect(adminGroup.Users).To(ContainElement("admin1"),
				"Admin group should contain admin user")

			By("Verifying view group contains admin users for role aggregation")
			viewGroup := &userv1.Group{}
			viewGroupLookup := types.NamespacedName{Name: "project-team-view"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, viewGroupLookup, viewGroup)
				return err == nil && len(viewGroup.Users) > 0
			}, timeout, interval).Should(BeTrue())

			Expect(viewGroup.Users).To(ContainElement("admin1"),
				"View group should contain admin user for role aggregation")
			Expect(viewGroup.Users).To(ContainElement("viewer1"),
				"View group should contain view user")
		})

		It("Should only add view users to view group", func() {
			By("Verifying admin group does NOT contain view users")
			adminGroup := &userv1.Group{}
			adminGroupLookup := types.NamespacedName{Name: "project-team-admin"}
			Expect(k8sClient.Get(ctx, adminGroupLookup, adminGroup)).Should(Succeed())

			Expect(adminGroup.Users).NotTo(ContainElement("viewer1"),
				"Admin group should NOT contain view-only users")

			By("Verifying view group contains view users")
			viewGroup := &userv1.Group{}
			viewGroupLookup := types.NamespacedName{Name: "project-team-view"}
			Expect(k8sClient.Get(ctx, viewGroupLookup, viewGroup)).Should(Succeed())

			Expect(viewGroup.Users).To(ContainElement("viewer1"),
				"View group should contain view user")
		})

		It("Should update groups when users are added", func() {
			By("Getting the existing ArgocdUser")
			argocdUser := &argocduserv1alpha1.ArgocdUser{}
			argocdUserLookup := types.NamespacedName{Name: "project-team"}
			Expect(k8sClient.Get(ctx, argocdUserLookup, argocdUser)).Should(Succeed())

			By("Adding new users to the spec")
			argocdUser.Spec.Admin.Users = []string{"admin1", "admin2"}
			argocdUser.Spec.View.Users = []string{"viewer1", "viewer2"}
			Expect(k8sClient.Update(ctx, argocdUser)).Should(Succeed())

			By("Waiting for admin group to be updated")
			adminGroup := &userv1.Group{}
			adminGroupLookup := types.NamespacedName{Name: "project-team-admin"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, adminGroupLookup, adminGroup)
				if err != nil {
					return false
				}
				return len(adminGroup.Users) >= 2 && containsUser(adminGroup.Users, "admin2")
			}, timeout, interval).Should(BeTrue())

			Expect(adminGroup.Users).To(ContainElement("admin1"))
			Expect(adminGroup.Users).To(ContainElement("admin2"))

			By("Verifying both new admin users are in view group")
			viewGroup := &userv1.Group{}
			viewGroupLookup := types.NamespacedName{Name: "project-team-view"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, viewGroupLookup, viewGroup)
				if err != nil {
					return false
				}
				return containsUser(viewGroup.Users, "admin2") && containsUser(viewGroup.Users, "viewer2")
			}, timeout, interval).Should(BeTrue())

			Expect(viewGroup.Users).To(ContainElement("admin1"))
			Expect(viewGroup.Users).To(ContainElement("admin2"))
			Expect(viewGroup.Users).To(ContainElement("viewer1"))
			Expect(viewGroup.Users).To(ContainElement("viewer2"))
		})
	})
})

// Helper function to check if a user is in the list
func containsUser(users []string, user string) bool {
	for _, u := range users {
		if u == user {
			return true
		}
	}
	return false
}
