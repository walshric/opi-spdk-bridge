// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.
// Copyright (C) 2023 Intel Corporation

// Package frontend implememnts the FrontEnd APIs (host facing) of the storage Server
package frontend

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	pc "github.com/opiproject/opi-api/common/v1/gen/go"
	pb "github.com/opiproject/opi-api/storage/v1alpha1/gen/go"
)

var (
	testSubsystem = pb.NVMeSubsystem{
		Spec: &pb.NVMeSubsystemSpec{
			Id:  &pc.ObjectKey{Value: "subsystem-test"},
			Nqn: "nqn.2022-09.io.spdk:opi3",
		},
	}

	testController = pb.NVMeController{
		Spec: &pb.NVMeControllerSpec{
			Id:               &pc.ObjectKey{Value: "controller-test"},
			SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
			PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
			NvmeControllerId: 17,
		},
		Status: &pb.NVMeControllerStatus{
			Active: true,
		},
	}

	testNamespace = pb.NVMeNamespace{
		Spec: &pb.NVMeNamespaceSpec{
			Id:          &pc.ObjectKey{Value: "namespace-test"},
			HostNsid:    22,
			SubsystemId: testSubsystem.Spec.Id,
		},
		Status: &pb.NVMeNamespaceStatus{
			PciState:     2,
			PciOperState: 1,
		},
	}
)

func TestFrontEnd_CreateNVMeSubsystem(t *testing.T) {
	spec := &pb.NVMeSubsystemSpec{
		Id:           &pc.ObjectKey{Value: "subsystem-test"},
		Nqn:          "nqn.2022-09.io.spdk:opi3",
		SerialNumber: "OpiSerialNumber",
		ModelNumber:  "OpiModelNumber",
	}
	tests := map[string]struct {
		in      *pb.NVMeSubsystem
		out     *pb.NVMeSubsystem
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
		exist   bool
	}{
		"valid request with invalid SPDK response": {
			&pb.NVMeSubsystem{
				Spec: spec,
			},
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":false}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not create NQN: %v", "nqn.2022-09.io.spdk:opi3"),
			true,
			false,
		},
		"valid request with empty SPDK response": {
			&pb.NVMeSubsystem{
				Spec: spec,
			},
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_create_subsystem: %v", "EOF"),
			true,
			false,
		},
		"valid request with ID mismatch SPDK response": {
			&pb.NVMeSubsystem{
				Spec: spec,
			},
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_create_subsystem: %v", "json response ID mismatch"),
			true,
			false,
		},
		"valid request with error code from SPDK response": {
			&pb.NVMeSubsystem{
				Spec: spec,
			},
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_create_subsystem: %v", "json response error: myopierr"),
			true,
			false,
		},
		"valid request with error code from SPDK version response": {
			&pb.NVMeSubsystem{
				Spec: spec,
			},
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":true}`, `{"id":%d,"error":{"code":1,"message":"myopierr"},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("spdk_get_version: %v", "json response error: myopierr"),
			true,
			false,
		},
		"valid request with valid SPDK response": {
			&pb.NVMeSubsystem{
				Spec: spec,
			},
			&pb.NVMeSubsystem{
				Spec: spec,
				Status: &pb.NVMeSubsystemStatus{
					FirmwareRevision: "SPDK v20.10",
				},
			},
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":true}`, `{"jsonrpc":"2.0","id":%d,"result":{"version":"SPDK v20.10","fields":{"major":20,"minor":10,"patch":0,"suffix":""}}}`}, // `{"jsonrpc": "2.0", "id": 1, "result": True}`,
			codes.OK,
			"",
			true,
			false,
		},
		"already exists": {
			&pb.NVMeSubsystem{
				Spec: spec,
			},
			&testSubsystem,
			[]string{""},
			codes.OK,
			"",
			false,
			true,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()

			testEnv.opiSpdkServer.Nvme.Controllers[testController.Spec.Id.Value] = &testController
			testEnv.opiSpdkServer.Nvme.Namespaces[testNamespace.Spec.Id.Value] = &testNamespace
			if tt.exist {
				testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem
			}

			request := &pb.CreateNVMeSubsystemRequest{NvMeSubsystem: tt.in}
			response, err := testEnv.client.CreateNVMeSubsystem(testEnv.ctx, request)
			if response != nil {
				mtt, _ := proto.Marshal(tt.out)
				mResponse, _ := proto.Marshal(response)
				if !bytes.Equal(mtt, mResponse) {
					t.Error("response: expected", tt.out, "received", response)
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_UpdateNVMeSubsystem(t *testing.T) {
	tests := map[string]struct {
		in      *pb.NVMeSubsystem
		out     *pb.NVMeSubsystem
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
	}{
		"unimplemented method": {
			&pb.NVMeSubsystem{},
			nil,
			[]string{""},
			codes.Unimplemented,
			fmt.Sprintf("%v method is not implemented", "UpdateNVMeSubsystem"),
			false,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()

			request := &pb.UpdateNVMeSubsystemRequest{NvMeSubsystem: tt.in}
			response, err := testEnv.client.UpdateNVMeSubsystem(testEnv.ctx, request)
			if response != nil {
				// Marshall the request and response, so we can just compare the contained data
				mtt, _ := proto.Marshal(tt.out.Spec)
				mResponse, _ := proto.Marshal(response.Spec)

				// Compare the marshalled messages
				if !bytes.Equal(mtt, mResponse) {
					t.Error("response: expected", tt.out.GetSpec(), "received", response.GetSpec())
				}
				if !reflect.DeepEqual(response.Status, tt.out.Status) {
					t.Error("response: expected", tt.out.GetStatus(), "received", response.GetStatus())
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_ListNVMeSubsystem(t *testing.T) {
	tests := map[string]struct {
		out     []*pb.NVMeSubsystem
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
		size    int32
		token   string
	}{
		"valid request with invalid SPDK response": {
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[]}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not create NQN: %v", "nqn.2022-09.io.spdk:opi3"),
			true,
			0,
			"",
		},
		"valid request with empty SPDK response": {
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "EOF"),
			true,
			0,
			"",
		},
		"valid request with ID mismatch SPDK response": {
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":[]}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json response ID mismatch"),
			true,
			0,
			"",
		},
		"valid request with error code from SPDK response": {
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"},"result":[]}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json response error: myopierr"),
			true,
			0,
			"",
		},
		"valid request with valid SPDK response": {
			[]*pb.NVMeSubsystem{
				{
					Spec: &pb.NVMeSubsystemSpec{
						Nqn:          "nqn.2022-09.io.spdk:opi1",
						SerialNumber: "OpiSerialNumber1",
						ModelNumber:  "OpiModelNumber1",
					},
				},
				{
					Spec: &pb.NVMeSubsystemSpec{
						Nqn:          "nqn.2022-09.io.spdk:opi2",
						SerialNumber: "OpiSerialNumber2",
						ModelNumber:  "OpiModelNumber2",
					},
				},
				{
					Spec: &pb.NVMeSubsystemSpec{
						Nqn:          "nqn.2022-09.io.spdk:opi3",
						SerialNumber: "OpiSerialNumber3",
						ModelNumber:  "OpiModelNumber3",
					},
				},
			},
			// {'jsonrpc': '2.0', 'id': 1, 'result': [{'nqn': 'nqn.2020-12.mlnx.snap', 'serial_number': 'Mellanox_NVMe_SNAP', 'model_number': 'Mellanox NVMe SNAP Controller', 'controllers': [{'name': 'NvmeEmu0pf1', 'cntlid': 0, 'pci_bdf': 'ca:00.3', 'pci_index': 1}]}]}
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[{"nqn": "nqn.2022-09.io.spdk:opi1", "serial_number": "OpiSerialNumber1", "model_number": "OpiModelNumber1"},{"nqn": "nqn.2022-09.io.spdk:opi2", "serial_number": "OpiSerialNumber2", "model_number": "OpiModelNumber2"},{"nqn": "nqn.2022-09.io.spdk:opi3", "serial_number": "OpiSerialNumber3", "model_number": "OpiModelNumber3"}]}`},
			codes.OK,
			"",
			true,
			0,
			"",
		},
		"pagination negative": {
			nil,
			[]string{},
			codes.InvalidArgument,
			"negative PageSize is not allowed",
			false,
			-10,
			"",
		},
		"pagination error": {
			nil,
			[]string{},
			codes.NotFound,
			fmt.Sprintf("unable to find pagination token %s", "unknown-pagination-token"),
			false,
			0,
			"unknown-pagination-token",
		},
		"pagination": {
			[]*pb.NVMeSubsystem{
				{
					Spec: &pb.NVMeSubsystemSpec{
						Nqn:          "nqn.2022-09.io.spdk:opi1",
						SerialNumber: "OpiSerialNumber1",
						ModelNumber:  "OpiModelNumber1",
					},
				},
			},
			// {'jsonrpc': '2.0', 'id': 1, 'result': [{'nqn': 'nqn.2020-12.mlnx.snap', 'serial_number': 'Mellanox_NVMe_SNAP', 'model_number': 'Mellanox NVMe SNAP Controller', 'controllers': [{'name': 'NvmeEmu0pf1', 'cntlid': 0, 'pci_bdf': 'ca:00.3', 'pci_index': 1}]}]}
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[{"nqn": "nqn.2022-09.io.spdk:opi1", "serial_number": "OpiSerialNumber1", "model_number": "OpiModelNumber1"},{"nqn": "nqn.2022-09.io.spdk:opi2", "serial_number": "OpiSerialNumber2", "model_number": "OpiModelNumber2"},{"nqn": "nqn.2022-09.io.spdk:opi3", "serial_number": "OpiSerialNumber3", "model_number": "OpiModelNumber3"}]}`},
			codes.OK,
			"",
			true,
			1,
			"",
		},
		"pagination offset": {
			[]*pb.NVMeSubsystem{
				{
					Spec: &pb.NVMeSubsystemSpec{
						Nqn:          "nqn.2022-09.io.spdk:opi2",
						SerialNumber: "OpiSerialNumber2",
						ModelNumber:  "OpiModelNumber2",
					},
				},
			},
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[{"nqn": "nqn.2022-09.io.spdk:opi1", "serial_number": "OpiSerialNumber1", "model_number": "OpiModelNumber1"},{"nqn": "nqn.2022-09.io.spdk:opi2", "serial_number": "OpiSerialNumber2", "model_number": "OpiModelNumber2"},{"nqn": "nqn.2022-09.io.spdk:opi3", "serial_number": "OpiSerialNumber3", "model_number": "OpiModelNumber3"}]}`},
			codes.OK,
			"",
			true,
			1,
			"existing-pagination-token",
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()

			testEnv.opiSpdkServer.Pagination["existing-pagination-token"] = 1

			request := &pb.ListNVMeSubsystemsRequest{PageSize: tt.size, PageToken: tt.token}
			response, err := testEnv.client.ListNVMeSubsystems(testEnv.ctx, request)
			if response != nil {
				if !reflect.DeepEqual(response.NvMeSubsystems, tt.out) {
					t.Error("response: expected", tt.out, "received", response.NvMeSubsystems)
				}
				// Empty NextPageToken indicates end of results list
				if tt.size != 1 && response.NextPageToken != "" {
					t.Error("Expected end of results, receieved non-empty next page token", response.NextPageToken)
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_GetNVMeSubsystem(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     *pb.NVMeSubsystem
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
	}{
		"valid request with invalid SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[]}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not find NQN: %v", "nqn.2022-09.io.spdk:opi3"),
			true,
		},
		"valid request with empty SPDK response": {
			"subsystem-test",
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "EOF"),
			true,
		},
		"valid request with ID mismatch SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":[]}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json response ID mismatch"),
			true,
		},
		"valid request with error code from SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"},"result":[]}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json response error: myopierr"),
			true,
		},
		"valid request with valid SPDK response": {
			"subsystem-test",
			&pb.NVMeSubsystem{
				Spec: &pb.NVMeSubsystemSpec{
					Nqn:          "nqn.2022-09.io.spdk:opi3",
					SerialNumber: "OpiSerialNumber3",
					ModelNumber:  "OpiModelNumber3",
				},
				Status: &pb.NVMeSubsystemStatus{
					FirmwareRevision: "TBD",
				},
			},
			// {'jsonrpc': '2.0', 'id': 1, 'result': [{'nqn': 'nqn.2020-12.mlnx.snap', 'serial_number': 'Mellanox_NVMe_SNAP', 'model_number': 'Mellanox NVMe SNAP Controller', 'controllers': [{'name': 'NvmeEmu0pf1', 'cntlid': 0, 'pci_bdf': 'ca:00.3', 'pci_index': 1}]}]}
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[{"nqn": "nqn.2022-09.io.spdk:opi1", "serial_number": "OpiSerialNumber1", "model_number": "OpiModelNumber1"},{"nqn": "nqn.2022-09.io.spdk:opi2", "serial_number": "OpiSerialNumber2", "model_number": "OpiModelNumber2"},{"nqn": "nqn.2022-09.io.spdk:opi3", "serial_number": "OpiSerialNumber3", "model_number": "OpiModelNumber3"}]}`},
			codes.OK,
			"",
			true,
		},
		"valid request with unknown key": {
			"unknown-subsystem-id",
			nil,
			[]string{""},
			codes.NotFound,
			fmt.Sprintf("unable to find key %v", "unknown-subsystem-id"),
			false,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()
			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem

			request := &pb.GetNVMeSubsystemRequest{Name: tt.in}
			response, err := testEnv.client.GetNVMeSubsystem(testEnv.ctx, request)
			if response != nil {
				if !reflect.DeepEqual(response.Spec, tt.out.Spec) {
					t.Error("response: expected", tt.out.GetSpec(), "received", response.GetSpec())
				}
				if !reflect.DeepEqual(response.Status, tt.out.Status) {
					t.Error("response: expected", tt.out.GetStatus(), "received", response.GetStatus())
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_NVMeSubsystemStats(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     *pb.VolumeStats
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
	}{
		"valid request with invalid marshal SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[]}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_stats: %v", "json: cannot unmarshal array into Go value of type spdk.NvmfGetSubsystemStatsResult"),
			true,
		},
		"valid request with empty SPDK response": {
			"subsystem-test",
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_stats: %v", "EOF"),
			true,
		},
		"valid request with ID mismatch SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":{"status": 1}}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_stats: %v", "json response ID mismatch"),
			true,
		},
		"valid request with error code from SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"}}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_stats: %v", "json response error: myopierr"),
			true,
		},
		"valid request with valid SPDK response": {
			"subsystem-test",
			&pb.VolumeStats{
				ReadOpsCount:  -1,
				WriteOpsCount: -1,
			},
			[]string{`{"jsonrpc":"2.0","id":%d,"result":{"tick_rate":2490000000,"poll_groups":[{"name":"nvmf_tgt_poll_group_0","admin_qpairs":0,"io_qpairs":0,"current_admin_qpairs":0,"current_io_qpairs":0,"pending_bdev_io":0,"transports":[{"trtype":"TCP"},{"trtype":"VFIOUSER"}]}]}}`},
			codes.OK,
			"",
			true,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()
			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem

			request := &pb.NVMeSubsystemStatsRequest{SubsystemId: &pc.ObjectKey{Value: tt.in}}
			response, err := testEnv.client.NVMeSubsystemStats(testEnv.ctx, request)
			if response != nil {
				if !reflect.DeepEqual(response.Stats, tt.out) {
					t.Error("response: expected", tt.out, "received", response.Stats)
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_CreateNVMeController(t *testing.T) {
	tests := map[string]struct {
		in      *pb.NVMeController
		out     *pb.NVMeController
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
		exist   bool
	}{
		"valid request with invalid SPDK response": {
			&pb.NVMeController{
				Spec: &pb.NVMeControllerSpec{
					Id:               &pc.ObjectKey{Value: "controller-test"},
					SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
					PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
					NvmeControllerId: 1,
				},
			},
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":false}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not create CTRL: %v", "controller-test"),
			true,
			false,
		},
		"valid request with empty SPDK response": {
			&pb.NVMeController{
				Spec: &pb.NVMeControllerSpec{
					Id:               &pc.ObjectKey{Value: "controller-test"},
					SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
					PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
					NvmeControllerId: 1,
				},
			},
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_add_listener: %v", "EOF"),
			true,
			false,
		},
		"valid request with ID mismatch SPDK response": {
			&pb.NVMeController{
				Spec: &pb.NVMeControllerSpec{
					Id:               &pc.ObjectKey{Value: "controller-test"},
					SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
					PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
					NvmeControllerId: 1,
				},
			},
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_add_listener: %v", "json response ID mismatch"),
			true,
			false,
		},
		"valid request with error code from SPDK response": {
			&pb.NVMeController{
				Spec: &pb.NVMeControllerSpec{
					Id:               &pc.ObjectKey{Value: "controller-test"},
					SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
					PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
					NvmeControllerId: 1,
				},
			},
			nil,
			[]string{`{"id":%d,"error":{"code":-32602,"message":"Invalid parameters"}}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_add_listener: %v", "json response error: Invalid parameters"),
			true,
			false,
		},
		"valid request with valid SPDK response": {
			&pb.NVMeController{
				Spec: &pb.NVMeControllerSpec{
					Id:               &pc.ObjectKey{Value: "controller-test"},
					SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
					PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
					NvmeControllerId: 17,
				},
			},
			&pb.NVMeController{
				Spec: &pb.NVMeControllerSpec{
					Id:               &pc.ObjectKey{Value: "controller-test"},
					SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
					PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
					NvmeControllerId: -1,
				},
				Status: &pb.NVMeControllerStatus{
					Active: true,
				},
			},
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":true}`},
			codes.OK,
			"",
			true,
			false,
		},
		"already exists": {
			&pb.NVMeController{
				Spec: &pb.NVMeControllerSpec{
					Id:               &pc.ObjectKey{Value: "controller-test"},
					SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
					PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
					NvmeControllerId: 17,
				},
			},
			&testController,
			[]string{""},
			codes.OK,
			"",
			false,
			true,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()

			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem
			testEnv.opiSpdkServer.Nvme.Namespaces[testNamespace.Spec.Id.Value] = &testNamespace
			if tt.exist {
				testEnv.opiSpdkServer.Nvme.Controllers[testController.Spec.Id.Value] = &testController
			}

			request := &pb.CreateNVMeControllerRequest{NvMeController: tt.in}
			response, err := testEnv.client.CreateNVMeController(testEnv.ctx, request)
			if response != nil {
				mtt, _ := proto.Marshal(tt.out)
				mResponse, _ := proto.Marshal(response)
				if !bytes.Equal(mtt, mResponse) {
					t.Error("response: expected", tt.out, "received", response)
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_UpdateNVMeController(t *testing.T) {
	spec := &pb.NVMeControllerSpec{
		Id:               &pc.ObjectKey{Value: "controller-test"},
		SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
		PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
		NvmeControllerId: 17,
	}
	tests := map[string]struct {
		in      *pb.NVMeController
		out     *pb.NVMeController
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
	}{
		"valid request without SPDK": {
			&pb.NVMeController{
				Spec: spec,
			},
			&pb.NVMeController{
				Spec: spec,
				Status: &pb.NVMeControllerStatus{
					Active: true,
				},
			},
			[]string{""},
			codes.OK,
			"",
			false,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()

			request := &pb.UpdateNVMeControllerRequest{NvMeController: tt.in}
			response, err := testEnv.client.UpdateNVMeController(testEnv.ctx, request)
			if response != nil {
				// Marshall the request and response, so we can just compare the contained data
				mtt, _ := proto.Marshal(tt.out.Spec)
				mResponse, _ := proto.Marshal(response.Spec)

				// Compare the marshalled messages
				if !bytes.Equal(mtt, mResponse) {
					t.Error("response: expected", tt.out.GetSpec(), "received", response.GetSpec())
				}
				if !reflect.DeepEqual(response.Status, tt.out.Status) {
					t.Error("response: expected", tt.out.GetStatus(), "received", response.GetStatus())
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_ListNVMeControllers(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     []*pb.NVMeController
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
		size    int32
		token   string
	}{
		"valid request without SPDK": {
			"subsystem-test",
			[]*pb.NVMeController{
				{
					Spec: &pb.NVMeControllerSpec{
						Id:               &pc.ObjectKey{Value: "controller-test"},
						SubsystemId:      &pc.ObjectKey{Value: "subsystem-test"},
						PcieId:           &pb.PciEndpoint{PhysicalFunction: 1, VirtualFunction: 2},
						NvmeControllerId: 17,
					},
					Status: &pb.NVMeControllerStatus{
						Active: true,
					},
				},
			},
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[{"subnqn": "nqn.2022-09.io.spdk:opi3", "cntlid": 1, "name": "NvmeEmu0pf1", "type": "nvme", "pci_index": 1, "pci_bdf": "ca:00.3"},{"subnqn": "nqn.2022-09.io.spdk:opi3", "cntlid": 2, "name": "NvmeEmu0pf1", "type": "nvme", "pci_index": 2, "pci_bdf": "ca:00.4"},{"subnqn": "nqn.2022-09.io.spdk:opi3", "cntlid": 3, "name": "NvmeEmu0pf1", "type": "nvme", "pci_index": 3, "pci_bdf": "ca:00.5"}]}`},
			codes.OK,
			"",
			false,
			0,
			"",
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()
			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem
			testEnv.opiSpdkServer.Nvme.Controllers[testController.Spec.Id.Value] = &testController

			request := &pb.ListNVMeControllersRequest{Parent: tt.in, PageSize: tt.size, PageToken: tt.token}
			response, err := testEnv.client.ListNVMeControllers(testEnv.ctx, request)
			if response != nil {
				if !reflect.DeepEqual(response.NvMeControllers, tt.out) {
					t.Error("response: expected", tt.out, "received", response.NvMeControllers)
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_GetNVMeController(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     *pb.NVMeController
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
	}{
		"valid request with valid SPDK response": {
			"controller-test",
			&pb.NVMeController{
				Spec: &pb.NVMeControllerSpec{
					Id:               &pc.ObjectKey{Value: "controller-test"},
					NvmeControllerId: 17,
				},
				Status: &pb.NVMeControllerStatus{
					Active: true,
				},
			},
			[]string{""},
			codes.OK,
			"",
			false,
		},
		"valid request with unknown key": {
			"unknown-controller-id",
			nil,
			[]string{""},
			codes.NotFound,
			fmt.Sprintf("unable to find key %s", "unknown-controller-id"),
			false,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()
			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem
			testEnv.opiSpdkServer.Nvme.Controllers[testController.Spec.Id.Value] = &testController

			request := &pb.GetNVMeControllerRequest{Name: tt.in}
			response, err := testEnv.client.GetNVMeController(testEnv.ctx, request)
			if response != nil {
				if !reflect.DeepEqual(response.Spec, tt.out.Spec) {
					t.Error("response: expected", tt.out.GetSpec(), "received", response.GetSpec())
				}
				if !reflect.DeepEqual(response.Status, tt.out.Status) {
					t.Error("response: expected", tt.out.GetStatus(), "received", response.GetStatus())
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_NVMeControllerStats(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     *pb.VolumeStats
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
	}{
		"valid request with valid SPDK response": {
			"subsystem-test",
			&pb.VolumeStats{
				ReadOpsCount:  -1,
				WriteOpsCount: -1,
			},
			[]string{""},
			codes.OK,
			"",
			false,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()

			request := &pb.NVMeControllerStatsRequest{Id: &pc.ObjectKey{Value: tt.in}}
			response, err := testEnv.client.NVMeControllerStats(testEnv.ctx, request)
			if response != nil {
				if !reflect.DeepEqual(response.Stats, tt.out) {
					t.Error("response: expected", tt.out, "received", response.Stats)
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_CreateNVMeNamespace(t *testing.T) {
	spec := &pb.NVMeNamespaceSpec{
		Id:          &pc.ObjectKey{Value: "namespace-test"},
		SubsystemId: &pc.ObjectKey{Value: "subsystem-test"},
		HostNsid:    0,
		VolumeId:    &pc.ObjectKey{Value: "Malloc1"},
		Uuid:        &pc.Uuid{Value: "1b4e28ba-2fa1-11d2-883f-b9a761bde3fb"},
		Nguid:       "1b4e28ba-2fa1-11d2-883f-b9a761bde3fb",
		Eui64:       1967554867335598546,
	}
	namespaceSpec := &pb.NVMeNamespaceSpec{
		Id:          &pc.ObjectKey{Value: "namespace-test"},
		SubsystemId: &pc.ObjectKey{Value: "subsystem-test"},
		HostNsid:    22,
		VolumeId:    &pc.ObjectKey{Value: "Malloc1"},
		Uuid:        &pc.Uuid{Value: "1b4e28ba-2fa1-11d2-883f-b9a761bde3fb"},
		Nguid:       "1b4e28ba-2fa1-11d2-883f-b9a761bde3fb",
		Eui64:       1967554867335598546,
	}
	tests := map[string]struct {
		in      *pb.NVMeNamespace
		out     *pb.NVMeNamespace
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
		exist   bool
	}{
		"valid request with invalid SPDK response": {
			&pb.NVMeNamespace{
				Spec: spec,
			},
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":-1}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not create NS: %v", "namespace-test"),
			true,
			false,
		},
		"valid request with empty SPDK response": {
			&pb.NVMeNamespace{
				Spec: spec,
			},
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_add_ns: %v", "EOF"),
			true,
			false,
		},
		"valid request with ID mismatch SPDK response": {
			&pb.NVMeNamespace{
				Spec: spec,
			},
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":-1}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_add_ns: %v", "json response ID mismatch"),
			true,
			false,
		},
		"valid request with error code from SPDK response": {
			&pb.NVMeNamespace{
				Spec: spec,
			},
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"},"result":-1}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_add_ns: %v", "json response error: myopierr"),
			true,
			false,
		},
		"valid request with valid SPDK response": {
			&pb.NVMeNamespace{
				Spec: namespaceSpec,
			},
			&pb.NVMeNamespace{
				Spec: namespaceSpec,
				Status: &pb.NVMeNamespaceStatus{
					PciState:     2,
					PciOperState: 1,
				},
			},
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":22}`},
			codes.OK,
			"",
			true,
			false,
		},
		"already exists": {
			&pb.NVMeNamespace{
				Spec: spec,
			},
			&testNamespace,
			[]string{""},
			codes.OK,
			"",
			false,
			true,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()

			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem
			testEnv.opiSpdkServer.Nvme.Controllers[testController.Spec.Id.Value] = &testController
			if tt.exist {
				testEnv.opiSpdkServer.Nvme.Namespaces[testNamespace.Spec.Id.Value] = &testNamespace
			}

			request := &pb.CreateNVMeNamespaceRequest{NvMeNamespace: tt.in}
			response, err := testEnv.client.CreateNVMeNamespace(testEnv.ctx, request)
			if response != nil {
				mtt, _ := proto.Marshal(tt.out)
				mResponse, _ := proto.Marshal(response)
				if !bytes.Equal(mtt, mResponse) {
					t.Error("response: expected", tt.out, "received", response)
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_UpdateNVMeNamespace(t *testing.T) {
	spec := &pb.NVMeNamespaceSpec{
		Id:          &pc.ObjectKey{Value: "namespace-test"},
		SubsystemId: &pc.ObjectKey{Value: "subsystem-test"},
		HostNsid:    22,
		VolumeId:    &pc.ObjectKey{Value: "Malloc1"},
		Uuid:        &pc.Uuid{Value: "1b4e28ba-2fa1-11d2-883f-b9a761bde3fb"},
		Nguid:       "1b4e28ba-2fa1-11d2-883f-b9a761bde3fb",
		Eui64:       1967554867335598546,
	}
	tests := map[string]struct {
		in      *pb.NVMeNamespace
		out     *pb.NVMeNamespace
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
	}{
		"valid request without SPDK": {
			&pb.NVMeNamespace{
				Spec: spec,
			},
			&pb.NVMeNamespace{
				Spec: spec,
				Status: &pb.NVMeNamespaceStatus{
					PciState:     2,
					PciOperState: 1,
				},
			},
			[]string{""},
			codes.OK,
			"",
			false,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()

			request := &pb.UpdateNVMeNamespaceRequest{NvMeNamespace: tt.in}
			response, err := testEnv.client.UpdateNVMeNamespace(testEnv.ctx, request)
			if response != nil {
				// Marshall the request and response, so we can just compare the contained data
				mtt, _ := proto.Marshal(tt.out.Spec)
				mResponse, _ := proto.Marshal(response.Spec)

				// Compare the marshalled messages
				if !bytes.Equal(mtt, mResponse) {
					t.Error("response: expected", tt.out.GetSpec(), "received", response.GetSpec())
				}
				if !reflect.DeepEqual(response.Status, tt.out.Status) {
					t.Error("response: expected", tt.out.GetStatus(), "received", response.GetStatus())
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_ListNVMeNamespaces(t *testing.T) {
	testNamespaces := []pb.NVMeNamespace{
		{
			Spec: &pb.NVMeNamespaceSpec{
				HostNsid: 11,
			},
		},
		{
			Spec: &pb.NVMeNamespaceSpec{
				HostNsid: 12,
			},
		},
		{
			Spec: &pb.NVMeNamespaceSpec{
				HostNsid: 13,
			},
		},
	}
	tests := map[string]struct {
		in      string
		out     []*pb.NVMeNamespace
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
		size    int32
		token   string
	}{
		"valid request with invalid SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[]}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not find any namespaces for NQN: %v", "nqn.2022-09.io.spdk:opi3"),
			true,
			0,
			"",
		},
		"valid request with invalid marshal SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json: cannot unmarshal bool into Go value of type []spdk.NvmfGetSubsystemsResult"),
			true,
			0,
			"",
		},
		"valid request with empty SPDK response": {
			"subsystem-test",
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "EOF"),
			true,
			0,
			"",
		},
		"valid request with ID mismatch SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":[]}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json response ID mismatch"),
			true,
			0,
			"",
		},
		"valid request with error code from SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"}}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json response error: myopierr"),
			true,
			0,
			"",
		},
		"valid request with valid SPDK response": {
			"subsystem-test",
			[]*pb.NVMeNamespace{
				&testNamespaces[0],
				&testNamespaces[1],
				&testNamespaces[2],
			},
			[]string{`{"jsonrpc":"2.0","id":%d,"result":[{"nqn":"nqn.2014-08.org.nvmexpress.discovery","subtype":"Discovery","listen_addresses":[],"allow_any_host":true,"hosts":[]},{"nqn":"nqn.2022-09.io.spdk:opi3","subtype":"NVMe","listen_addresses":[{"transport":"TCP","trtype":"TCP","adrfam":"IPv4","traddr":"192.168.80.2","trsvcid":"4444"}],"allow_any_host":false,"hosts":[{"nqn":"nqn.2014-08.org.nvmexpress:uuid:feb98abe-d51f-40c8-b348-2753f3571d3c"}],"serial_number":"SPDK00000000000001","model_number":"SPDK_Controller1","max_namespaces":32,"min_cntlid":1,"max_cntlid":65519,"namespaces":[{"nsid":11,"bdev_name":"Malloc0","name":"Malloc0","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"},{"nsid":12,"bdev_name":"Malloc1","name":"Malloc1","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"},{"nsid":13,"bdev_name":"Malloc2","name":"Malloc2","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"}]}]}`},
			codes.OK,
			"",
			true,
			0,
			"",
		},
		"pagination overflow": {
			"subsystem-test",
			[]*pb.NVMeNamespace{
				&testNamespaces[0],
				&testNamespaces[1],
				&testNamespaces[2],
			},
			[]string{`{"jsonrpc":"2.0","id":%d,"result":[{"nqn":"nqn.2014-08.org.nvmexpress.discovery","subtype":"Discovery","listen_addresses":[],"allow_any_host":true,"hosts":[]},{"nqn":"nqn.2022-09.io.spdk:opi3","subtype":"NVMe","listen_addresses":[{"transport":"TCP","trtype":"TCP","adrfam":"IPv4","traddr":"192.168.80.2","trsvcid":"4444"}],"allow_any_host":false,"hosts":[{"nqn":"nqn.2014-08.org.nvmexpress:uuid:feb98abe-d51f-40c8-b348-2753f3571d3c"}],"serial_number":"SPDK00000000000001","model_number":"SPDK_Controller1","max_namespaces":32,"min_cntlid":1,"max_cntlid":65519,"namespaces":[{"nsid":11,"bdev_name":"Malloc0","name":"Malloc0","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"},{"nsid":12,"bdev_name":"Malloc1","name":"Malloc1","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"},{"nsid":13,"bdev_name":"Malloc2","name":"Malloc2","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"}]}]}`},
			codes.OK,
			"",
			true,
			1000,
			"",
		},
		"pagination negative": {
			"volume-test",
			nil,
			[]string{},
			codes.InvalidArgument,
			"negative PageSize is not allowed",
			false,
			-10,
			"",
		},
		"pagination error": {
			"volume-test",
			nil,
			[]string{},
			codes.NotFound,
			fmt.Sprintf("unable to find pagination token %s", "unknown-pagination-token"),
			false,
			0,
			"unknown-pagination-token",
		},
		"pagination": {
			"subsystem-test",
			[]*pb.NVMeNamespace{
				&testNamespaces[0],
			},
			[]string{`{"jsonrpc":"2.0","id":%d,"result":[{"nqn":"nqn.2014-08.org.nvmexpress.discovery","subtype":"Discovery","listen_addresses":[],"allow_any_host":true,"hosts":[]},{"nqn":"nqn.2022-09.io.spdk:opi3","subtype":"NVMe","listen_addresses":[{"transport":"TCP","trtype":"TCP","adrfam":"IPv4","traddr":"192.168.80.2","trsvcid":"4444"}],"allow_any_host":false,"hosts":[{"nqn":"nqn.2014-08.org.nvmexpress:uuid:feb98abe-d51f-40c8-b348-2753f3571d3c"}],"serial_number":"SPDK00000000000001","model_number":"SPDK_Controller1","max_namespaces":32,"min_cntlid":1,"max_cntlid":65519,"namespaces":[{"nsid":11,"bdev_name":"Malloc0","name":"Malloc0","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"},{"nsid":12,"bdev_name":"Malloc1","name":"Malloc1","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"},{"nsid":13,"bdev_name":"Malloc2","name":"Malloc2","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"}]}]}`},
			codes.OK,
			"",
			true,
			1,
			"",
		},
		"pagination offset": {
			"subsystem-test",
			[]*pb.NVMeNamespace{
				&testNamespaces[1],
			},
			[]string{`{"jsonrpc":"2.0","id":%d,"result":[{"nqn":"nqn.2014-08.org.nvmexpress.discovery","subtype":"Discovery","listen_addresses":[],"allow_any_host":true,"hosts":[]},{"nqn":"nqn.2022-09.io.spdk:opi3","subtype":"NVMe","listen_addresses":[{"transport":"TCP","trtype":"TCP","adrfam":"IPv4","traddr":"192.168.80.2","trsvcid":"4444"}],"allow_any_host":false,"hosts":[{"nqn":"nqn.2014-08.org.nvmexpress:uuid:feb98abe-d51f-40c8-b348-2753f3571d3c"}],"serial_number":"SPDK00000000000001","model_number":"SPDK_Controller1","max_namespaces":32,"min_cntlid":1,"max_cntlid":65519,"namespaces":[{"nsid":11,"bdev_name":"Malloc0","name":"Malloc0","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"},{"nsid":12,"bdev_name":"Malloc1","name":"Malloc1","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"},{"nsid":13,"bdev_name":"Malloc2","name":"Malloc2","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"}]}]}`},
			codes.OK,
			"",
			true,
			1,
			"existing-pagination-token",
		},
		"valid request with unknown key": {
			"unknown-namespace-id",
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("unable to find subsystem %v", "unknown-namespace-id"),
			false,
			0,
			"",
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()
			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem
			testEnv.opiSpdkServer.Nvme.Controllers[testController.Spec.Id.Value] = &testController
			testEnv.opiSpdkServer.Nvme.Namespaces["ns0"] = &testNamespaces[0]
			testEnv.opiSpdkServer.Nvme.Namespaces["ns1"] = &testNamespaces[1]
			testEnv.opiSpdkServer.Nvme.Namespaces["ns2"] = &testNamespaces[2]
			testEnv.opiSpdkServer.Pagination["existing-pagination-token"] = 1

			request := &pb.ListNVMeNamespacesRequest{Parent: tt.in, PageSize: tt.size, PageToken: tt.token}
			response, err := testEnv.client.ListNVMeNamespaces(testEnv.ctx, request)
			if response != nil {
				if !reflect.DeepEqual(response.NvMeNamespaces, tt.out) {
					t.Error("response: expected", tt.out, "received", response.NvMeNamespaces)
				}
				// Empty NextPageToken indicates end of results list
				if tt.size != 1 && response.NextPageToken != "" {
					t.Error("Expected end of results, receieved non-empty next page token", response.NextPageToken)
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_GetNVMeNamespace(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     *pb.NVMeNamespace
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
	}{
		"valid request with invalid SPDK response": {
			"namespace-test",
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":[]}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not find NQN: %v", "nqn.2022-09.io.spdk:opi3"),
			true,
		},
		"valid request with invalid marshal SPDK response": {
			"namespace-test",
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json: cannot unmarshal bool into Go value of type []spdk.NvmfGetSubsystemsResult"),
			true,
		},
		"valid request with empty SPDK response": {
			"namespace-test",
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "EOF"),
			true,
		},
		"valid request with ID mismatch SPDK response": {
			"namespace-test",
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":[]}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json response ID mismatch"),
			true,
		},
		"valid request with error code from SPDK response": {
			"namespace-test",
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"}}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_get_subsystems: %v", "json response error: myopierr"),
			true,
		},
		"valid request with valid SPDK response": {
			"namespace-test",
			&pb.NVMeNamespace{
				Spec: &pb.NVMeNamespaceSpec{
					Id:       &pc.ObjectKey{Value: "namespace-test"},
					HostNsid: 22,
				},
				Status: &pb.NVMeNamespaceStatus{
					PciState:     2,
					PciOperState: 1,
				},
			},
			[]string{`{"jsonrpc":"2.0","id":%d,"result":[{"nqn":"nqn.2014-08.org.nvmexpress.discovery","subtype":"Discovery","listen_addresses":[],"allow_any_host":true,"hosts":[]},{"nqn":"nqn.2022-09.io.spdk:opi3","subtype":"NVMe","listen_addresses":[{"transport":"TCP","trtype":"TCP","adrfam":"IPv4","traddr":"192.168.80.2","trsvcid":"4444"}],"allow_any_host":false,"hosts":[{"nqn":"nqn.2014-08.org.nvmexpress:uuid:feb98abe-d51f-40c8-b348-2753f3571d3c"}],"serial_number":"SPDK00000000000001","model_number":"SPDK_Controller1","max_namespaces":32,"min_cntlid":1,"max_cntlid":65519,"namespaces":[{"nsid":22,"bdev_name":"Malloc0","name":"Malloc0","nguid":"611C13802D994E1DAB121F38A9887929","uuid":"611c1380-2d99-4e1d-ab12-1f38a9887929"}]}]}`},
			codes.OK,
			"",
			true,
		},
		"valid request with unknown key": {
			"unknown-namespace-id",
			nil,
			[]string{""},
			codes.NotFound,
			fmt.Sprintf("unable to find key %v", "unknown-namespace-id"),
			false,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()
			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem
			testEnv.opiSpdkServer.Nvme.Controllers[testController.Spec.Id.Value] = &testController
			testEnv.opiSpdkServer.Nvme.Namespaces[testNamespace.Spec.Id.Value] = &testNamespace

			request := &pb.GetNVMeNamespaceRequest{Name: tt.in}
			response, err := testEnv.client.GetNVMeNamespace(testEnv.ctx, request)
			if response != nil {
				if !reflect.DeepEqual(response.Spec, tt.out.Spec) {
					t.Error("response: expected", tt.out.GetSpec(), "received", response.GetSpec())
				}
				if !reflect.DeepEqual(response.Status, tt.out.Status) {
					t.Error("response: expected", tt.out.GetStatus(), "received", response.GetStatus())
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_NVMeNamespaceStats(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     *pb.VolumeStats
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
	}{
		"valid request with valid SPDK response": {
			"subsystem-test",
			&pb.VolumeStats{
				ReadOpsCount:  -1,
				WriteOpsCount: -1,
			},
			[]string{""},
			codes.OK,
			"",
			false,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()

			request := &pb.NVMeNamespaceStatsRequest{NamespaceId: &pc.ObjectKey{Value: tt.in}}
			response, err := testEnv.client.NVMeNamespaceStats(testEnv.ctx, request)
			if response != nil {
				if !reflect.DeepEqual(response.Stats, tt.out) {
					t.Error("response: expected", tt.out, "received", response.Stats)
				}
			}

			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
		})
	}
}

func TestFrontEnd_DeleteNVMeNamespace(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     *emptypb.Empty
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
		missing bool
	}{
		"valid request with invalid SPDK response": {
			"namespace-test",
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":false}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not delete NS: %v", "namespace-test"),
			true,
			false,
		},
		"valid request with empty SPDK response": {
			"namespace-test",
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_remove_ns: %v", "EOF"),
			true,
			false,
		},
		"valid request with ID mismatch SPDK response": {
			"namespace-test",
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_remove_ns: %v", "json response ID mismatch"),
			true,
			false,
		},
		"valid request with error code from SPDK response": {
			"namespace-test",
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_remove_ns: %v", "json response error: myopierr"),
			true,
			false,
		},
		"valid request with valid SPDK response": {
			"namespace-test",
			&emptypb.Empty{},
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":true}`}, // `{"jsonrpc": "2.0", "id": 1, "result": True}`,
			codes.OK,
			"",
			true,
			false,
		},
		"valid request with unknown key": {
			"unknown-namespace-id",
			nil,
			[]string{""},
			codes.NotFound,
			fmt.Sprintf("unable to find key %v", "unknown-namespace-id"),
			false,
			false,
		},
		"unknown key with missing allowed": {
			"unknown-id",
			&emptypb.Empty{},
			[]string{""},
			codes.OK,
			"",
			false,
			true,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()
			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem
			testEnv.opiSpdkServer.Nvme.Controllers[testController.Spec.Id.Value] = &testController
			testEnv.opiSpdkServer.Nvme.Namespaces[testNamespace.Spec.Id.Value] = &testNamespace

			request := &pb.DeleteNVMeNamespaceRequest{Name: tt.in, AllowMissing: tt.missing}
			response, err := testEnv.client.DeleteNVMeNamespace(testEnv.ctx, request)
			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
			if reflect.TypeOf(response) != reflect.TypeOf(tt.out) {
				t.Error("response: expected", reflect.TypeOf(tt.out), "received", reflect.TypeOf(response))
			}
		})
	}
}

func TestFrontEnd_DeleteNVMeController(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     *emptypb.Empty
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
		missing bool
	}{
		"valid request with invalid SPDK response": {
			"controller-test",
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":false}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not delete NQN:ID %v", "nqn.2022-09.io.spdk:opi3:17"),
			true,
			false,
		},
		"valid request with empty SPDK response": {
			"controller-test",
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_remove_listener: %v", "EOF"),
			true,
			false,
		},
		"valid request with ID mismatch SPDK response": {
			"controller-test",
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_remove_listener: %v", "json response ID mismatch"),
			true,
			false,
		},
		"valid request with error code from SPDK response": {
			"controller-test",
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_subsystem_remove_listener: %v", "json response error: myopierr"),
			true,
			false,
		},
		"valid request with valid SPDK response": {
			"controller-test",
			&emptypb.Empty{},
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":true}`}, // `{"jsonrpc": "2.0", "id": 1, "result": True}`,
			codes.OK,
			"",
			true,
			false,
		},
		"valid request with unknown key": {
			"unknown-controller-id",
			nil,
			[]string{""},
			codes.NotFound,
			fmt.Sprintf("unable to find key %v", "unknown-controller-id"),
			false,
			false,
		},
		"unknown key with missing allowed": {
			"unknown-id",
			&emptypb.Empty{},
			[]string{""},
			codes.OK,
			"",
			false,
			true,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()
			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem
			testEnv.opiSpdkServer.Nvme.Controllers[testController.Spec.Id.Value] = &testController

			request := &pb.DeleteNVMeControllerRequest{Name: tt.in, AllowMissing: tt.missing}
			response, err := testEnv.client.DeleteNVMeController(testEnv.ctx, request)
			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
			if reflect.TypeOf(response) != reflect.TypeOf(tt.out) {
				t.Error("response: expected", reflect.TypeOf(tt.out), "received", reflect.TypeOf(response))
			}
		})
	}
}

func TestFrontEnd_DeleteNVMeSubsystem(t *testing.T) {
	tests := map[string]struct {
		in      string
		out     *emptypb.Empty
		spdk    []string
		errCode codes.Code
		errMsg  string
		start   bool
		missing bool
	}{
		"valid request with invalid SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":false}`},
			codes.InvalidArgument,
			fmt.Sprintf("Could not delete NQN: %v", "nqn.2022-09.io.spdk:opi3"),
			true,
			false,
		},
		"valid request with empty SPDK response": {
			"subsystem-test",
			nil,
			[]string{""},
			codes.Unknown,
			fmt.Sprintf("nvmf_delete_subsystem: %v", "EOF"),
			true,
			false,
		},
		"valid request with ID mismatch SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":0,"error":{"code":0,"message":""},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_delete_subsystem: %v", "json response ID mismatch"),
			true,
			false,
		},
		"valid request with error code from SPDK response": {
			"subsystem-test",
			nil,
			[]string{`{"id":%d,"error":{"code":1,"message":"myopierr"},"result":false}`},
			codes.Unknown,
			fmt.Sprintf("nvmf_delete_subsystem: %v", "json response error: myopierr"),
			true,
			false,
		},
		"valid request with valid SPDK response": {
			"subsystem-test",
			&emptypb.Empty{},
			[]string{`{"id":%d,"error":{"code":0,"message":""},"result":true}`}, // `{"jsonrpc": "2.0", "id": 1, "result": True}`,
			codes.OK,
			"",
			true,
			false,
		},
		"valid request with unknown key": {
			"unknown-subsystem-id",
			nil,
			[]string{""},
			codes.NotFound,
			fmt.Sprintf("unable to find key %v", "unknown-subsystem-id"),
			false,
			false,
		},
		"unknown key with missing allowed": {
			"unknown-id",
			&emptypb.Empty{},
			[]string{""},
			codes.OK,
			"",
			false,
			true,
		},
	}

	// run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			testEnv := createTestEnvironment(tt.start, tt.spdk)
			defer testEnv.Close()
			testEnv.opiSpdkServer.Nvme.Subsystems[testSubsystem.Spec.Id.Value] = &testSubsystem

			request := &pb.DeleteNVMeSubsystemRequest{Name: tt.in, AllowMissing: tt.missing}
			response, err := testEnv.client.DeleteNVMeSubsystem(testEnv.ctx, request)
			if err != nil {
				if er, ok := status.FromError(err); ok {
					if er.Code() != tt.errCode {
						t.Error("error code: expected", codes.InvalidArgument, "received", er.Code())
					}
					if er.Message() != tt.errMsg {
						t.Error("error message: expected", tt.errMsg, "received", er.Message())
					}
				}
			}
			if reflect.TypeOf(response) != reflect.TypeOf(tt.out) {
				t.Error("response: expected", reflect.TypeOf(tt.out), "received", reflect.TypeOf(response))
			}
		})
	}
}

func TestFrontEnd_NewTcpSubsystemListener(t *testing.T) {
	tests := map[string]struct {
		listenAddress string
		wantPanic     bool
		protocol      string
	}{
		"ipv4 valid address": {
			listenAddress: "10.10.10.10:12345",
			wantPanic:     false,
			protocol:      ipv4NvmeTCPProtocol,
		},
		"valid ipv6 addresses": {
			listenAddress: "[2002:0db0:8833:0000:0000:8a8a:0330:7337]:54321",
			wantPanic:     false,
			protocol:      ipv6NvmeTCPProtocol,
		},
		"empty string as listen address": {
			listenAddress: "",
			wantPanic:     true,
			protocol:      "",
		},
		"missing port": {
			listenAddress: "10.10.10.10",
			wantPanic:     true,
			protocol:      "",
		},
		"valid port invalid ip": {
			listenAddress: "wrong:12345",
			wantPanic:     true,
			protocol:      "",
		},
		"meaningless listen address": {
			listenAddress: "some string which is not ip address",
			wantPanic:     true,
			protocol:      "",
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantPanic {
					t.Errorf("NewTCPSubsystemListener() recover = %v, wantPanic = %v", r, tt.wantPanic)
				}
			}()

			gotSubsysListener := NewTCPSubsystemListener(tt.listenAddress)
			host, port, _ := net.SplitHostPort(tt.listenAddress)
			wantSubsysListener := &tcpSubsystemListener{
				listenAddr: net.ParseIP(host),
				listenPort: port,
				protocol:   tt.protocol,
			}

			if !reflect.DeepEqual(gotSubsysListener, wantSubsysListener) {
				t.Errorf("Expect %v subsystem listener, received %v", wantSubsysListener, gotSubsysListener)
			}
		})
	}
}
