/*

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

// ConfigWatchSpec defines the desired state of ConfigWatch
type ConfigWatchSpec struct {
	// The namespace where the configmap and secrets to be watched
	Namespace string `json:"namespace,omitempty"`

	// config maps to watch
	ConfigMaps []string `json:"configMaps,omitempty"`

	// secrets to watch
	Secrets []string `json:"secrets,omitempty"`
}

// ConfigWatchStatus defines the observed state of ConfigWatch
type ConfigWatchStatus struct {
	// The description of the current status
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cw
// +k8s:openapi-gen=true

// ConfigWatch is the Schema for the configwatches API
type ConfigWatch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigWatchSpec   `json:"spec,omitempty"`
	Status ConfigWatchStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConfigWatchList contains a list of ConfigWatch
type ConfigWatchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigWatch `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigWatch{}, &ConfigWatchList{})
}
