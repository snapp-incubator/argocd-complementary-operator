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
	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"
	"github.com/snapp-incubator/argocd-complementary-operator/internal/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	// Namespace controller tests variables
	testNSName          = "test-ns"
	nsTestAuName        = "ns-test-au"
	nsTestAuAdminCIPass = "some_pass_admin"
	nsTestAuAdminUsers  = []string{"user-a1", "user-a2"}
	testAuViewCIPass    = "some_pass_view"
	testAuviewUsers     = []string{"user-v1", "user-v2"}
	nsCloudyAuName      = "ns-cloudy-au"
	nonExistAuName      = "nonexists-team"
)

var _ = Describe("Namespace controller", func() {
	logger := logf.FromContext(ctx)
	// Should NOT creating AppProj when we create a test namespace with approj label not exists
	Context("When creating namespace", func() {
		It("Should NOT create non-exists appProject", func() {
			By("Creating test namespace with non-exists appProject label")
			// create test namespace with test-team label.
			testNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNSName,
					Labels: map[string]string{
						controller.ProjectsLabel: nonExistAuName,
						controller.SourceLabel:   nonExistAuName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNS)).Should(Succeed())

			// make sure test namespace is created.
			testNSLookup := types.NamespacedName{Name: testNSName}
			Expect(k8sClient.Get(ctx, testNSLookup, testNS)).Should(Succeed())

			// make sure test argocduser is not created.
			testAppProj := &argov1alpha1.AppProject{}
			testAppProjLookup := types.NamespacedName{Name: nonExistAuName, Namespace: argocdAppsNs}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, testAppProjLookup, testAppProj)
				if err != nil {
					logger.Error(err, "AppProject not found yet")
					return false
				}
				return err == nil
			}, timeout, interval).Should(BeFalse())
			Expect(testAppProj.Name).Should(Equal(""))
		})
	})

	// Changing the namespace label and checking if the appProjects are updated
	Context("When changing namespace team label", func() {
		It("Should update related existing appProjects", func() {
			By("Verifying Argocduser named test-au exists or created")
			_ = ensureArgocdUserExists(ctx, nsTestAuName, argocduserv1alpha1.ArgocdUserSpec{
				Admin: argocduserv1alpha1.ArgocdCIAdmin{
					CIPass: nsTestAuAdminCIPass,
					Users:  nsTestAuAdminUsers,
				},
				View: argocduserv1alpha1.ArgocdCIView{
					CIPass: testAuViewCIPass,
					Users:  testAuviewUsers,
				},
			})
			By("Verifying AppProject named test-au exists")
			testAuAppProj := &argov1alpha1.AppProject{}
			testAuAppProjLookup := types.NamespacedName{Name: nsTestAuName, Namespace: argocdAppsNs}
			Eventually(func() error {
				return k8sClient.Get(ctx, testAuAppProjLookup, testAuAppProj)
			}, timeout, interval).Should(Succeed())
			Expect(len(testAuAppProj.Spec.Destinations)).Should(Equal(0))

			By("Adding test-ns namespace to test-au's destinations")
			// update test namespace with test-au label.
			testNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNSName,
					Labels: map[string]string{controller.ProjectsLabel: nsTestAuName},
				},
			}
			Expect(k8sClient.Update(ctx, testNS)).Should(Succeed())

			// make sure test namespace is updated.
			testNSLookup := types.NamespacedName{Name: testNSName}
			Expect(k8sClient.Get(ctx, testNSLookup, testNS)).Should(Succeed())
			Expect(testNS.ObjectMeta.Labels[controller.ProjectsLabel]).Should(Equal(nsTestAuName))

			// the test-au Appproject should be updated in user-argocd because of
			// test-ns having team label for it
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, testAuAppProjLookup, testAuAppProj)).To(Succeed())
				g.Expect(testAuAppProj.Spec.Destinations).To(HaveLen(1))
				g.Expect(testAuAppProj.Spec.Destinations[0].Namespace).To(Equal(testNS.Name))
			}, timeout, interval).Should(Succeed())

			// make sure appproject has the correct fields.
			Expect(testAuAppProj.Name).Should(Equal(testAuAppProjLookup.Name))
			Expect(testAuAppProj.Namespace).Should(Equal(testAuAppProjLookup.Namespace))

			By("Removeing the old namespace from old appproject and add it to the new one")
			// Create cloudy-au argocduser
			cloudyAu := &argocduserv1alpha1.ArgocdUser{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsCloudyAuName,
				},
			}
			Expect(k8sClient.Create(ctx, cloudyAu)).Should(Succeed())
			lookup := types.NamespacedName{Name: nsCloudyAuName}
			au := &argocduserv1alpha1.ArgocdUser{}
			Expect(k8sClient.Get(ctx, lookup, au)).Should(Succeed())
			Expect(au.Spec).Should(Equal(cloudyAu.Spec))

			// Verify appproject has been created
			cloudyAppProj := &argov1alpha1.AppProject{}
			cloudyAppProjLookup := types.NamespacedName{Name: nsCloudyAuName, Namespace: argocdAppsNs}
			Eventually(func() error {
				return k8sClient.Get(ctx, cloudyAppProjLookup, cloudyAppProj)
			}, timeout, interval).Should(Succeed())
			// Check if new cloudy app project has no destination
			Expect(len(cloudyAppProj.Spec.Destinations)).Should(Equal(0))

			// update test namespace with cloudy-au label.
			testNS = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNSName,
					Labels: map[string]string{controller.ProjectsLabel: nsCloudyAuName},
				},
			}
			Expect(k8sClient.Update(ctx, testNS)).Should(Succeed())

			// Make sure test namespace is updated.
			Expect(k8sClient.Get(ctx, testNSLookup, testNS)).Should(Succeed())
			Expect(testNS.ObjectMeta.Labels[controller.ProjectsLabel]).Should(Equal(nsCloudyAuName))

			// Make sure old test-au appproject has no destination.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, testAuAppProjLookup, testAuAppProj)).To(Succeed())
				g.Expect(testAuAppProj.Spec.Destinations).To(HaveLen(0))
			}, timeout, interval).Should(Succeed())

			// the cloudy-au Appproject should be updated in user-argocd because of
			// test-ns having team label for it now.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, cloudyAppProjLookup, cloudyAppProj)).To(Succeed())
				g.Expect(cloudyAppProj.Spec.Destinations).To(HaveLen(1))
				g.Expect(cloudyAppProj.Spec.Destinations[0].Namespace).To(Equal(testNS.Name))
			}, timeout, interval).Should(Succeed())

			// Changing the namespace label to multiple projects names
			By("Updating multiple appprojects")

			// update test namespace with cloudy-au and test-au label.
			testNS = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNSName,
					Labels: map[string]string{controller.ProjectsLabel: nsTestAuName + "." + nsCloudyAuName},
				},
			}
			Expect(k8sClient.Update(ctx, testNS)).Should(Succeed())

			// Make sure test namespace is updated.
			Expect(k8sClient.Get(ctx, testNSLookup, testNS)).Should(Succeed())

			// The cloudy-au Appproject should be updated because the test-ns
			// namespace has it's label.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, cloudyAppProjLookup, cloudyAppProj)).To(Succeed())
				g.Expect(cloudyAppProj.Spec.Destinations).To(HaveLen(1))
				g.Expect(cloudyAppProj.Spec.Destinations[0].Namespace).To(Equal(testNS.Name))
			}, timeout, interval).Should(Succeed())

			// The test-au Appproject should be updated because the test-ns
			// namespace has it's label.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, testAuAppProjLookup, testAuAppProj)).To(Succeed())
				g.Expect(testAuAppProj.Spec.Destinations).To(HaveLen(1))
				g.Expect(testAuAppProj.Spec.Destinations[0].Namespace).To(Equal(testNS.Name))
			}, timeout, interval).Should(Succeed())

		})
	})
})
