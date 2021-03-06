// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

import (
	"errors"
	"fmt"
	"strings"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
)

const (
	// DefaultTimeout the default timeout for the entire retry operation
	defaultConfigTimeout = time.Second * 30

	// DefaultDelay the default delay between successive retry attempts
	defaultConfigDelay = time.Millisecond * 100
)

// ConfigFetchFunc retrieves the config dump from Envoy.
type ConfigFetchFunc func() (*envoyAdmin.ConfigDump, error)

// ConfigAcceptFunc evaluates the Envoy config dump and either accept/reject it. This is used
// by WaitForConfig to control the retry loop. If an error is returned, a retry will be attempted.
// Otherwise the loop is immediately terminated with an error if rejected or none if accepted.
type ConfigAcceptFunc func(*envoyAdmin.ConfigDump) (bool, error)

func WaitForConfig(fetch ConfigFetchFunc, accept ConfigAcceptFunc, options ...retry.Option) error {
	options = append([]retry.Option{retry.BackoffDelay(defaultConfigDelay), retry.Timeout(defaultConfigTimeout)}, options...)

	var cfg *envoyAdmin.ConfigDump
	_, err := retry.Do(func() (result interface{}, completed bool, err error) {
		cfg, err = fetch()
		if err != nil {
			if strings.Contains(err.Error(), "could not resolve Any message type") {
				// Unable to parse an Any in the message, likely due to missing imports.
				// This is not a recoverable error.
				return nil, true, nil
			}
			if strings.Contains(err.Error(), `Any JSON doesn't have '@type'`) {
				// Unable to parse an Any in the message, likely due to an older version.
				// This is not a recoverable error.
				return nil, true, nil
			}
			return nil, false, err
		}

		accepted, err := accept(cfg)
		if err != nil {
			// Accept returned an error - retry.
			return nil, false, err
		}

		if accepted {
			// The configuration was accepted.
			return nil, true, nil
		}

		// The configuration was rejected, don't try again.
		return nil, true, errors.New("envoy config rejected")
	}, options...)
	if err != nil {
		configDumpStr := "nil"
		if cfg != nil {
			b, err := protomarshal.MarshalIndent(cfg, "  ")
			if err == nil {
				configDumpStr = string(b)
			}
		}

		return fmt.Errorf("failed waiting for Envoy configuration: %v. Last config_dump:\n%s", err, configDumpStr)
	}
	return nil
}
