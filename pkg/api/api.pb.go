// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.30.0
// 	protoc        v4.25.1
// source: pkg/api/api.proto

package api

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type ResultCode int32

const (
	ResultCode_Success      ResultCode = 0
	ResultCode_Fail         ResultCode = 1
	ResultCode_Insufficient ResultCode = 2
	ResultCode_NotFound     ResultCode = 3
	ResultCode_DeviceBusy   ResultCode = 4
	ResultCode_Invalid      ResultCode = 5
	ResultCode_Unknown      ResultCode = 99
)

// Enum value maps for ResultCode.
var (
	ResultCode_name = map[int32]string{
		0:  "Success",
		1:  "Fail",
		2:  "Insufficient",
		3:  "NotFound",
		4:  "DeviceBusy",
		5:  "Invalid",
		99: "Unknown",
	}
	ResultCode_value = map[string]int32{
		"Success":      0,
		"Fail":         1,
		"Insufficient": 2,
		"NotFound":     3,
		"DeviceBusy":   4,
		"Invalid":      5,
		"Unknown":      99,
	}
)

func (x ResultCode) Enum() *ResultCode {
	p := new(ResultCode)
	*p = x
	return p
}

func (x ResultCode) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (ResultCode) Descriptor() protoreflect.EnumDescriptor {
	return file_pkg_api_api_proto_enumTypes[0].Descriptor()
}

func (ResultCode) Type() protoreflect.EnumType {
	return &file_pkg_api_api_proto_enumTypes[0]
}

func (x ResultCode) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use ResultCode.Descriptor instead.
func (ResultCode) EnumDescriptor() ([]byte, []int) {
	return file_pkg_api_api_proto_rawDescGZIP(), []int{0}
}

type Container struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Index uint32 `protobuf:"varint,1,opt,name=index,proto3" json:"index,omitempty"`
	Name  string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
}

func (x *Container) Reset() {
	*x = Container{}
	if protoimpl.UnsafeEnabled {
		mi := &file_pkg_api_api_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Container) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Container) ProtoMessage() {}

func (x *Container) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_api_api_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Container.ProtoReflect.Descriptor instead.
func (*Container) Descriptor() ([]byte, []int) {
	return file_pkg_api_api_proto_rawDescGZIP(), []int{0}
}

func (x *Container) GetIndex() uint32 {
	if x != nil {
		return x.Index
	}
	return 0
}

func (x *Container) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

type MountDeviceRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	PodName        string            `protobuf:"bytes,1,opt,name=pod_name,json=podName,proto3" json:"pod_name,omitempty"`
	PodNamespace   string            `protobuf:"bytes,2,opt,name=pod_namespace,json=podNamespace,proto3" json:"pod_namespace,omitempty"`
	Container      *Container        `protobuf:"bytes,3,opt,name=container,proto3" json:"container,omitempty"`
	Resources      map[string]string `protobuf:"bytes,4,rep,name=resources,proto3" json:"resources,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
	Annotations    map[string]string `protobuf:"bytes,5,rep,name=annotations,proto3" json:"annotations,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
	DeviceType     string            `protobuf:"bytes,6,opt,name=device_type,json=deviceType,proto3" json:"device_type,omitempty"`
	IsEntireMount  bool              `protobuf:"varint,7,opt,name=is_entire_mount,json=isEntireMount,proto3" json:"is_entire_mount,omitempty"`
	TimeoutSeconds uint32            `protobuf:"varint,8,opt,name=timeout_seconds,json=timeoutSeconds,proto3" json:"timeout_seconds,omitempty"`
}

func (x *MountDeviceRequest) Reset() {
	*x = MountDeviceRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_pkg_api_api_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MountDeviceRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MountDeviceRequest) ProtoMessage() {}

func (x *MountDeviceRequest) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_api_api_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MountDeviceRequest.ProtoReflect.Descriptor instead.
func (*MountDeviceRequest) Descriptor() ([]byte, []int) {
	return file_pkg_api_api_proto_rawDescGZIP(), []int{1}
}

func (x *MountDeviceRequest) GetPodName() string {
	if x != nil {
		return x.PodName
	}
	return ""
}

func (x *MountDeviceRequest) GetPodNamespace() string {
	if x != nil {
		return x.PodNamespace
	}
	return ""
}

func (x *MountDeviceRequest) GetContainer() *Container {
	if x != nil {
		return x.Container
	}
	return nil
}

func (x *MountDeviceRequest) GetResources() map[string]string {
	if x != nil {
		return x.Resources
	}
	return nil
}

func (x *MountDeviceRequest) GetAnnotations() map[string]string {
	if x != nil {
		return x.Annotations
	}
	return nil
}

func (x *MountDeviceRequest) GetDeviceType() string {
	if x != nil {
		return x.DeviceType
	}
	return ""
}

func (x *MountDeviceRequest) GetIsEntireMount() bool {
	if x != nil {
		return x.IsEntireMount
	}
	return false
}

func (x *MountDeviceRequest) GetTimeoutSeconds() uint32 {
	if x != nil {
		return x.TimeoutSeconds
	}
	return 0
}

type UnMountDeviceRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	PodName      string     `protobuf:"bytes,1,opt,name=pod_name,json=podName,proto3" json:"pod_name,omitempty"`
	PodNamespace string     `protobuf:"bytes,2,opt,name=pod_namespace,json=podNamespace,proto3" json:"pod_namespace,omitempty"`
	Container    *Container `protobuf:"bytes,3,opt,name=container,proto3" json:"container,omitempty"`
	DeviceType   string     `protobuf:"bytes,4,opt,name=device_type,json=deviceType,proto3" json:"device_type,omitempty"`
	Force        bool       `protobuf:"varint,5,opt,name=force,proto3" json:"force,omitempty"`
}

func (x *UnMountDeviceRequest) Reset() {
	*x = UnMountDeviceRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_pkg_api_api_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *UnMountDeviceRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*UnMountDeviceRequest) ProtoMessage() {}

func (x *UnMountDeviceRequest) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_api_api_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use UnMountDeviceRequest.ProtoReflect.Descriptor instead.
func (*UnMountDeviceRequest) Descriptor() ([]byte, []int) {
	return file_pkg_api_api_proto_rawDescGZIP(), []int{2}
}

func (x *UnMountDeviceRequest) GetPodName() string {
	if x != nil {
		return x.PodName
	}
	return ""
}

func (x *UnMountDeviceRequest) GetPodNamespace() string {
	if x != nil {
		return x.PodNamespace
	}
	return ""
}

func (x *UnMountDeviceRequest) GetContainer() *Container {
	if x != nil {
		return x.Container
	}
	return nil
}

func (x *UnMountDeviceRequest) GetDeviceType() string {
	if x != nil {
		return x.DeviceType
	}
	return ""
}

func (x *UnMountDeviceRequest) GetForce() bool {
	if x != nil {
		return x.Force
	}
	return false
}

type DeviceResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Result  ResultCode `protobuf:"varint,1,opt,name=result,proto3,enum=device_mount.ResultCode" json:"result,omitempty"`
	Message string     `protobuf:"bytes,2,opt,name=message,proto3" json:"message,omitempty"`
}

func (x *DeviceResponse) Reset() {
	*x = DeviceResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_pkg_api_api_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *DeviceResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*DeviceResponse) ProtoMessage() {}

func (x *DeviceResponse) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_api_api_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use DeviceResponse.ProtoReflect.Descriptor instead.
func (*DeviceResponse) Descriptor() ([]byte, []int) {
	return file_pkg_api_api_proto_rawDescGZIP(), []int{3}
}

func (x *DeviceResponse) GetResult() ResultCode {
	if x != nil {
		return x.Result
	}
	return ResultCode_Success
}

func (x *DeviceResponse) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

var File_pkg_api_api_proto protoreflect.FileDescriptor

var file_pkg_api_api_proto_rawDesc = []byte{
	0x0a, 0x11, 0x70, 0x6b, 0x67, 0x2f, 0x61, 0x70, 0x69, 0x2f, 0x61, 0x70, 0x69, 0x2e, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x12, 0x0c, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x75, 0x6e,
	0x74, 0x22, 0x35, 0x0a, 0x09, 0x43, 0x6f, 0x6e, 0x74, 0x61, 0x69, 0x6e, 0x65, 0x72, 0x12, 0x14,
	0x0a, 0x05, 0x69, 0x6e, 0x64, 0x65, 0x78, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0d, 0x52, 0x05, 0x69,
	0x6e, 0x64, 0x65, 0x78, 0x12, 0x12, 0x0a, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x02, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x22, 0x9f, 0x04, 0x0a, 0x12, 0x4d, 0x6f, 0x75,
	0x6e, 0x74, 0x44, 0x65, 0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12,
	0x19, 0x0a, 0x08, 0x70, 0x6f, 0x64, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x07, 0x70, 0x6f, 0x64, 0x4e, 0x61, 0x6d, 0x65, 0x12, 0x23, 0x0a, 0x0d, 0x70, 0x6f,
	0x64, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x73, 0x70, 0x61, 0x63, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x0c, 0x70, 0x6f, 0x64, 0x4e, 0x61, 0x6d, 0x65, 0x73, 0x70, 0x61, 0x63, 0x65, 0x12,
	0x35, 0x0a, 0x09, 0x63, 0x6f, 0x6e, 0x74, 0x61, 0x69, 0x6e, 0x65, 0x72, 0x18, 0x03, 0x20, 0x01,
	0x28, 0x0b, 0x32, 0x17, 0x2e, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x75, 0x6e,
	0x74, 0x2e, 0x43, 0x6f, 0x6e, 0x74, 0x61, 0x69, 0x6e, 0x65, 0x72, 0x52, 0x09, 0x63, 0x6f, 0x6e,
	0x74, 0x61, 0x69, 0x6e, 0x65, 0x72, 0x12, 0x4d, 0x0a, 0x09, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72,
	0x63, 0x65, 0x73, 0x18, 0x04, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x2f, 0x2e, 0x64, 0x65, 0x76, 0x69,
	0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x75, 0x6e, 0x74, 0x2e, 0x4d, 0x6f, 0x75, 0x6e, 0x74, 0x44, 0x65,
	0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x2e, 0x52, 0x65, 0x73, 0x6f,
	0x75, 0x72, 0x63, 0x65, 0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x09, 0x72, 0x65, 0x73, 0x6f,
	0x75, 0x72, 0x63, 0x65, 0x73, 0x12, 0x53, 0x0a, 0x0b, 0x61, 0x6e, 0x6e, 0x6f, 0x74, 0x61, 0x74,
	0x69, 0x6f, 0x6e, 0x73, 0x18, 0x05, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x31, 0x2e, 0x64, 0x65, 0x76,
	0x69, 0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x75, 0x6e, 0x74, 0x2e, 0x4d, 0x6f, 0x75, 0x6e, 0x74, 0x44,
	0x65, 0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x2e, 0x41, 0x6e, 0x6e,
	0x6f, 0x74, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x0b, 0x61,
	0x6e, 0x6e, 0x6f, 0x74, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x12, 0x1f, 0x0a, 0x0b, 0x64, 0x65,
	0x76, 0x69, 0x63, 0x65, 0x5f, 0x74, 0x79, 0x70, 0x65, 0x18, 0x06, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x0a, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x54, 0x79, 0x70, 0x65, 0x12, 0x26, 0x0a, 0x0f, 0x69,
	0x73, 0x5f, 0x65, 0x6e, 0x74, 0x69, 0x72, 0x65, 0x5f, 0x6d, 0x6f, 0x75, 0x6e, 0x74, 0x18, 0x07,
	0x20, 0x01, 0x28, 0x08, 0x52, 0x0d, 0x69, 0x73, 0x45, 0x6e, 0x74, 0x69, 0x72, 0x65, 0x4d, 0x6f,
	0x75, 0x6e, 0x74, 0x12, 0x27, 0x0a, 0x0f, 0x74, 0x69, 0x6d, 0x65, 0x6f, 0x75, 0x74, 0x5f, 0x73,
	0x65, 0x63, 0x6f, 0x6e, 0x64, 0x73, 0x18, 0x08, 0x20, 0x01, 0x28, 0x0d, 0x52, 0x0e, 0x74, 0x69,
	0x6d, 0x65, 0x6f, 0x75, 0x74, 0x53, 0x65, 0x63, 0x6f, 0x6e, 0x64, 0x73, 0x1a, 0x3c, 0x0a, 0x0e,
	0x52, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10,
	0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79,
	0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02, 0x38, 0x01, 0x1a, 0x3e, 0x0a, 0x10, 0x41, 0x6e,
	0x6e, 0x6f, 0x74, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10,
	0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79,
	0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02, 0x38, 0x01, 0x22, 0xc4, 0x01, 0x0a, 0x14, 0x55,
	0x6e, 0x4d, 0x6f, 0x75, 0x6e, 0x74, 0x44, 0x65, 0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75,
	0x65, 0x73, 0x74, 0x12, 0x19, 0x0a, 0x08, 0x70, 0x6f, 0x64, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x70, 0x6f, 0x64, 0x4e, 0x61, 0x6d, 0x65, 0x12, 0x23,
	0x0a, 0x0d, 0x70, 0x6f, 0x64, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x73, 0x70, 0x61, 0x63, 0x65, 0x18,
	0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0c, 0x70, 0x6f, 0x64, 0x4e, 0x61, 0x6d, 0x65, 0x73, 0x70,
	0x61, 0x63, 0x65, 0x12, 0x35, 0x0a, 0x09, 0x63, 0x6f, 0x6e, 0x74, 0x61, 0x69, 0x6e, 0x65, 0x72,
	0x18, 0x03, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x17, 0x2e, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x5f,
	0x6d, 0x6f, 0x75, 0x6e, 0x74, 0x2e, 0x43, 0x6f, 0x6e, 0x74, 0x61, 0x69, 0x6e, 0x65, 0x72, 0x52,
	0x09, 0x63, 0x6f, 0x6e, 0x74, 0x61, 0x69, 0x6e, 0x65, 0x72, 0x12, 0x1f, 0x0a, 0x0b, 0x64, 0x65,
	0x76, 0x69, 0x63, 0x65, 0x5f, 0x74, 0x79, 0x70, 0x65, 0x18, 0x04, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x0a, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x54, 0x79, 0x70, 0x65, 0x12, 0x14, 0x0a, 0x05, 0x66,
	0x6f, 0x72, 0x63, 0x65, 0x18, 0x05, 0x20, 0x01, 0x28, 0x08, 0x52, 0x05, 0x66, 0x6f, 0x72, 0x63,
	0x65, 0x22, 0x5c, 0x0a, 0x0e, 0x44, 0x65, 0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f,
	0x6e, 0x73, 0x65, 0x12, 0x30, 0x0a, 0x06, 0x72, 0x65, 0x73, 0x75, 0x6c, 0x74, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x0e, 0x32, 0x18, 0x2e, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x75,
	0x6e, 0x74, 0x2e, 0x52, 0x65, 0x73, 0x75, 0x6c, 0x74, 0x43, 0x6f, 0x64, 0x65, 0x52, 0x06, 0x72,
	0x65, 0x73, 0x75, 0x6c, 0x74, 0x12, 0x18, 0x0a, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65,
	0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x2a,
	0x6d, 0x0a, 0x0a, 0x52, 0x65, 0x73, 0x75, 0x6c, 0x74, 0x43, 0x6f, 0x64, 0x65, 0x12, 0x0b, 0x0a,
	0x07, 0x53, 0x75, 0x63, 0x63, 0x65, 0x73, 0x73, 0x10, 0x00, 0x12, 0x08, 0x0a, 0x04, 0x46, 0x61,
	0x69, 0x6c, 0x10, 0x01, 0x12, 0x10, 0x0a, 0x0c, 0x49, 0x6e, 0x73, 0x75, 0x66, 0x66, 0x69, 0x63,
	0x69, 0x65, 0x6e, 0x74, 0x10, 0x02, 0x12, 0x0c, 0x0a, 0x08, 0x4e, 0x6f, 0x74, 0x46, 0x6f, 0x75,
	0x6e, 0x64, 0x10, 0x03, 0x12, 0x0e, 0x0a, 0x0a, 0x44, 0x65, 0x76, 0x69, 0x63, 0x65, 0x42, 0x75,
	0x73, 0x79, 0x10, 0x04, 0x12, 0x0b, 0x0a, 0x07, 0x49, 0x6e, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x10,
	0x05, 0x12, 0x0b, 0x0a, 0x07, 0x55, 0x6e, 0x6b, 0x6e, 0x6f, 0x77, 0x6e, 0x10, 0x63, 0x32, 0xba,
	0x01, 0x0a, 0x12, 0x44, 0x65, 0x76, 0x69, 0x63, 0x65, 0x4d, 0x6f, 0x75, 0x6e, 0x74, 0x53, 0x65,
	0x72, 0x76, 0x69, 0x63, 0x65, 0x12, 0x4f, 0x0a, 0x0b, 0x4d, 0x6f, 0x75, 0x6e, 0x74, 0x44, 0x65,
	0x76, 0x69, 0x63, 0x65, 0x12, 0x20, 0x2e, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x5f, 0x6d, 0x6f,
	0x75, 0x6e, 0x74, 0x2e, 0x4d, 0x6f, 0x75, 0x6e, 0x74, 0x44, 0x65, 0x76, 0x69, 0x63, 0x65, 0x52,
	0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x1c, 0x2e, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65, 0x5f,
	0x6d, 0x6f, 0x75, 0x6e, 0x74, 0x2e, 0x44, 0x65, 0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73, 0x70,
	0x6f, 0x6e, 0x73, 0x65, 0x22, 0x00, 0x12, 0x53, 0x0a, 0x0d, 0x55, 0x6e, 0x4d, 0x6f, 0x75, 0x6e,
	0x74, 0x44, 0x65, 0x76, 0x69, 0x63, 0x65, 0x12, 0x22, 0x2e, 0x64, 0x65, 0x76, 0x69, 0x63, 0x65,
	0x5f, 0x6d, 0x6f, 0x75, 0x6e, 0x74, 0x2e, 0x55, 0x6e, 0x4d, 0x6f, 0x75, 0x6e, 0x74, 0x44, 0x65,
	0x76, 0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x1c, 0x2e, 0x64, 0x65,
	0x76, 0x69, 0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x75, 0x6e, 0x74, 0x2e, 0x44, 0x65, 0x76, 0x69, 0x63,
	0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x22, 0x00, 0x42, 0x09, 0x5a, 0x07, 0x70,
	0x6b, 0x67, 0x2f, 0x61, 0x70, 0x69, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_pkg_api_api_proto_rawDescOnce sync.Once
	file_pkg_api_api_proto_rawDescData = file_pkg_api_api_proto_rawDesc
)

func file_pkg_api_api_proto_rawDescGZIP() []byte {
	file_pkg_api_api_proto_rawDescOnce.Do(func() {
		file_pkg_api_api_proto_rawDescData = protoimpl.X.CompressGZIP(file_pkg_api_api_proto_rawDescData)
	})
	return file_pkg_api_api_proto_rawDescData
}

var file_pkg_api_api_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_pkg_api_api_proto_msgTypes = make([]protoimpl.MessageInfo, 6)
var file_pkg_api_api_proto_goTypes = []interface{}{
	(ResultCode)(0),              // 0: device_mount.ResultCode
	(*Container)(nil),            // 1: device_mount.Container
	(*MountDeviceRequest)(nil),   // 2: device_mount.MountDeviceRequest
	(*UnMountDeviceRequest)(nil), // 3: device_mount.UnMountDeviceRequest
	(*DeviceResponse)(nil),       // 4: device_mount.DeviceResponse
	nil,                          // 5: device_mount.MountDeviceRequest.ResourcesEntry
	nil,                          // 6: device_mount.MountDeviceRequest.AnnotationsEntry
}
var file_pkg_api_api_proto_depIdxs = []int32{
	1, // 0: device_mount.MountDeviceRequest.container:type_name -> device_mount.Container
	5, // 1: device_mount.MountDeviceRequest.resources:type_name -> device_mount.MountDeviceRequest.ResourcesEntry
	6, // 2: device_mount.MountDeviceRequest.annotations:type_name -> device_mount.MountDeviceRequest.AnnotationsEntry
	1, // 3: device_mount.UnMountDeviceRequest.container:type_name -> device_mount.Container
	0, // 4: device_mount.DeviceResponse.result:type_name -> device_mount.ResultCode
	2, // 5: device_mount.DeviceMountService.MountDevice:input_type -> device_mount.MountDeviceRequest
	3, // 6: device_mount.DeviceMountService.UnMountDevice:input_type -> device_mount.UnMountDeviceRequest
	4, // 7: device_mount.DeviceMountService.MountDevice:output_type -> device_mount.DeviceResponse
	4, // 8: device_mount.DeviceMountService.UnMountDevice:output_type -> device_mount.DeviceResponse
	7, // [7:9] is the sub-list for method output_type
	5, // [5:7] is the sub-list for method input_type
	5, // [5:5] is the sub-list for extension type_name
	5, // [5:5] is the sub-list for extension extendee
	0, // [0:5] is the sub-list for field type_name
}

func init() { file_pkg_api_api_proto_init() }
func file_pkg_api_api_proto_init() {
	if File_pkg_api_api_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_pkg_api_api_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Container); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_pkg_api_api_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MountDeviceRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_pkg_api_api_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*UnMountDeviceRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_pkg_api_api_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*DeviceResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_pkg_api_api_proto_rawDesc,
			NumEnums:      1,
			NumMessages:   6,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_pkg_api_api_proto_goTypes,
		DependencyIndexes: file_pkg_api_api_proto_depIdxs,
		EnumInfos:         file_pkg_api_api_proto_enumTypes,
		MessageInfos:      file_pkg_api_api_proto_msgTypes,
	}.Build()
	File_pkg_api_api_proto = out.File
	file_pkg_api_api_proto_rawDesc = nil
	file_pkg_api_api_proto_goTypes = nil
	file_pkg_api_api_proto_depIdxs = nil
}
