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
	"time"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"
	"github.com/snapp-incubator/argocd-complementary-operator/internal/controller"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var ctx = context.Background()

var _ = Describe("namespace controller to create teams", Ordered, func() {
	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		timeout  = time.Second * 20
		interval = time.Millisecond * 30
	)

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
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: "user-argocd"}, userArgocdNS)
		}

		By("Ensuring required ConfigMaps exist for ArgocdUser reconciliation")
		// Create argocd-rbac-cm
		rbacConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argocd-rbac-cm",
				Namespace: "user-argocd",
			},
			Data: map[string]string{
				"policy.csv": "",
			},
		}
		err = k8sClient.Create(ctx, rbacConfigMap)
		if err != nil {
			// Might already exist from other tests
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: "argocd-rbac-cm", Namespace: "user-argocd"}, rbacConfigMap)
		}

		// Create argocd-cm
		argocdConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argocd-cm",
				Namespace: "user-argocd",
			},
			Data: map[string]string{},
		}
		err = k8sClient.Create(ctx, argocdConfigMap)
		if err != nil {
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: "argocd-cm", Namespace: "user-argocd"}, argocdConfigMap)
		}

		// Create argocd-secret
		argocdSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argocd-secret",
				Namespace: "user-argocd",
			},
			Data: map[string][]byte{},
		}
		err = k8sClient.Create(ctx, argocdSecret)
		if err != nil {
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: "argocd-secret", Namespace: "user-argocd"}, argocdSecret)
		}

		By("Creating test-team ArgocdUser for namespace tests")
		testTeamUser := &argocduserv1alpha1.ArgocdUser{
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
		err = k8sClient.Create(ctx, testTeamUser)
		if err != nil {
			// Might already exist from ArgocdUser tests
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: "test-team"}, testTeamUser)
		}

		// Wait for test-team AppProject to be created
		testAppProj := &argov1alpha1.AppProject{}
		testAppProjLookup := types.NamespacedName{Name: "test-team", Namespace: "user-argocd"}
		Eventually(func() bool {
			err := k8sClient.Get(ctx, testAppProjLookup, testAppProj)
			return err == nil
		}, timeout, interval).Should(BeTrue())
	})

	// Creating user-argocd namespace
	Context("When cluster bootstrap", func() {
		It("Should create user-argocd NS", func() {
			By("Creating user-argocd NS", func() {
				ArgoNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "user-argocd",
					},
				}
				err := k8sClient.Create(ctx, ArgoNs)
				if err != nil {
					// Namespace might already exist from ArgocdUser tests
					lookupns := types.NamespacedName{Name: "user-argocd"}
					Expect(k8sClient.Get(ctx, lookupns, ArgoNs)).Should(Succeed())
				}

				// Verify namespace exists
				lookupns := types.NamespacedName{Name: "user-argocd"}
				Expect(k8sClient.Get(ctx, lookupns, ArgoNs)).Should(Succeed())
			})
		})
	})

	// Creating AppProj as soon as we create a test namespace
	Context("when creating namespace", func() {
		It("Should create appProject", func() {
			By("Creating test namespace")
			// create test namespace with test-team label.
			testNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						controller.ProjectsLabel: "test-team",
						controller.SourceLabel:   "test-team",
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNS)).Should(Succeed())

			// make sure test namespace is created.
			testNSLookup := types.NamespacedName{Name: "test-ns"}
			Expect(k8sClient.Get(ctx, testNSLookup, testNS)).Should(Succeed())

			testAppProj := &argov1alpha1.AppProject{}
			testAppProjLookup := types.NamespacedName{Name: "test-team", Namespace: "user-argocd"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, testAppProjLookup, testAppProj)
				if err != nil {
					return false
				}
				// Wait for Destinations to be populated by NamespaceReconciler
				return len(testAppProj.Spec.Destinations) > 0 && len(testAppProj.Spec.SourceNamespaces) > 0
			}, timeout, interval).Should(BeTrue())

			// make sure appproject has the correct fields.
			Expect(testAppProj.Name).Should(Equal(testAppProjLookup.Name))
			Expect(testAppProj.Namespace).Should(Equal(testAppProjLookup.Namespace))
			Expect(testAppProj.Spec.Destinations[0].Namespace).Should(Equal(testNS.Name))
			Expect(testAppProj.Spec.SourceNamespaces).Should(HaveLen(1))
			Expect(testAppProj.Spec.SourceNamespaces[0]).Should(Equal(testNS.Name))
		})
	})

	// Changing the namespace label and checking if the appProjects are updated
	Context("when changing namespace team label", func() {
		It("Should update appProject", func() {
			By("Creating ArgocdUser for cloudy-team first", func() {
				cloudyUser := &argocduserv1alpha1.ArgocdUser{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cloudy-team",
					},
					Spec: argocduserv1alpha1.ArgocdUserSpec{
						Admin: argocduserv1alpha1.ArgocdCIAdmin{
							CIPass: "admin-pass",
							Users:  []string{},
						},
						View: argocduserv1alpha1.ArgocdCIView{
							CIPass: "view-pass",
							Users:  []string{},
						},
					},
				}
				Expect(k8sClient.Create(ctx, cloudyUser)).Should(Succeed())

				// Wait for AppProject to be created by ArgocdUserReconciler
				cloudyAppProj := &argov1alpha1.AppProject{}
				cloudyAppProjLookup := types.NamespacedName{Name: "cloudy-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, cloudyAppProjLookup, cloudyAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())
			})

			By("Removing from AppProject and creating new AppProject", func() {
				// update test namespace with cloudy-team label.
				testNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-ns",
						Labels: map[string]string{controller.ProjectsLabel: "cloudy-team"},
					},
				}
				Expect(k8sClient.Update(ctx, testNS)).Should(Succeed())

				// make sure test namespace is updated.
				testNSLookup := types.NamespacedName{Name: "test-ns"}
				Expect(k8sClient.Get(ctx, testNSLookup, testNS)).Should(Succeed())

				// appproject should be created in user-argocd for cloudy-team because of having
				// namespace.
				cloudyAppProj := &argov1alpha1.AppProject{}
				cloudyAppProjLookup := types.NamespacedName{Name: "cloudy-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, cloudyAppProjLookup, cloudyAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())

				// Wait for NamespaceReconciler to populate Destinations
				Eventually(func() bool {
					err := k8sClient.Get(ctx, cloudyAppProjLookup, cloudyAppProj)
					if err != nil {
						return false
					}
					return len(cloudyAppProj.Spec.Destinations) > 0
				}, timeout, interval).Should(BeTrue())

				// make sure appproject has the correct fields.
				Expect(cloudyAppProj.Name).Should(Equal(cloudyAppProjLookup.Name))
				Expect(cloudyAppProj.Namespace).Should(Equal(cloudyAppProjLookup.Namespace))
				Expect(cloudyAppProj.Spec.Destinations[0].Namespace).Should(Equal(testNS.Name))

				testAppProj := &argov1alpha1.AppProject{}
				appProjLookupKey := types.NamespacedName{Name: "test-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, appProjLookupKey, testAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())
				Expect(testAppProj.Name).Should(Equal(appProjLookupKey.Name))
				Expect(testAppProj.Namespace).Should(Equal(appProjLookupKey.Namespace))
				// Eventually(testAppProj.Spec.Destinations).Should(BeEmpty())
			})
		})
	})

	// Changing the namespace label and checking if the appProjects are updated
	Context("when changing namespace team label with multiple teams", func() {
		It("Should update appProject with multiple labels", func() {
			By("Creating ArgocdUser for rainy-team first", func() {
				rainyUser := &argocduserv1alpha1.ArgocdUser{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rainy-team",
					},
					Spec: argocduserv1alpha1.ArgocdUserSpec{
						Admin: argocduserv1alpha1.ArgocdCIAdmin{
							CIPass: "admin-pass",
							Users:  []string{},
						},
						View: argocduserv1alpha1.ArgocdCIView{
							CIPass: "view-pass",
							Users:  []string{},
						},
					},
				}
				Expect(k8sClient.Create(ctx, rainyUser)).Should(Succeed())

				// Wait for AppProject to be created by ArgocdUserReconciler
				rainyAppProj := &argov1alpha1.AppProject{}
				rainyAppProjLookup := types.NamespacedName{Name: "rainy-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, rainyAppProjLookup, rainyAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())
			})

			By("Removing from AppProject and creating new AppProject", func() {
				// update test namespace with cloudy-team label.
				testNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-ns",
						Labels: map[string]string{controller.ProjectsLabel: "cloudy-team.rainy-team"},
					},
				}
				Expect(k8sClient.Update(ctx, testNS)).Should(Succeed())

				// make sure test namespace is updated.
				testNSLookup := types.NamespacedName{Name: "test-ns"}
				Expect(k8sClient.Get(ctx, testNSLookup, testNS)).Should(Succeed())

				// appproject should be created in user-argocd for cloudy-team because of having
				// namespace.
				cloudyAppProj := new(argov1alpha1.AppProject)
				cloudyAppProjLookup := types.NamespacedName{Name: "cloudy-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, cloudyAppProjLookup, cloudyAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())

				// Wait for NamespaceReconciler to populate Destinations
				Eventually(func() bool {
					err := k8sClient.Get(ctx, cloudyAppProjLookup, cloudyAppProj)
					if err != nil {
						return false
					}
					return len(cloudyAppProj.Spec.Destinations) > 0
				}, timeout, interval).Should(BeTrue())

				// make sure appproject has the correct fields.
				Expect(cloudyAppProj.Name).Should(Equal(cloudyAppProjLookup.Name))
				Expect(cloudyAppProj.Namespace).Should(Equal(cloudyAppProjLookup.Namespace))
				Expect(cloudyAppProj.Spec.Destinations[0].Namespace).Should(Equal(testNS.Name))

				// appproject should be created in user-argocd for rainy-team because of having
				// namespace.
				rainyAppProj := new(argov1alpha1.AppProject)
				rainyAppProjLookup := types.NamespacedName{Name: "rainy-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, rainyAppProjLookup, rainyAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())

				// Wait for NamespaceReconciler to populate Destinations
				Eventually(func() bool {
					err := k8sClient.Get(ctx, rainyAppProjLookup, rainyAppProj)
					if err != nil {
						return false
					}
					return len(rainyAppProj.Spec.Destinations) > 0
				}, timeout, interval).Should(BeTrue())

				// make sure appproject has the correct fields.
				Expect(rainyAppProj.Name).Should(Equal(rainyAppProjLookup.Name))
				Expect(rainyAppProj.Namespace).Should(Equal(rainyAppProjLookup.Namespace))
				Expect(rainyAppProj.Spec.Destinations[0].Namespace).Should(Equal(testNS.Name))
			})
		})
	})

	// Multi-team with missing ArgocdUser validation
	Context("when multi-team label includes non-existent ArgocdUser", func() {
		It("Should handle missing ArgocdUser gracefully in multi-team label", func() {
			By("Creating ArgocdUser for existing-team first", func() {
				existingUser := &argocduserv1alpha1.ArgocdUser{
					ObjectMeta: metav1.ObjectMeta{
						Name: "existing-team",
					},
					Spec: argocduserv1alpha1.ArgocdUserSpec{
						Admin: argocduserv1alpha1.ArgocdCIAdmin{
							CIPass: "admin-pass",
							Users:  []string{},
						},
						View: argocduserv1alpha1.ArgocdCIView{
							CIPass: "view-pass",
							Users:  []string{},
						},
					},
				}
				Expect(k8sClient.Create(ctx, existingUser)).Should(Succeed())

				// Wait for AppProject to be created by ArgocdUserReconciler
				existingAppProj := &argov1alpha1.AppProject{}
				existingAppProjLookup := types.NamespacedName{Name: "existing-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, existingAppProjLookup, existingAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())
			})

			By("Creating namespace with multi-team label including non-existent team")
			partialNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "partial-team-ns",
					Labels: map[string]string{
						controller.ProjectsLabel: "existing-team.nonexistent-team",
					},
				},
			}
			Expect(k8sClient.Create(ctx, partialNS)).Should(Succeed())

			// Verify namespace was created
			partialNSLookup := types.NamespacedName{Name: "partial-team-ns"}
			Expect(k8sClient.Get(ctx, partialNSLookup, partialNS)).Should(Succeed())

			By("Verifying existing-team AppProject gets the namespace in destinations")
			existingAppProj := &argov1alpha1.AppProject{}
			existingAppProjLookup := types.NamespacedName{Name: "existing-team", Namespace: "user-argocd"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, existingAppProjLookup, existingAppProj)
				if err != nil {
					return false
				}
				// Check if namespace is in destinations
				for _, dest := range existingAppProj.Spec.Destinations {
					if dest.Namespace == "partial-team-ns" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "existing-team AppProject should have partial-team-ns in destinations")

			By("Verifying nonexistent-team AppProject is NOT created")
			nonexistentAppProj := &argov1alpha1.AppProject{}
			nonexistentAppProjLookup := types.NamespacedName{Name: "nonexistent-team", Namespace: "user-argocd"}

			// Wait a bit to ensure reconciliation has been attempted
			time.Sleep(2 * time.Second)

			// AppProject should not exist for non-existent team
			err := k8sClient.Get(ctx, nonexistentAppProjLookup, nonexistentAppProj)
			Expect(err).Should(HaveOccurred(), "nonexistent-team AppProject should not exist")
			Expect(errors.IsNotFound(err)).Should(BeTrue(),
				"AppProject for non-existent ArgocdUser should not be created - only ArgocdUserReconciler creates AppProjects")
		})
	})

	// Verifying RBAC policies in AppProject
	Context("When verifying AppProject RBAC policies", func() {
		It("Should include logs permission for admin role", func() {
			By("Creating ArgocdUser for rbac-team first", func() {
				rbacUser := &argocduserv1alpha1.ArgocdUser{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rbac-team",
					},
					Spec: argocduserv1alpha1.ArgocdUserSpec{
						Admin: argocduserv1alpha1.ArgocdCIAdmin{
							CIPass: "admin-pass",
							Users:  []string{},
						},
						View: argocduserv1alpha1.ArgocdCIView{
							CIPass: "view-pass",
							Users:  []string{},
						},
					},
				}
				Expect(k8sClient.Create(ctx, rbacUser)).Should(Succeed())

				// Wait for AppProject to be created by ArgocdUserReconciler
				rbacAppProj := &argov1alpha1.AppProject{}
				rbacAppProjLookup := types.NamespacedName{Name: "rbac-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, rbacAppProjLookup, rbacAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())
			})

			By("Creating a namespace with team label")
			rbacTestNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rbac-test-ns",
					Labels: map[string]string{
						controller.ProjectsLabel: "rbac-team",
					},
				},
			}
			Expect(k8sClient.Create(ctx, rbacTestNS)).Should(Succeed())

			By("Waiting for AppProject to be created")
			rbacAppProj := &argov1alpha1.AppProject{}
			rbacAppProjLookup := types.NamespacedName{Name: "rbac-team", Namespace: "user-argocd"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, rbacAppProjLookup, rbacAppProj)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying admin role has full permissions (logs inherited from view role)")
			adminRoleFound := false
			for _, role := range rbacAppProj.Spec.Roles {
				if role.Name == "rbac-team-admin" {
					adminRoleFound = true

					// Verify admin-specific permissions
					Expect(role.Policies).To(ContainElement(ContainSubstring("applications, *, rbac-team/*, allow")),
						"Admin should have applications permissions")
					Expect(role.Policies).To(ContainElement(ContainSubstring("repositories, *, rbac-team/*, allow")),
						"Admin should have repositories permissions")
					Expect(role.Policies).To(ContainElement(ContainSubstring("exec, create, rbac-team/*, allow")),
						"Admin should have exec permissions")

					// Note: logs permission is in view role, which admin inherits
				}
			}
			Expect(adminRoleFound).To(BeTrue(), "Admin role should exist in AppProject")
		})

		It("Should include admin groups in view role for role aggregation", func() {
			By("Getting the AppProject created in previous test")
			rbacAppProj := &argov1alpha1.AppProject{}
			rbacAppProjLookup := types.NamespacedName{Name: "rbac-team", Namespace: "user-argocd"}
			Expect(k8sClient.Get(ctx, rbacAppProjLookup, rbacAppProj)).Should(Succeed())

			By("Verifying view role group assignments include both admin and view groups")
			viewRoleFound := false
			for _, role := range rbacAppProj.Spec.Roles {
				if role.Name == "rbac-team-view" {
					viewRoleFound = true

					// View role should contain ALL groups (admin + view) for role aggregation
					Expect(role.Groups).To(ContainElement("rbac-team-view"),
						"View role should include view group")
					Expect(role.Groups).To(ContainElement("rbac-team-view-ci"),
						"View role should include view-ci group")
					Expect(role.Groups).To(ContainElement("rbac-team-admin"),
						"View role should include admin group for role aggregation")
					Expect(role.Groups).To(ContainElement("rbac-team-admin-ci"),
						"View role should include admin-ci group for role aggregation")

					// Verify view role has correct permissions
					Expect(role.Policies).To(ContainElement(ContainSubstring("applications, get, rbac-team/*, allow")),
						"View should have applications get permission")
					Expect(role.Policies).To(ContainElement(ContainSubstring("repositories, get, rbac-team/*, allow")),
						"View should have repositories get permission")
					Expect(role.Policies).To(ContainElement(ContainSubstring("logs, get, rbac-team/*, allow")),
						"View should have logs get permission")
				}
			}
			Expect(viewRoleFound).To(BeTrue(), "View role should exist in AppProject")
		})

		It("Should have complete admin role structure with all permissions", func() {
			By("Creating ArgocdUser for complete-team first", func() {
				completeUser := &argocduserv1alpha1.ArgocdUser{
					ObjectMeta: metav1.ObjectMeta{
						Name: "complete-team",
					},
					Spec: argocduserv1alpha1.ArgocdUserSpec{
						Admin: argocduserv1alpha1.ArgocdCIAdmin{
							CIPass: "admin-pass",
							Users:  []string{},
						},
						View: argocduserv1alpha1.ArgocdCIView{
							CIPass: "view-pass",
							Users:  []string{},
						},
					},
				}
				Expect(k8sClient.Create(ctx, completeUser)).Should(Succeed())

				// Wait for AppProject to be created by ArgocdUserReconciler
				completeAppProj := &argov1alpha1.AppProject{}
				completeAppProjLookup := types.NamespacedName{Name: "complete-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, completeAppProjLookup, completeAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())
			})

			By("Creating another test namespace")
			fullTestNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "full-rbac-test",
					Labels: map[string]string{
						controller.ProjectsLabel: "complete-team",
					},
				},
			}
			Expect(k8sClient.Create(ctx, fullTestNS)).Should(Succeed())

			By("Waiting for AppProject to be created")
			completeAppProj := &argov1alpha1.AppProject{}
			completeAppProjLookup := types.NamespacedName{Name: "complete-team", Namespace: "user-argocd"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, completeAppProjLookup, completeAppProj)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying admin role has all expected permissions")
			adminRoleFound := false
			for _, role := range completeAppProj.Spec.Roles {
				if role.Name == "complete-team-admin" {
					adminRoleFound = true

					// Admin role should have these specific permissions
					expectedPolicySubstrings := []string{
						"applications, *, complete-team/*, allow",
						"repositories, *, complete-team/*, allow",
						"exec, create, complete-team/*, allow",
					}

					for _, expectedSubstring := range expectedPolicySubstrings {
						Expect(role.Policies).To(ContainElement(ContainSubstring(expectedSubstring)),
							"Admin role should have policy containing: %s", expectedSubstring)
					}

					// Verify group assignments (only admin groups in admin role)
					Expect(role.Groups).To(HaveLen(2),
						"Admin role should have exactly 2 groups (admin and admin-ci)")
					Expect(role.Groups).To(ContainElement("complete-team-admin"))
					Expect(role.Groups).To(ContainElement("complete-team-admin-ci"))
				}
			}
			Expect(adminRoleFound).To(BeTrue(), "Admin role should exist in AppProject")
		})
	})

	// Verifying that namespace controller doesn't create AppProject
	Context("When namespace is labeled with non-existent team", func() {
		It("Should fail because AppProject must be created by ArgocdUser first", func() {
			By("Creating a namespace with label for non-existent team")
			failNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "fail-test-ns",
					Labels: map[string]string{
						controller.ProjectsLabel: "non-existent-team",
					},
				},
			}
			Expect(k8sClient.Create(ctx, failNS)).Should(Succeed())

			By("Verifying namespace was created")
			failNSLookup := types.NamespacedName{Name: "fail-test-ns"}
			Expect(k8sClient.Get(ctx, failNSLookup, failNS)).Should(Succeed())

			By("Verifying AppProject is NOT created by NamespaceReconciler")
			failAppProj := &argov1alpha1.AppProject{}
			failAppProjLookup := types.NamespacedName{Name: "non-existent-team", Namespace: "user-argocd"}

			// Wait a bit to ensure reconciliation has been attempted
			time.Sleep(2 * time.Second)

			// AppProject should not exist
			err := k8sClient.Get(ctx, failAppProjLookup, failAppProj)
			Expect(err).Should(HaveOccurred())
			Expect(errors.IsNotFound(err)).Should(BeTrue(),
				"AppProject should not be created by NamespaceReconciler - only ArgocdUserReconciler can create AppProjects")
		})
	})
})
