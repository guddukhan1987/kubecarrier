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

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gernest/wow"
	"github.com/gernest/wow/spin"
	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kubermatic/kubecarrier/pkg/anchor/internal/spinner"
	"github.com/kubermatic/kubecarrier/pkg/internal/kustomize"
	"github.com/kubermatic/kubecarrier/pkg/internal/reconcile"
	"github.com/kubermatic/kubecarrier/pkg/internal/resources/e2e"
)

func newSetupE2EOperator(log logr.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup-e2e-operator",
		Short: "install e2e operator in the given cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeconfig := cmd.Flag("kubeconfig").Value.String()
			namespaceName := cmd.Flag("namespace").Value.String()
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespaceName,
				},
			}
			if kubeconfig != "" {
				if err := os.Setenv("KUBECONFIG", kubeconfig); err != nil {
					return fmt.Errorf("kubeconfig env setup: %w", err)
				}
			}
			ctx := context.Background()
			cfg, err := config.GetConfig()
			if err != nil {
				return fmt.Errorf("getting Kubernetes cluster config: %w", err)
			}
			scheme := runtime.NewScheme()

			if err := clientgoscheme.AddToScheme(scheme); err != nil {
				return fmt.Errorf("cannot add clientgo scheme: %w", err)
			}
			if err := apiextensionsv1beta1.AddToScheme(scheme); err != nil {
				return fmt.Errorf("cannot add apiextenstions v1beta1 scheme: %w", err)
			}

			c, err := client.New(cfg, client.Options{Scheme: scheme})
			if err != nil {
				return fmt.Errorf("creating Kubernetes client: %w", err)
			}
			s := wow.New(cmd.OutOrStdout(), spin.Get(spin.Dots), "spinner text")
			s.Start()
			defer s.Stop()

			if err := spinner.AttachSpinnerTo(s, "creating namespace", func() error {
				_, err := controllerutil.CreateOrUpdate(ctx, c, namespace, func() error {
					return nil
				})
				return err

			}); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "%v", err)
				os.Exit(1)
			}

			if err := spinner.AttachSpinnerTo(s, "deploy e2e-operator", func() error {
				objects, err := e2e.Manifests(
					kustomize.NewDefaultKustomize(),
					e2e.Config{
						Namespace: namespace.Name,
					})
				if err != nil {
					return fmt.Errorf("creating operator manifests: %w", err)
				}

				for _, object := range objects {
					if err := controllerutil.SetControllerReference(namespace, &object, scheme); err != nil {
						return fmt.Errorf("set controller reference: %w", err)
					}
					b, err := yaml.Marshal(object)
					if err != nil {
						// this should never ever happen
						panic(err)
					}
					log.V(9).Info("Creating object\n" + string(b))
					_, err = reconcile.Unstructured(ctx, log, c, &object)
					if err != nil {
						return fmt.Errorf("reconcile kind: %s, err: %w", object.GroupVersionKind().Kind, err)
					}
					log.V(6).Info("reconciled",
						"name", object.GetName(),
						"namespace", object.GetNamespace(),
						"kind", object.GroupVersionKind().Kind,
						"group", object.GroupVersionKind().Group,
						"version", object.GroupVersionKind().Version,
					)
				}
				return nil
			}); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "%v", err)
				os.Exit(1)
			}

			if err := spinner.AttachSpinnerTo(s, "waiting for available deployment", func() error {
				log.Info("querying deployment")
				return wait.Poll(time.Second, 10*time.Second, func() (done bool, err error) {
					log.Info("querying deployment")
					deployment := &appsv1.Deployment{}
					err = c.Get(ctx, types.NamespacedName{
						Name:      "kubecarrier-e2e-e2e",
						Namespace: namespace.Name,
					}, deployment)
					switch {
					case errors.IsNotFound(err):
						log.V(6).Info("deployment yet not found")
						return false, nil
					case err != nil:
						return false, err
					default:
						if deployment.Status.ObservedGeneration != deployment.Generation {
							log.V(6).Info("deployment not in the latest generation")
							return false, nil
						}
						for _, condition := range deployment.Status.Conditions {
							if condition.Type == appsv1.DeploymentAvailable &&
								condition.Status == corev1.ConditionTrue {
								return true, nil
							}
						}
						log.V(6).Info("deployment isn't available yet")
						return false, nil
					}
				})
			}); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "%v", err)
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().String("kubeconfig", os.Getenv("KUBECONFIG"), "cluster kubeconfig where to install")
	cmd.Flags().StringP("namespace", "n", "kubecarrier-e2e-operator", "namespace where to deploy operator")
	return cmd
}
