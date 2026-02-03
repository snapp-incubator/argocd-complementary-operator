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
	"path/filepath"
	"testing"
	"time"

	argov1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/snapp-incubator/argocd-complementary-operator/internal/controller"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// Uncomment if you are going to test the openshift group creation
	// userv1 "github.com/openshift/api/user/v1"

	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const (
	timeout      = time.Second * 30
	interval     = time.Millisecond * 100
	argocdAppsNs = "user-argocd"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	cancel    context.CancelFunc

	ctx = context.Background()
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	// reporterConfig.Verbose = true // Enable verbose output
	// reporterConfig.VeryVerbose = true    // Enable very verbose output
	// reporterConfig.FullTrace = true      // Show full stack traces
	// reporterConfig.ShowNodeEvents = true // Show node lifecycle events
	RunSpecs(t, "Controller Suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.DebugLevel))) // Add this for debug level logs

	ctx, cancel = context.WithCancel(ctx)

	By("Bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "config", "dependency"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = argocduserv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = argov1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// Uncomment if you are going to test the openshift group creation
	// err = userv1.AddToScheme(scheme.Scheme)
	// Expect(err).NotTo(HaveOccurred())

	err = rbacv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	// Register NamespaceReconciler
	err = (&controller.NamespaceReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	// Register ArgocdUserReconciler
	err = (&controller.ArgocdUserReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	// changing configuration according to the issue mentioned here:
	// https://github.com/kubernetes-sigs/controller-runtime/issues/1571
	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to start manager")
	}()

	// Create user-argocd namespace
	By("Creating user-argocd NS")
	argoNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: argocdAppsNs,
		},
	}
	Expect(k8sClient.Create(ctx, argoNs)).Should(Succeed())
	argoNsLookup := types.NamespacedName{Name: argocdAppsNs}
	Expect(k8sClient.Get(ctx, argoNsLookup, argoNs)).Should(Succeed())

	// Create argocd-cm ConfigMap
	By("Creating argocd-cm ConfigMap")
	argocdCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-cm",
			Namespace: argocdAppsNs,
		},
		Data: map[string]string{
			"initial.key": "initial-value",
		},
	}
	Expect(k8sClient.Create(ctx, argocdCM)).Should(Succeed())
	argocdCMLookup := types.NamespacedName{Name: "argocd-cm", Namespace: argocdAppsNs}
	Expect(k8sClient.Get(ctx, argocdCMLookup, argocdCM)).Should(Succeed())

	// Create argocd-secret Secret
	By("Creating argocd-secret Secret")
	argocdSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-secret",
			Namespace: argocdAppsNs,
		},
		Data: map[string][]byte{
			"initial.key": []byte("initial-value"),
		},
	}
	Expect(k8sClient.Create(ctx, argocdSecret)).Should(Succeed())
	argocdSecretLookup := types.NamespacedName{Name: "argocd-secret", Namespace: argocdAppsNs}
	Expect(k8sClient.Get(ctx, argocdSecretLookup, argocdSecret)).Should(Succeed())

	// Wait for cache to sync before running tests
	By("Waiting for manager cache to sync")
	Expect(k8sManager.GetCache().WaitForCacheSync(ctx)).Should(BeTrue(), "cache should sync")
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// ensureArgocdUserExists creates the ArgocdUser if it doesn't exist, or returns the existing one
func ensureArgocdUserExists(ctx context.Context, name string, spec argocduserv1alpha1.ArgocdUserSpec) *argocduserv1alpha1.ArgocdUser {
	au := &argocduserv1alpha1.ArgocdUser{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, au)

	if err == nil {
		// Already exists, return it
		return au
	}

	if !errors.IsNotFound(err) {
		// Unexpected error
		Expect(err).NotTo(HaveOccurred())
	}

	// Doesn't exist, create it
	au = &argocduserv1alpha1.ArgocdUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: spec,
	}
	Expect(k8sClient.Create(ctx, au)).Should(Succeed())
	return au
}
