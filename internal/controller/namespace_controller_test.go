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
	"time"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var ctx = context.Background()

var _ = Describe("namespace controller", func() {
	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		timeout   = time.Second * 20
		interval  = time.Millisecond * 30
		teamLabel = "argocd.snappcloud.io/appproj"
	)
	// Creating user-argocd namespace
	Context("When cluster bootstrap", func() {
		It("Should create user-argocd NS", func() {
			By("Creating user-argocd NS", func() {
				ArgoNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "user-argocd",
					},
				}
				Expect(k8sClient.Create(ctx, ArgoNs)).Should(Succeed())

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
					Name:   "test-ns",
					Labels: map[string]string{teamLabel: "test-team"},
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
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// make sure appproject has the correct fields.
			Expect(testAppProj.Name).Should(Equal(testAppProjLookup.Name))
			Expect(testAppProj.Namespace).Should(Equal(testAppProjLookup.Namespace))
			Expect(testAppProj.Spec.Destinations[0].Namespace).Should(Equal(testNS.Name))
		})
	})

	// Changing the namespace label and checking if the appProjects are updated
	Context("when changing namespace team label", func() {
		It("Should update appProject", func() {
			By("Removing from AppProject and creating new AppProject", func() {
				// update test namespace with cloudy-team label.
				testNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-ns",
						Labels: map[string]string{teamLabel: "cloudy-team"},
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
			By("Removing from AppProject and creating new AppProject", func() {
				// update test namespace with cloudy-team label.
				testNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-ns",
						Labels: map[string]string{teamLabel: "cloudy-team.rainy-team"},
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

				// make sure appproject has the correct fields.
				Expect(rainyAppProj.Name).Should(Equal(rainyAppProjLookup.Name))
				Expect(rainyAppProj.Namespace).Should(Equal(rainyAppProjLookup.Namespace))
				Expect(rainyAppProj.Spec.Destinations[0].Namespace).Should(Equal(testNS.Name))
			})
		})
	})
})
