/**
# SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package dgpu_test

import (
	"fmt"
	"path/filepath"
	"testing"

	mocknvml "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	testlog "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/NVIDIA/go-nvml/pkg/nvml/mock/dgxa100"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/devices"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup/root"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/nvsandboxutils"
	mocknvsandboxutils "github.com/NVIDIA/nvidia-container-toolkit/internal/nvsandboxutils/mock"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/platform-support/dgpu"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/test"
)

func TestNewForDevice(t *testing.T) {
	logger, _ := testlog.NewNullLogger()

	defer devices.SetAllForTest()()

	moduleRoot, err := test.GetModuleRoot()
	require.NoError(t, err)

	lookupRoot := filepath.Join(moduleRoot, "testdata", "lookup")

	testCase := []struct {
		description string
		mocks
		driverRootfs        string
		devRootfs           string
		expectedErrorString string
		expectedDevices     []discover.Device
		expectedMounts      []discover.Mount
		expectedHooks       []discover.Hook
	}{
		{
			description: "single full gpu",
			mocks:       mocksServerForTest(),
			devRootfs:   "rootfs-1",
			expectedDevices: []discover.Device{
				{Path: "/dev/nvidia0", HostPath: "/dev/nvidia0"},
			},
		},
	}

	for _, tc := range testCase {
		t.Run(tc.description, func(t *testing.T) {
			driverRoot := tc.driverRootfs
			if driverRoot != "" {
				driverRoot = filepath.Join(lookupRoot, tc.driverRootfs)
			}
			devRoot := tc.devRootfs
			if devRoot != "" {
				devRoot = filepath.Join(lookupRoot, tc.devRootfs)
			}
			driver := root.New(root.WithDriverRoot(driverRoot), root.WithDevRoot(devRoot))

			devicelib := device.New(tc.nvmllib)
			device, err := devicelib.NewDevice(tc.device)
			require.NoError(t, err)

			d, err := dgpu.NewForDevice(device,
				dgpu.WithLogger(logger),
				dgpu.WithDriver(driver),
			)
			if tc.expectedErrorString == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.expectedErrorString)
				return
			}

			if devRoot == "" {
				devRoot = driverRoot
			}

			devices, err := d.Devices()
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedDevices, test.StripRoot(devices, devRoot))

			mounts, err := d.Mounts()
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedMounts, test.StripRoot(mounts, driverRoot))

			hooks, err := d.Hooks()
			require.NoError(t, err)
			require.EqualValues(t, tc.expectedHooks, test.StripRoot(hooks, driverRoot))

			envVars, err := d.EnvVars()
			require.NoError(t, err)
			require.Empty(t, envVars)
		})

	}
}

func TestNewForDeviceLogsDiscovererConstructionFailures(t *testing.T) {
	logger := &warningLogger{}
	devicelib := device.New(&mocknvml.Interface{})

	mockDevice := &mocknvml.Device{
		GetUUIDFunc: func() (string, nvml.Return) {
			return "", nvml.ERROR_UNKNOWN
		},
		GetMinorNumberFunc: func() (int, nvml.Return) {
			return 0, nvml.ERROR_UNKNOWN
		},
	}
	d, err := devicelib.NewDevice(mockDevice)
	require.NoError(t, err)

	discoverer, err := dgpu.NewForDevice(
		d,
		dgpu.WithLogger(logger),
		dgpu.WithDriver(root.New()),
		dgpu.WithNvsandboxuitilsLib(&mocknvsandboxutils.Interface{
			GetGpuResourceFunc: func(string) ([]nvsandboxutils.GpuFileInfo, nvsandboxutils.Ret) {
				return nil, nvsandboxutils.SUCCESS
			},
		}),
	)
	require.Nil(t, discoverer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get device UUID")
	require.Contains(t, err.Error(), "error getting GPU device minor number")

	require.Len(t, logger.warnings, 2)
	require.Contains(t, logger.warnings[0], "nvsandboxutils dGPU discoverer")
	require.Contains(t, logger.warnings[0], "ERROR_UNKNOWN")
	require.Contains(t, logger.warnings[1], "NVML dGPU discoverer")
	require.Contains(t, logger.warnings[1], "ERROR_UNKNOWN")
}

type warningLogger struct {
	warnings []string
}

func (*warningLogger) Debugf(string, ...any) {}
func (*warningLogger) Errorf(string, ...any) {}
func (*warningLogger) Infof(string, ...any) {}
func (l *warningLogger) Warningf(format string, args ...any) {
	l.warnings = append(l.warnings, fmt.Sprintf(format, args...))
}
func (*warningLogger) Tracef(string, ...any) {}

type mocks struct {
	nvmllib nvml.Interface
	device  nvml.Device
}

func mocksServerForTest() mocks {
	server := dgxa100.New()

	return mocks{
		nvmllib: server,
		device:  server.Devices[0],
	}
}
