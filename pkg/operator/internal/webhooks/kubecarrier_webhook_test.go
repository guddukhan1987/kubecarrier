/*
Copyright 2019 The KubeCarrier Authors.

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

package webhooks

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/kubermatic/kubecarrier/pkg/apis/operator/v1alpha1"
	"github.com/kubermatic/kubecarrier/pkg/testutil"
)

func TestKubeCarrierValidatingCreate(t *testing.T) {
	kubeCarrierWebhookHandler := KubeCarrierWebhookHandler{
		Log: testutil.NewLogger(t),
	}

	tests := []struct {
		name          string
		object        *operatorv1alpha1.KubeCarrier
		expectedError error
	}{
		{
			name: "invalid KubeCarrier name",
			object: &operatorv1alpha1.KubeCarrier{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-kubecarrier",
				},
			},
			expectedError: fmt.Errorf("KubeCarrier object name should be 'kubecarrier', found: test-kubecarrier"),
		},
		{
			name: "can pass validate create",
			object: &operatorv1alpha1.KubeCarrier{
				ObjectMeta: metav1.ObjectMeta{
					Name: "kubecarrier",
				},
			},
			expectedError: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expectedError, kubeCarrierWebhookHandler.validateCreate(test.object))
		})
	}
}
