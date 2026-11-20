package controller_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	argocduserv1alpha1 "github.com/snapp-incubator/argocd-complementary-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getAppProjectUntyped reads an AppProject without the Argo CD v2 types, which
// have no `name` on a cluster resource rule and would hide it.
func getAppProjectUntyped(g Gomega, name string) *unstructured.Unstructured {
	proj := &unstructured.Unstructured{}
	proj.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "AppProject"})
	g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: argocdAppsNs}, proj)).To(Succeed())
	return proj
}

func clusterRules(g Gomega, proj *unstructured.Unstructured, field string) ([]interface{}, bool) {
	rules, found, err := unstructured.NestedSlice(proj.Object, "spec", field)
	g.Expect(err).NotTo(HaveOccurred())
	return rules, found
}

var crdRule = map[string]interface{}{
	"group": "apiextensions.k8s.io",
	"kind":  "CustomResourceDefinition",
	"name":  "*.example.io",
}

var _ = Describe("AppProject cluster resource rules", func() {
	Context("When an ArgocdUser sets clusterResourceWhitelist", func() {
		const name = "crd-au"

		It("Should whitelist exactly those resources and drop the default blacklist", func() {
			ensureArgocdUserExists(ctx, name, argocduserv1alpha1.ArgocdUserSpec{
				Admin: argocduserv1alpha1.ArgocdCIAdmin{CIPass: "crd-admin-pass", Users: []string{"crd-admin"}},
				ClusterResourceWhitelist: []argocduserv1alpha1.ClusterResource{{
					Group: "apiextensions.k8s.io",
					Kind:  "CustomResourceDefinition",
					Name:  "*.example.io",
				}},
			})

			Eventually(func(g Gomega) {
				proj := getAppProjectUntyped(g, name)
				whitelist, _ := clusterRules(g, proj, "clusterResourceWhitelist")
				g.Expect(whitelist).To(Equal([]interface{}{crdRule}))
				_, hasBlacklist := clusterRules(g, proj, "clusterResourceBlacklist")
				g.Expect(hasBlacklist).To(BeFalse())
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("When an ArgocdUser does not set clusterResourceWhitelist", Ordered, func() {
		const name = "legacy-au"

		BeforeAll(func() {
			ensureArgocdUserExists(ctx, name, argocduserv1alpha1.ArgocdUserSpec{
				Admin: argocduserv1alpha1.ArgocdCIAdmin{CIPass: "legacy-admin-pass", Users: []string{"legacy-admin"}},
			})
		})

		It("Should create the AppProject with the default */* blacklist", func() {
			Eventually(func(g Gomega) {
				proj := getAppProjectUntyped(g, name)
				blacklist, _ := clusterRules(g, proj, "clusterResourceBlacklist")
				g.Expect(blacklist).To(Equal([]interface{}{map[string]interface{}{"group": "*", "kind": "*"}}))
			}, timeout, interval).Should(Succeed())
		})

		It("Should keep a hand-set rule name while it repairs other fields", func() {
			By("Setting a named whitelist by hand and breaking the roles")
			proj := &unstructured.Unstructured{}
			proj.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "AppProject"})
			proj.SetName(name)
			proj.SetNamespace(argocdAppsNs)
			patch := []byte(`{"spec":{"clusterResourceBlacklist":null,` +
				`"clusterResourceWhitelist":[{"group":"apiextensions.k8s.io","kind":"CustomResourceDefinition","name":"*.example.io"}],` +
				`"roles":[{"name":"stale"}]}}`)
			Expect(k8sClient.Patch(ctx, proj, client.RawPatch(types.MergePatchType, patch))).To(Succeed())

			By("Triggering a reconcile through an ArgocdUser change")
			au := &argocduserv1alpha1.ArgocdUser{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, au)).To(Succeed())
			au.Spec.Admin.Users = append(au.Spec.Admin.Users, "legacy-admin-2")
			Expect(k8sClient.Update(ctx, au)).To(Succeed())

			By("Verifying the roles are repaired and the rule name survived")
			Eventually(func(g Gomega) {
				proj := getAppProjectUntyped(g, name)
				roles, _, err := unstructured.NestedSlice(proj.Object, "spec", "roles")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(roles).To(HaveLen(3))
				whitelist, _ := clusterRules(g, proj, "clusterResourceWhitelist")
				g.Expect(whitelist).To(Equal([]interface{}{crdRule}))
			}, timeout, interval).Should(Succeed())
		})
	})
})
