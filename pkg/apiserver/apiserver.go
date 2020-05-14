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

package apiserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	catalogv1alpha1 "github.com/kubermatic/kubecarrier/pkg/apis/catalog/v1alpha1"
	apiserverv1 "github.com/kubermatic/kubecarrier/pkg/apiserver/api/v1"
	v1 "github.com/kubermatic/kubecarrier/pkg/apiserver/internal/v1"
	"github.com/kubermatic/kubecarrier/pkg/internal/util"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(catalogv1alpha1.AddToScheme(scheme))
}

type flags struct {
	address           string
	TLSCertFile       string
	TLSPrivateKeyFile string
}

func NewAPIServer() *cobra.Command {
	log := ctrl.Log.WithName("apiserver")
	flags := &flags{}
	cmd := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "apiserver",
		Short: "KubeCarrier API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runE(flags, log)
		},
	}
	cmd.Flags().StringVar(&flags.address, "address", "0.0.0.0:8080", "Address to bind this API server on.")
	cmd.Flags().StringVar(&flags.TLSCertFile, "tls-cert-file", "", "File containing the default x509 Certificate for HTTPS. If not provided no TLS security shall be enabled")
	cmd.Flags().StringVar(&flags.TLSPrivateKeyFile, "tls-private-key-file", "", "File containing the default x509 private key matching --tls-cert-file.")
	return util.CmdLogMixin(cmd)
}

func runE(flags *flags, log logr.Logger) error {
	// Validation
	if flags.TLSCertFile == "" || flags.TLSPrivateKeyFile == "" {
		return fmt.Errorf("--tls-cert-file or --tls-private-key-file not specified, cannot start")
	}

	// Startup
	grpcServer := grpc.NewServer()
	grpcGatewayMux := gwruntime.NewServeMux(
		gwruntime.WithProtoErrorHandler(func(ctx context.Context, serveMux *gwruntime.ServeMux, marshaler gwruntime.Marshaler, writer http.ResponseWriter, request *http.Request, err error) {
			const fallback = `{"error": "failed to marshal error message"}`
			writer.Header().Del("Trailer")
			writer.Header().Set("Content-Type", marshaler.ContentType())
			s, ok := status.FromError(err)
			if !ok {
				s = status.New(codes.Unknown, err.Error())
			}
			buf, marshalerr := marshaler.Marshal(s.Proto())
			if marshalerr != nil {
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(writer, fallback)
				return
			}

			st := gwruntime.HTTPStatusFromCode(s.Code())
			writer.WriteHeader(st)
			_, _ = writer.Write(buf)
		}),
	)

	// Create Kubernetes Client
	cfg := config.GetConfigOrDie()
	mapper, err := apiutil.NewDiscoveryRESTMapper(cfg)
	if err != nil {
		return fmt.Errorf("creating rest mapper: %w", err)
	}
	c, err := client.New(cfg, client.Options{
		Scheme: scheme,
		Mapper: mapper,
	})
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating dynamic client: %w", err)
	}

	ctx := context.Background()
	certPool := x509.NewCertPool()
	certs, err := ioutil.ReadFile(flags.TLSCertFile)
	if err != nil {
		return err
	}
	certPool.AppendCertsFromPEM(certs)
	grpcTLSConfig := &tls.Config{
		InsecureSkipVerify: true,
		RootCAs:            certPool,
	}

	grpcClient, err := grpc.DialContext(ctx, flags.address, grpc.WithTransportCredentials(credentials.NewTLS(grpcTLSConfig)))
	if err != nil {
		return err
	}

	apiserverv1.RegisterKubeCarrierServer(grpcServer, &v1.KubeCarrierServer{})
	if err := apiserverv1.RegisterKubeCarrierHandlerServer(context.Background(), grpcGatewayMux, &v1.KubeCarrierServer{}); err != nil {
		return err
	}
	offeringServer, err := v1.NewOfferingServiceServer(c, dynamicClient, mapper, scheme)
	if err != nil {
		return err
	}
	apiserverv1.RegisterOfferingServiceServer(grpcServer, offeringServer)
	if err := apiserverv1.RegisterOfferingServiceHandler(ctx, grpcGatewayMux, grpcClient); err != nil {
		return err
	}

	var handlerFunc http.HandlerFunc = func(writer http.ResponseWriter, request *http.Request) {
		log.Info("got request for", "path", request.URL.Path)
		if strings.Contains(request.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(writer, request)
		} else {
			grpcGatewayMux.ServeHTTP(writer, request)
		}
	}

	server := http.Server{
		Handler: handlerFunc,
		Addr:    flags.address,
	}

	log.Info("serving serving API-server", "address", flags.address)
	return server.ListenAndServeTLS(flags.TLSCertFile, flags.TLSPrivateKeyFile)
}
