package util

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/coldzerofear/device-mounter/pkg/api"
	"github.com/coldzerofear/device-mounter/pkg/config"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/cgroups/devices"
	cgroupsystemd "github.com/opencontainers/runc/libcontainer/cgroups/systemd"
	"github.com/opencontainers/runc/libcontainer/configs"
	devices2 "github.com/opencontainers/runc/libcontainer/devices"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/util/qos"
)

// Config is the nsenter configuration used to generate
// nsenter command
type Config struct {
	Cgroup              bool   // Enter cgroup namespace
	CgroupFile          string // Cgroup namespace location, default to /proc/PID/ns/cgroup
	FollowContext       bool   // Set SELinux security context
	GID                 int    // GID to use to execute given program
	IPC                 bool   // Enter IPC namespace
	IPCFile             string // IPC namespace location, default to /proc/PID/ns/ipc
	Mount               bool   // Enter mount namespace
	MountFile           string // Mount namespace location, default to /proc/PID/ns/mnt
	Net                 bool   // Enter network namespace
	NetFile             string // Network namespace location, default to /proc/PID/ns/net
	NoFork              bool   // Do not fork before executing the specified program
	PID                 bool   // Enter PID namespace
	PIDFile             string // PID namespace location, default to /proc/PID/ns/pid
	PreserveCredentials bool   // Preserve current UID/GID when entering namespaces
	RootDirectory       string // Set the root directory, default to target process root directory
	Target              int    // Target PID (required)
	UID                 int    // UID to use to execute given program
	User                bool   // Enter user namespace
	UserFile            string // User namespace location, default to /proc/PID/ns/user
	UTS                 bool   // Enter UTS namespace
	UTSFile             string // UTS namespace location, default to /proc/PID/ns/uts
	WorkingDirectory    string // Set the working directory, default to target process working directory
}

// Execute executs the givne command with a default background context
func (c *Config) Execute(program string, args ...string) (string, string, error) {
	return c.ExecuteContext(context.Background(), program, args...)
}

// ExecuteContext the given program using the given nsenter configuration and given context
// and return stdout/stderr or an error if command has failed
func (c *Config) ExecuteContext(ctx context.Context, program string, args ...string) (string, string, error) {
	cmd, err := c.buildCommand(ctx)
	if err != nil {
		return "", "", fmt.Errorf("Error while building command: %v ", err)
	}

	// Prepare command
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Args = append(cmd.Args, program)
	cmd.Args = append(cmd.Args, args...)

	err = cmd.Run()
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("Error while executing command: %v ", err)
	}

	return stdout.String(), stderr.String(), nil
}

func (c *Config) buildCommand(ctx context.Context) (*exec.Cmd, error) {
	if c.Target == 0 {
		return nil, fmt.Errorf("Target must be specified ")
	}

	var args []string
	args = append(args, "--target", strconv.Itoa(c.Target))

	if c.Cgroup {
		if c.CgroupFile != "" {
			args = append(args, fmt.Sprintf("--cgroup=%s", c.CgroupFile))
		} else {
			args = append(args, "--cgroup")
		}
	}

	if c.FollowContext {
		args = append(args, "--follow-context")
	}

	if c.GID != 0 {
		args = append(args, "--setgid", strconv.Itoa(c.GID))
	}

	if c.IPC {
		if c.IPCFile != "" {
			args = append(args, fmt.Sprintf("--ip=%s", c.IPCFile))
		} else {
			args = append(args, "--ipc")
		}
	}

	if c.Mount {
		if c.MountFile != "" {
			args = append(args, fmt.Sprintf("--mount=%s", c.MountFile))
		} else {
			args = append(args, "--mount")
		}
	}

	if c.Net {
		if c.NetFile != "" {
			args = append(args, fmt.Sprintf("--net=%s", c.NetFile))
		} else {
			args = append(args, "--net")
		}
	}

	if c.NoFork {
		args = append(args, "--no-fork")
	}

	if c.PID {
		if c.PIDFile != "" {
			args = append(args, fmt.Sprintf("--pid=%s", c.PIDFile))
		} else {
			args = append(args, "--pid")
		}
	}

	if c.PreserveCredentials {
		args = append(args, "--preserve-credentials")
	}

	if c.RootDirectory != "" {
		args = append(args, "--root", c.RootDirectory)
	}

	if c.UID != 0 {
		args = append(args, "--setuid", strconv.Itoa(c.UID))
	}

	if c.User {
		if c.UserFile != "" {
			args = append(args, fmt.Sprintf("--user=%s", c.UserFile))
		} else {
			args = append(args, "--user")
		}
	}

	if c.UTS {
		if c.UTSFile != "" {
			args = append(args, fmt.Sprintf("--uts=%s", c.UTSFile))
		} else {
			args = append(args, "--uts")
		}
	}

	if c.WorkingDirectory != "" {
		args = append(args, "--wd", c.WorkingDirectory)
	}

	cmd := exec.CommandContext(ctx, "nsenter", args...)

	return cmd, nil
}

func AddDeviceFile(config *Config, deviceInfo api.DeviceInfo) error {
	if !deviceInfo.Allow {
		return nil
	}
	cmd := fmt.Sprintf("mknod -m %s %s %c %d %d", "666",
		deviceInfo.DeviceFilePath, deviceInfo.Type, deviceInfo.Major, deviceInfo.Minor)
	stdout, stderr, err := config.Execute("sh", "-c", cmd)
	if err != nil {
		klog.Errorln("Failed to execute cmd:", cmd)
		klog.Errorln("Std Output:", stdout)
		klog.Errorln("Err Output:", stderr)
		return err
	}
	return nil
}

func RemoveDeviceFile(config *Config, deviceInfo api.DeviceInfo) error {
	if deviceInfo.Allow {
		return nil
	}
	cmd := "rm " + deviceInfo.DeviceFilePath
	stdout, stderr, err := config.Execute("sh", "-c", cmd)
	if err != nil {
		klog.Errorln("Failed to execute cmd:", cmd)
		klog.Errorln("Std Output:", stdout)
		klog.Errorln("Err Output:", stderr)
		return err
	}
	return nil
}

func KillRunningProcesses(config *Config, processes []int) error {
	var procs []string
	for _, process := range processes {
		procs = append(procs, strconv.Itoa(process))
	}
	cmd := "kill " + strings.Join(procs, " ")
	stdout, stderr, err := config.Execute("sh", "-c", cmd)
	if err != nil {
		klog.Errorln("Failed to execute cmd:", cmd)
		klog.Errorln("Std Output:", stdout)
		klog.Errorln("Err Output:", stderr)
		return err
	}
	return nil
}

func GetK8sPodDeviceCGroupFullPath(podCGroupPath string) string {
	return filepath.Join("/sys/fs/cgroup/devices", podCGroupPath)
}

func GetK8sPodCGroupFullPath(podCGroupPath string) string {
	return filepath.Join("/sys/fs/cgroup", podCGroupPath)
}

func GetContainerStatus(pod *v1.Pod, containerName string) (*v1.ContainerStatus, bool) {
	for i, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return &pod.Status.ContainerStatuses[i], true
		}
	}
	return nil, false
}

type CgroupName []string

func NewPodCgroupName(pod *v1.Pod) CgroupName {
	podQos := pod.Status.QOSClass
	if len(podQos) == 0 {
		podQos = qos.GetPodQOS(pod)
	}
	var cgroupName CgroupName
	switch podQos {
	case v1.PodQOSGuaranteed:
		cgroupName = append(cgroupName, "kubepods")
	case v1.PodQOSBurstable:
		cgroupName = append(cgroupName, "kubepods", strings.ToLower(string(v1.PodQOSBurstable)))
	case v1.PodQOSBestEffort:
		cgroupName = append(cgroupName, "kubepods", strings.ToLower(string(v1.PodQOSBestEffort)))
	}
	cgroupName = append(cgroupName, "pod"+string(pod.UID))
	return cgroupName
}

// cgroupName.ToSystemd converts the internal cgroup name to a systemd name.
// For example, the name {"kubepods", "burstable", "pod1234-abcd-5678-efgh"} becomes
// "/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod1234_abcd_5678_efgh.slice"
// This function always expands the systemd name into the cgroupfs form. If only
// the last part is needed, use path.Base(...) on it to discard the rest.
func (cgroupName CgroupName) ToSystemd() string {
	if len(cgroupName) == 0 || (len(cgroupName) == 1 && cgroupName[0] == "") {
		return "/"
	}
	newparts := []string{}
	for _, part := range cgroupName {
		part = escapeSystemdCgroupName(part)
		newparts = append(newparts, part)
	}
	result, err := cgroupsystemd.ExpandSlice(strings.Join(newparts, "-") + ".slice")
	if err != nil {
		// Should never happen...
		panic(fmt.Errorf("error converting cgroup name [%v] to systemd format: %v", cgroupName, err))
	}
	return result
}

func escapeSystemdCgroupName(part string) string {
	return strings.Replace(part, "-", "_", -1)
}

func (cgroupName CgroupName) ToCgroupfs() string {
	return "/" + path.Join(cgroupName...)
}

func GetK8sPodCGroupPath(pod *v1.Pod, container *api.Container,
	getFullPath func(string) string) (string, error) {
	var (
		runtimeName string
		containerId string
	)
	status, exist := GetContainerStatus(pod, container.Name)
	if !exist {
		return "", fmt.Errorf("failed to obtain container cgroup path")
	}
	runtimeName, containerId = parseRuntime(status.ContainerID)
	cgroupName := NewPodCgroupName(pod)
	switch config.CurrentCGroupDriver {
	case config.SYSTEMD:
		return convertSystemdFullPath(runtimeName, containerId, cgroupName, getFullPath)
	case config.CGROUPFS:
		return convertCGroupfsFullPath(runtimeName, containerId, cgroupName, getFullPath)
	default:
		return "", fmt.Errorf("unknown CGroup driver: %s", config.CurrentCGroupDriver)
	}
}

func convertSystemdFullPath(runtimeName, containerId string,
	cgroupName CgroupName, getFullPath func(string) string) (string, error) {
	var toSystemd = func(cgroupName CgroupName) string {
		if len(cgroupName) == 0 || (len(cgroupName) == 1 && cgroupName[0] == "") {
			return "/"
		}
		var newparts []string
		for _, part := range cgroupName {
			part = strings.Replace(part, "-", "_", -1)
			newparts = append(newparts, part)
		}
		return strings.Join(newparts, "-") + ".slice"
	}
	cgroupPath := fmt.Sprintf("%s/%s-%s.scope", cgroupName.ToSystemd(),
		SystemdPathPrefixOfRuntime(runtimeName), containerId)
	fullPath := getFullPath(cgroupPath)
	if !PathIsNotExist(fullPath) {
		return fullPath, nil
	}
	switch runtimeName {
	case "containerd":
		klog.Warningf("CGroup full path <%s> not exist", fullPath)
		cgroupPath = fmt.Sprintf("system.slice/%s.service/%s:%s:%s", runtimeName,
			toSystemd(cgroupName), SystemdPathPrefixOfRuntime(runtimeName), containerId)
		fullPath = getFullPath(cgroupPath)
		if !PathIsNotExist(fullPath) {
			return fullPath, nil
		}
	case "docker":
		klog.Warningf("CGroup full path <%s> not exist", fullPath)
		cgroupPath = fmt.Sprintf("%s/%s", cgroupName.ToSystemd(), containerId)
		fullPath = getFullPath(cgroupPath)
		if !PathIsNotExist(fullPath) {
			return fullPath, nil
		}
	default:
	}
	klog.Infof("Possible upgrade required to adapt container runtime <%s> CGroup driver <%s>",
		runtimeName, "systemd")
	return "", fmt.Errorf("container CGroup full path <%s> not exist", fullPath)
}

func convertCGroupfsFullPath(runtimeName, containerId string,
	cgroupName CgroupName, getFullPath func(string) string) (string, error) {
	fullPath := getFullPath(filepath.Join(cgroupName.ToCgroupfs(), containerId))
	if !PathIsNotExist(fullPath) {
		return fullPath, nil
	}
	fullPath = getFullPath(filepath.Join("system.slice", cgroupName[len(cgroupName)-1]))
	if !PathIsNotExist(fullPath) {
		return fullPath, nil
	}
	klog.Infof("Possible upgrade required to adapt container runtime <%s> CGroup driver <%s>",
		runtimeName, "cgroupfs")
	return "", fmt.Errorf("container CGroup full path <%s> not exist", fullPath)
}

func PathIsNotExist(fullPath string) bool {
	_, err := os.Stat(fullPath)
	return os.IsNotExist(err)
}

func SystemdPathPrefixOfRuntime(runtimeName string) string {
	switch runtimeName {
	case "cri-o":
		return "crio"
	case "containerd":
		return "cri-containerd"
	default:
		if runtimeName != "docker" {
			klog.Warningf("prefix of container runtime %s was not tested. Maybe not correct!", runtimeName)
		}
		return runtimeName
	}
}

func parseRuntime(podContainerId string) (runtimeName string, containerId string) {
	if splits := strings.Split(podContainerId, "://"); len(splits) == 2 {
		runtimeName = splits[0]
		containerId = splits[1]
	}
	return
}

func SetDeviceRulesByCgroupv1(path string, r *configs.Resources) (func() error, error) {
	if r.SkipDevices {
		return NilCloser, nil
	}
	// Generate two emulators, one for the current state of the cgroup and one
	// for the requested state by the user.
	current, err := LoadEmulator(path)
	if err != nil {
		return NilCloser, err
	}
	rules, err := current.Rules()
	if err != nil {
		return NilCloser, err
	}

	exist := func(rules []*devices2.Rule, rule *devices2.Rule) bool {
		for _, ru := range rules {
			if ru != nil && rule != nil &&
				ru.Major == rule.Major &&
				ru.Minor == rule.Minor &&
				ru.Type == rule.Type {
				return true
			}
		}
		return false
	}
	for i, rule := range rules {
		if !exist(r.Devices, rule) {
			r.Devices = append(r.Devices, rules[i])
		}
	}
	target := &devices.Emulator{}
	for _, rule := range r.Devices {
		if err = target.Apply(*rule); err != nil {
			return NilCloser, err
		}
	}

	// Compute the minimal set of transition rules needed to achieve the
	// requested state.
	transitionRules, err := current.Transition(target)
	if err != nil {
		return NilCloser, err
	}

	rollback := func() error {
		if len(rules) > 0 {
			res := &configs.Resources{
				SkipDevices: false,
				Devices:     rules,
			}
			// 重新将旧规则装载回去
			_, err2 := SetDeviceRulesByCgroupv1(path, res)
			if err2 != nil {
				klog.Errorln(err2)
				return fmt.Errorf("failed to call rollback device rules: %w", err)
			}
		}
		return nil
	}

	for _, rule := range transitionRules {
		file := "devices.deny"
		if rule.Allow {
			file = "devices.allow"
		}
		if err := cgroups.WriteFile(path, file, rule.CgroupString()); err != nil {
			return rollback, err
		}
	}

	// Final safety check -- ensure that the resulting state is what was
	// requested. This is only really correct for white-lists, but for
	// black-lists we can at least check that the cgroup is in the right mode.
	//
	// This safety-check is skipped for the unit tests because we cannot
	// currently mock devices.list correctly.

	currentAfter, err := LoadEmulator(path)
	if err != nil {
		return rollback, err
	}
	if !target.IsBlacklist() && !reflect.DeepEqual(currentAfter, target) {
		return rollback, errors.New("resulting devices cgroup doesn't precisely match target")
	} else if target.IsBlacklist() != currentAfter.IsBlacklist() {
		return rollback, errors.New("resulting devices cgroup doesn't match target mode")
	}
	return rollback, nil
}

func LoadEmulator(path string) (*devices.Emulator, error) {
	list, err := cgroups.ReadFile(path, "devices.list")
	if err != nil {
		return nil, err
	}
	return devices.EmulatorFromList(bytes.NewBufferString(list))
}
