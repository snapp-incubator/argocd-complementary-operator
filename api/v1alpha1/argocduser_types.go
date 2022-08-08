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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ArgocdUserSpec defines the desired state of ArgocdUser
type ArgocdUserSpec struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Admin ArgocdCIAdmin `json:"admin,omitempty"`
	View  ArgocdCIView  `json:"view,omitempty"`
}
type ArgocdCIAdmin struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	CIPass string `json:"ciPass,omitempty"`
	//ArgocdToken string `json:"argocdToken,omitempty"`
	Users []string `json:"users,omitempty"`
}
type ArgocdCIView struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	CIPass string `json:"ciPass,omitempty"`
	//ArgocdToken string `json:"argocdToken,omitempty"`
	Users []string `json:"users,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// ArgocdUser is the Schema for the ArgocdUsers API
type ArgocdUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ArgocdUserSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// ArgocdUserList contains a list of ArgocdUser
type ArgocdUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ArgocdUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ArgocdUser{}, &ArgocdUserList{})
}
