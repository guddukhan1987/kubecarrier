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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	catalogv1alpha1 "github.com/kubermatic/kubecarrier/pkg/apis/catalog/v1alpha1"
	elevatorutil "github.com/kubermatic/kubecarrier/pkg/elevator/internal/util"
)

// TenantObjWebhookHandler handles TenantObjs validation.
type TenantObjWebhookHandler struct {
	Log     logr.Logger
	Scheme  *runtime.Scheme
	decoder *admission.Decoder

	// Client has a global cache, and is used to perform Create/Update request with dry-run flag to against the Catapult webhook.
	client.Client
	// NamespacedClient has a namespace-only cache, and is only allowed to access the provider namespace,
	// this is used to fetch the DerivedCustomResource object.
	NamespacedClient client.Client

	TenantGVK, ProviderGVK schema.GroupVersionKind

	DerivedCRName, ProviderNamespace string
}

var _ admission.Handler = (*TenantObjWebhookHandler)(nil)

// Handle is the function to handle TenantObjs validating requests.
func (r *TenantObjWebhookHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	obj := &unstructured.Unstructured{}
	if err := r.decoder.Decode(req, obj); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// If the obj is being deleted, just skip the webhook.
	if !obj.GetDeletionTimestamp().IsZero() {
		return admission.Allowed("Allow to delete object")
	}

	// Check if the GVK from request is as same as the GVK from configuration
	objGVK := obj.GroupVersionKind()
	if !reflect.DeepEqual(objGVK, r.TenantGVK) {
		return admission.Errored(http.StatusBadRequest,
			fmt.Errorf("the GVK (group, version and kind) is wrong with the requestd object, expected: %s, got: %s", r.TenantGVK, objGVK))
	}

	// Get DerivedCustomResource field configs
	derivedCustomResource := &catalogv1alpha1.DerivedCustomResource{}
	if err := r.NamespacedClient.Get(ctx, types.NamespacedName{
		Name:      r.DerivedCRName,
		Namespace: r.ProviderNamespace,
	}, derivedCustomResource); err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("getting the DerivedCustomResource: %w", err))
	}

	// Check if the DerivedCustomResource object is ready
	if !derivedCustomResource.IsReady() {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("DerivedCustomResource object is not ready"))
	}

	// Get the exposed config and version
	version := r.ProviderGVK.Version
	exposeConfig, ok := elevatorutil.VersionExposeConfigForVersion(derivedCustomResource.Spec.Expose, version)
	if !ok {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("DerivedCustomResource object is missing version expose config for version %q", version))
	}
	// prepare config
	_, nonStatusExposedFields := elevatorutil.SplitStatusFields(exposeConfig.Fields)

	tenantObj := obj.DeepCopy()
	providerObj := &unstructured.Unstructured{}
	providerObj.SetGroupVersionKind(r.ProviderGVK)
	defaults, err := elevatorutil.FormDefaults(exposeConfig.Default)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("forming defaults: %w", err))
	}
	err = r.Get(ctx, types.NamespacedName{
		Name:      tenantObj.GetName(),
		Namespace: tenantObj.GetNamespace(),
	}, providerObj)
	if err != nil && !errors.IsNotFound(err) {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("getting providerObj: %w", err))
	}
	if err := elevatorutil.BuildProviderObj(tenantObj, providerObj, r.Scheme, nonStatusExposedFields, defaults); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("build and elevate: %w", err))
	}
	// client.ForceOwnership is here until the
	// https://github.com/kubernetes/kubernetes/issues/88901 is backported
	// we use server-side-apply in both UPDATE and CREATE path since it would create an base CRD instance if it
	// doesn't exist
	if err := r.Patch(ctx, providerObj, client.Apply, client.DryRunAll, client.ForceOwnership, elevatorutil.FieldOwner); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("apply-patch %w", err))
	}

	newObj := obj.DeepCopy()
	if err := elevatorutil.CopyFields(providerObj, newObj, nonStatusExposedFields); err != nil {
		return admission.Errored(http.StatusInternalServerError,
			fmt.Errorf("changing %s .fields back: %w", r.ProviderGVK.Kind, err))
	}

	marshalledObj, err := json.Marshal(newObj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	// Create the defaults
	return admission.PatchResponseFromRaw(req.Object.Raw, marshalledObj)
}

// TenantObjWebhookHandler implements admission.DecoderInjector.
// A decoder will be automatically injected.
// InjectDecoder injects the decoder.
func (r *TenantObjWebhookHandler) InjectDecoder(d *admission.Decoder) error {
	r.decoder = d
	return nil
}