/*
Copyright 2020 The Flux authors

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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/pkg/apis/meta"
)

// statusable is used to see if a resource is considered ready in the usual way
type statusable interface {
	adapter
	// this is implemented by ObjectMeta
	GetGeneration() int64
	getObservedGeneration() int64
	// this is usually implemented by GOTK API objects because it's used by pkg/apis/meta
	GetStatusConditions() *[]metav1.Condition
	// successMessage gives a short summary of the successful reconciliation
	successMessage() string
}

func isReady(ctx context.Context, kubeClient client.Client,
	namespacedName types.NamespacedName, object statusable) wait.ConditionFunc {
	return func() (bool, error) {
		err := kubeClient.Get(ctx, namespacedName, object.asRuntimeObject())
		if err != nil {
			return false, err
		}

		// Confirm the state we are observing is for the current generation
		if object.GetGeneration() != object.getObservedGeneration() {
			return false, nil
		}

		if c := apimeta.FindStatusCondition(*object.GetStatusConditions(), meta.ReadyCondition); c != nil {
			switch c.Status {
			case metav1.ConditionTrue:
				return true, nil
			case metav1.ConditionFalse:
				return false, fmt.Errorf(c.Message)
			}
		}
		return false, nil
	}
}
