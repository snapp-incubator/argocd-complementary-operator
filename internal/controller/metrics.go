package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// undeclaredGroupMembers tracks members that live in an OpenShift Group managed
// by an ArgocdUser but are not declared on its spec — i.e. people who hold the
// project's ArgoCD role (and, where the same Group backs a namespace
// RoleBinding, cluster access) without git saying so.
//
// A non-zero value means the declaration and reality have diverged. In
// authoritative mode the extra members are pruned and the gauge falls back to
// zero; otherwise it keeps reporting the drift so it can be reconciled in git
// before authoritative mode is switched on.
var undeclaredGroupMembers = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "argocduser_group_undeclared_members",
		Help: "Members of an ArgocdUser-managed OpenShift Group that are not declared on the ArgocdUser spec.",
	},
	[]string{"argocduser", "role", "group"},
)

func init() {
	metrics.Registry.MustRegister(undeclaredGroupMembers)
}
