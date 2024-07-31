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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var ctx = context.Background()

var _ = Describe("namespace controller", func() {
	// Define utility constants for object names and testing timeouts/durations and intervals.
	const (
		timeout = time.Second * 10
		// duration  = time.Second * 10
		interval  = time.Millisecond * 250
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
	// Creating AppPRoj as soon as we create a test namespace
	Context("when creating namespace", func() {
		It("Should create appProject", func() {
			By("Creating test namespace")
			testNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: map[string]string{teamLabel: "test-team"},
				},
			}
			Expect(k8sClient.Create(ctx, testNs)).Should(Succeed())
			lookupTestns := types.NamespacedName{Name: "test-ns"}
			Expect(k8sClient.Get(ctx, lookupTestns, testNs)).Should(Succeed())
			testAppProj := &argov1alpha1.AppProject{}
			appProjLookupKey := types.NamespacedName{Name: "test-team", Namespace: "user-argocd"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, appProjLookupKey, testAppProj)
				return err == nil
			}, timeout, interval).Should(BeTrue())
			Expect(testAppProj.Name).Should(Equal(appProjLookupKey.Name))
			Expect(testAppProj.Namespace).Should(Equal(appProjLookupKey.Namespace))
			Expect(testAppProj.Spec.Destinations[0].Namespace).Should(Equal(testNs.ObjectMeta.Name))
		})
	})
	// Changing the namespace label and checking if the appProjects are updated
	Context("when changing namespace team label", func() {
		It("Should update appProject", func() {
			By("Removing  from AppProject and creating new AppProject", func() {
				testNs := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-ns",
						Labels: map[string]string{teamLabel: "cloudy-team"},
					},
				}
				Expect(k8sClient.Update(ctx, testNs)).Should(Succeed())
				lookupTestns := types.NamespacedName{Name: "test-ns"}
				Expect(k8sClient.Get(ctx, lookupTestns, testNs)).Should(Succeed())
				newAppProj := &argov1alpha1.AppProject{}
				newAppProjLookup := types.NamespacedName{Name: "cloudy-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, newAppProjLookup, newAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())
				Expect(newAppProj.Name).Should(Equal(newAppProjLookup.Name))
				Expect(newAppProj.Namespace).Should(Equal(newAppProjLookup.Namespace))
				Expect(newAppProj.Spec.Destinations[0].Namespace).Should(Equal(testNs.ObjectMeta.Name))
				testAppProj := &argov1alpha1.AppProject{}
				appProjLookupKey := types.NamespacedName{Name: "test-team", Namespace: "user-argocd"}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, appProjLookupKey, testAppProj)
					return err == nil
				}, timeout, interval).Should(BeTrue())
				Expect(testAppProj.Name).Should(Equal(appProjLookupKey.Name))
				Expect(testAppProj.Namespace).Should(Equal(appProjLookupKey.Namespace))
				Expect(testAppProj.Spec.Destinations).To(BeNil())
			})
		})
	})
})
