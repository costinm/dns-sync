/*
Copyright 2017 The Kubernetes Authors.

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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	//awsSDK "github.com/aws/aws-sdk-go/aws"
	//"github.com/aws/aws-sdk-go/service/dynamodb"
	//"github.com/aws/aws-sdk-go/service/route53"
	//sd "github.com/aws/aws-sdk-go/service/servicediscovery"

	"log"

	dns_sync "github.com/costinm/dns-sync/pkg/dns-sync"
	"github.com/costinm/dns-sync/pkg/sources/dnsmesh"
	"github.com/costinm/dns-sync/pkg/tel"
	"github.com/costinm/dns-sync/source"
	"github.com/go-logr/logr"
	"k8s.io/klog/v2"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	// K8S, Otel are using logr - which provides structured logging similar to slog.
	// Use slog handler - either logr or slog will go to the same place.
	logr.FromSlogHandler(slog.Default().Handler())


	ctx, cancel := context.WithCancel(context.Background())

	// Klog V2 is used by k8s.io/apimachinery/pkg/labels and can throw (a lot) of irrelevant logs
	// See https://github.com/kubernetes-sigs/external-dns/issues/2348
	defer klog.ClearLogger()
	//klog.SetLogger(logr.Discard())

	source.SourceFn["mesh-service"] = dnsmesh.NewMeshServiceSource

	if os.Getenv("DNS_SYNC_ONCE") != "" {
		err := dns_sync.LoadAndRun(ctx, true)
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	fmt.Println("Starting dns-sync")
	// No need to register metrics or signal handling if we're running in once mode.
	// TODO: switch to OTel, generate traces too
	go tel.ServeMetrics()
	fmt.Println("Starting dns-sync1")

	err := dns_sync.LoadAndRun(ctx,  false)
	fmt.Println("Starting dns-sync2")

	if err != nil {
		log.Fatal(err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals
	slog.Info("Received SIGTERM. Terminating...")
	cancel()
}

