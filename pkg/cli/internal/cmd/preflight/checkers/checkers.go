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

package checkers

import (
	"bytes"
	"fmt"
	"time"

	"github.com/gernest/wow"
	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"

	"github.com/kubermatic/kubecarrier/pkg/cli/internal/spinner"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
}

const (
	firstSupportedKubernetesVersion = "v1.16.0"

	certManagerCertificatesCRDName = "certificates.cert-manager.io"
	certManagerIssuersCRDName      = "issuers.cert-manager.io"
)

// checker checks if the state of the system meets KubeCarrier installation requirements.
type checker interface {
	check() error
	name() string
}

func RunChecks(c *rest.Config, s *wow.Wow, startTime time.Time, log logr.Logger) error {
	var errBuffer bytes.Buffer
	checkers := []checker{
		&kubernetesVersionChecker{
			config:                c,
			firstSupportedVersion: firstSupportedKubernetesVersion,
		},
		&crdEstablishedChecker{
			config:   c,
			log:      log,
			crdNames: []string{certManagerCertificatesCRDName, certManagerIssuersCRDName},
		},
	}
	for _, checker := range checkers {
		if err := spinner.AttachSpinnerTo(s, startTime, fmt.Sprintf("[preflight check] %s", checker.name()), func() error {
			return checker.check()
		}); err != nil {
			errBuffer.WriteString(fmt.Errorf("[preflight check] %s: %w\n", checker.name(), err).Error())
		}
	}
	if errBuffer.Len() > 0 {
		return fmt.Errorf(errBuffer.String())
	}
	return nil
}
