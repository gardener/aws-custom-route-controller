// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gardener/aws-custom-route-controller/pkg/controller"
	"github.com/gardener/aws-custom-route-controller/pkg/updater"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var Version string

func main() {
	logf.SetLogger(zap.New())

	var log = logf.Log.WithName("aws-custom-route-controller")
	log.Info("version", "version", Version)

	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{})
	if err != nil {
		log.Error(err, "could not create manager")
		os.Exit(1)
	}

	reconciler := controller.NewNodeReconciler()
	err = builder.
		ControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(reconciler)
	if err != nil {
		log.Error(err, "could not create controller")
		os.Exit(1)
	}

	updater := func(routes []updater.NodeRoute) {
		fmt.Printf("routes: %#v\n", routes)
	}
	ctx := signals.SetupSignalHandler()
	reconciler.StartUpdater(ctx, updater, 5*time.Second)
	if err := mgr.Start(ctx); err != nil {
		log.Error(err, "could not start manager")
		os.Exit(1)
	}
}
