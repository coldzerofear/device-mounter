// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             v4.25.1
// source: pkg/api/api.proto

package api

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	DeviceMountService_MountDevice_FullMethodName   = "/device_mount.DeviceMountService/MountDevice"
	DeviceMountService_UnMountDevice_FullMethodName = "/device_mount.DeviceMountService/UnMountDevice"
)

// DeviceMountServiceClient is the client API for DeviceMountService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type DeviceMountServiceClient interface {
	MountDevice(ctx context.Context, in *MountDeviceRequest, opts ...grpc.CallOption) (*DeviceResponse, error)
	UnMountDevice(ctx context.Context, in *UnMountDeviceRequest, opts ...grpc.CallOption) (*DeviceResponse, error)
}

type deviceMountServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewDeviceMountServiceClient(cc grpc.ClientConnInterface) DeviceMountServiceClient {
	return &deviceMountServiceClient{cc}
}

func (c *deviceMountServiceClient) MountDevice(ctx context.Context, in *MountDeviceRequest, opts ...grpc.CallOption) (*DeviceResponse, error) {
	out := new(DeviceResponse)
	err := c.cc.Invoke(ctx, DeviceMountService_MountDevice_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *deviceMountServiceClient) UnMountDevice(ctx context.Context, in *UnMountDeviceRequest, opts ...grpc.CallOption) (*DeviceResponse, error) {
	out := new(DeviceResponse)
	err := c.cc.Invoke(ctx, DeviceMountService_UnMountDevice_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeviceMountServiceServer is the server API for DeviceMountService service.
// All implementations must embed UnimplementedDeviceMountServiceServer
// for forward compatibility
type DeviceMountServiceServer interface {
	MountDevice(context.Context, *MountDeviceRequest) (*DeviceResponse, error)
	UnMountDevice(context.Context, *UnMountDeviceRequest) (*DeviceResponse, error)
	mustEmbedUnimplementedDeviceMountServiceServer()
}

// UnimplementedDeviceMountServiceServer must be embedded to have forward compatible implementations.
type UnimplementedDeviceMountServiceServer struct {
}

func (UnimplementedDeviceMountServiceServer) MountDevice(context.Context, *MountDeviceRequest) (*DeviceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method MountDevice not implemented")
}
func (UnimplementedDeviceMountServiceServer) UnMountDevice(context.Context, *UnMountDeviceRequest) (*DeviceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method UnMountDevice not implemented")
}
func (UnimplementedDeviceMountServiceServer) mustEmbedUnimplementedDeviceMountServiceServer() {}

// UnsafeDeviceMountServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to DeviceMountServiceServer will
// result in compilation errors.
type UnsafeDeviceMountServiceServer interface {
	mustEmbedUnimplementedDeviceMountServiceServer()
}

func RegisterDeviceMountServiceServer(s grpc.ServiceRegistrar, srv DeviceMountServiceServer) {
	s.RegisterService(&DeviceMountService_ServiceDesc, srv)
}

func _DeviceMountService_MountDevice_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MountDeviceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeviceMountServiceServer).MountDevice(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DeviceMountService_MountDevice_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeviceMountServiceServer).MountDevice(ctx, req.(*MountDeviceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DeviceMountService_UnMountDevice_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UnMountDeviceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeviceMountServiceServer).UnMountDevice(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: DeviceMountService_UnMountDevice_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeviceMountServiceServer).UnMountDevice(ctx, req.(*UnMountDeviceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// DeviceMountService_ServiceDesc is the grpc.ServiceDesc for DeviceMountService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var DeviceMountService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "device_mount.DeviceMountService",
	HandlerType: (*DeviceMountServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "MountDevice",
			Handler:    _DeviceMountService_MountDevice_Handler,
		},
		{
			MethodName: "UnMountDevice",
			Handler:    _DeviceMountService_UnMountDevice_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "pkg/api/api.proto",
}