package util

import (
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"runtime"
	"sync"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/devices"
	"golang.org/x/sys/unix"
	ebpf2 "k8s-device-mounter/pkg/util/ebpf"
	"k8s.io/klog/v2"
)

func SetDeviceRulesV2(dirPath string, r *configs.Resources) (func() error, func() error, error) {
	if r.SkipDevices {
		return NilCloser, NilCloser, nil
	}

	dirFD, err := unix.Open(dirPath, unix.O_DIRECTORY|unix.O_RDONLY, 0o600)
	if err != nil {
		return NilCloser, NilCloser, fmt.Errorf("cannot get dir FD for %s", dirPath)
	}
	var closedFd = func() error {
		return unix.Close(dirFD)
	}

	rollback, err := loadAttachCgroupDeviceFilter(dirFD, r.Devices)
	if err != nil {
		if !canSkipEBPFError(r) {
			return closedFd, rollback, err
		}
	}
	return closedFd, rollback, nil
}

// This is similar to the logic applied in crun for handling errors from bpf(2)
// <https://github.com/containers/crun/blob/0.17/src/libcrun/cgroup.c#L2438-L2470>.
func canSkipEBPFError(r *configs.Resources) bool {
	// If we're running in a user namespace we can ignore eBPF rules because we
	// usually cannot use bpf(2), as well as rootless containers usually don't
	// have the necessary privileges to mknod(2) device inodes or access
	// host-level instances (though ideally we would be blocking device access
	// for rootless containers anyway).
	//if userns.RunningInUserNS() {
	//	return true
	//}

	// We cannot ignore an eBPF load error if any rule if is a block rule or it
	// doesn't permit all access modes.
	//
	// NOTE: This will sometimes trigger in cases where access modes are split
	//       between different rules but to handle this correctly would require
	//       using ".../libcontainer/cgroup/devices".Emulator.
	for _, dev := range r.Devices {
		if !dev.Allow || !isRWM(dev.Permissions) {
			return false
		}
	}
	return true
}

func isRWM(perms devices.Permissions) bool {
	var r, w, m bool
	for _, perm := range perms {
		switch perm {
		case 'r':
			r = true
		case 'w':
			w = true
		case 'm':
			m = true
		}
	}
	return r && w && m
}

func findAttachedCgroupDeviceFilters(dirFd int) ([]*ebpf.Program, error) {
	type bpfAttrQuery struct {
		TargetFd    uint32
		AttachType  uint32
		QueryType   uint32
		AttachFlags uint32
		ProgIds     uint64 // __aligned_u64
		ProgCnt     uint32
	}

	// Currently you can only have 64 eBPF programs attached to a cgroup.
	size := 64
	retries := 0
	for retries < 10 {
		progIds := make([]uint32, size)
		query := bpfAttrQuery{
			TargetFd:   uint32(dirFd),
			AttachType: uint32(unix.BPF_CGROUP_DEVICE),
			ProgIds:    uint64(uintptr(unsafe.Pointer(&progIds[0]))),
			ProgCnt:    uint32(len(progIds)),
		}

		// Fetch the list of program ids.
		_, _, errno := unix.Syscall(unix.SYS_BPF,
			uintptr(unix.BPF_PROG_QUERY),
			uintptr(unsafe.Pointer(&query)),
			unsafe.Sizeof(query))
		size = int(query.ProgCnt)
		runtime.KeepAlive(query)
		if errno != 0 {
			// On ENOSPC we get the correct number of programs.
			if errno == unix.ENOSPC {
				retries++
				continue
			}
			return nil, fmt.Errorf("bpf_prog_query(BPF_CGROUP_DEVICE) failed: %w", errno)
		}

		// Convert the ids to program handles.
		progIds = progIds[:size]
		programs := make([]*ebpf.Program, 0, len(progIds))
		for _, progId := range progIds {
			program, err := ebpf.NewProgramFromID(ebpf.ProgramID(progId))
			if err != nil {
				// We skip over programs that give us -EACCES or -EPERM. This
				// is necessary because there may be BPF programs that have
				// been attached (such as with --systemd-cgroup) which have an
				// LSM label that blocks us from interacting with the program.
				//
				// Because additional BPF_CGROUP_DEVICE programs only can add
				// restrictions, there's no real issue with just ignoring these
				// programs (and stops runc from breaking on distributions with
				// very strict SELinux policies).
				if errors.Is(err, os.ErrPermission) {
					log.Printf("ignoring existing CGROUP_DEVICE program (prog_id=%v) which cannot be accessed by runc -- likely due to LSM policy: %v\n", progId, err)
					continue
				}
				return nil, fmt.Errorf("cannot fetch program from id: %w", err)
			}
			programs = append(programs, program)
		}
		runtime.KeepAlive(progIds)
		return programs, nil
	}

	return nil, errors.New("could not get complete list of CGROUP_DEVICE programs")
}

func NilCloser() error {
	return nil
}

func loadAttachCgroupDeviceFilter(dirFd int, rules []*devices.Rule) (func() error, error) {
	// Increase `ulimit -l` limit to avoid BPF_PROG_LOAD error (#2167).
	// This limit is not inherited into the container.
	memlockLimit := &unix.Rlimit{
		Cur: unix.RLIM_INFINITY,
		Max: unix.RLIM_INFINITY,
	}
	_ = unix.Setrlimit(unix.RLIMIT_MEMLOCK, memlockLimit)

	oldProgs, err := findAttachedCgroupDeviceFilters(dirFd)
	if err != nil {
		return NilCloser, err
	}
	if len(oldProgs) == 0 {
		return NilCloser, fmt.Errorf("Device rule ebpf program not found")
	}
	if len(oldProgs) > 1 {
		return NilCloser, fmt.Errorf("Multiple ebpf programs detected, unable to set device permissions")
	}

	info, err := oldProgs[0].Info()
	if err != nil {
		return NilCloser, err
	}

	currentInstructions, err := info.Instructions()
	if err != nil {
		return NilCloser, err
	}
	tmpInstructions, err := ebpf2.LoadInstructions(currentInstructions)
	if err != nil {
		return NilCloser, err
	}
	for i, rule := range rules {
		if rule == nil {
			continue
		}
		if err := tmpInstructions.AppendRule(rules[i]); err != nil {
			return NilCloser, err
		}
	}
	tmpInstructions.Finalize()
	newInstructions := asm.Instructions(tmpInstructions)

	if !reflect.DeepEqual(newInstructions, currentInstructions) {
		klog.V(5).InfoS("The newly generated instruction is equal to the current "+
			"instruction, skip replacement", "current", currentInstructions, "new", newInstructions)
		return NilCloser, nil
	}

	supportReplaceProg := haveBpfProgReplace()

	// If there is only one old program, we can just replace it directly.
	var (
		replaceProg *ebpf.Program
		attachFlags uint32 = unix.BPF_F_ALLOW_MULTI
	)

	if supportReplaceProg {
		replaceProg = oldProgs[0]
		attachFlags |= unix.BPF_F_REPLACE
	}

	spec := &ebpf.ProgramSpec{
		Type:         ebpf.CGroupDevice,
		Instructions: newInstructions,
		License:      "Apache", // TODO 根据runc devicefilter.DeviceFilter() 返回
	}
	prog, err := ebpf.NewProgram(spec)
	if err != nil {
		return NilCloser, err
	}

	err = link.RawAttachProgram(link.RawAttachProgramOptions{
		Target:  dirFd,
		Program: prog,
		Replace: replaceProg,
		Attach:  ebpf.AttachCGroupDevice,
		Flags:   attachFlags,
	})
	if err != nil {
		return NilCloser, fmt.Errorf("failed to call BPF_PROG_ATTACH (BPF_CGROUP_DEVICE, BPF_F_ALLOW_MULTI): %w", err)
	}
	rollback := func() error {
		var rollbackErr error
		var action string
		if supportReplaceProg {
			// 支持替换操作加载旧程序替换新程序
			action = "BPF_F_REPLACE"
			rollbackErr = link.RawAttachProgram(link.RawAttachProgramOptions{
				Target:  dirFd,
				Program: replaceProg,
				Replace: prog,
				Attach:  ebpf.AttachCGroupDevice,
				Flags:   attachFlags,
			})
		} else {
			// 不支持替换的时候直接卸载掉新程序
			action = "BPF_PROG_DETACH"
			rollbackErr = link.RawDetachProgram(link.RawDetachProgramOptions{
				Target:  dirFd,
				Program: prog,
				Attach:  ebpf.AttachCGroupDevice,
			})
		}
		if rollbackErr != nil {
			klog.Errorln(rollbackErr)
			return fmt.Errorf("failed to call rollback %s (BPF_CGROUP_DEVICE): %w", action, err)
		}

		// TODO: Should we attach the old filters back in this case? Otherwise
		//       we fail-open on a security feature, which is a bit scary.
		return nil
	}
	if !supportReplaceProg {
		// 卸载旧的程序
		err = link.RawDetachProgram(link.RawDetachProgramOptions{
			Target:  dirFd,
			Program: oldProgs[0],
			Attach:  ebpf.AttachCGroupDevice,
		})
		if err != nil {
			return rollback, fmt.Errorf("failed to call BPF_PROG_DETACH (BPF_CGROUP_DEVICE) on old filter program: %w", err)
		}
	}
	return rollback, nil
}

var (
	haveBpfProgReplaceBool bool
	haveBpfProgReplaceOnce sync.Once
)

// Loosely based on the BPF_F_REPLACE support check in
// https://github.com/cilium/ebpf/blob/v0.6.0/link/syscalls.go.
//
// TODO: move this logic to cilium/ebpf
func haveBpfProgReplace() bool {
	haveBpfProgReplaceOnce.Do(func() {
		prog, err := ebpf.NewProgram(&ebpf.ProgramSpec{
			Type:    ebpf.CGroupDevice,
			License: "MIT",
			Instructions: asm.Instructions{
				asm.Mov.Imm(asm.R0, 0),
				asm.Return(),
			},
		})
		if err != nil {
			log.Printf("checking for BPF_F_REPLACE support: ebpf.NewProgram failed: %v\n", err)
			return
		}
		defer prog.Close()

		devnull, err := os.Open("/dev/null")
		if err != nil {
			log.Printf("\"checking for BPF_F_REPLACE support: open dummy target fd: %v\n", err)
			return
		}
		defer devnull.Close()

		// We know that we have BPF_PROG_ATTACH since we can load
		// BPF_CGROUP_DEVICE programs. If passing BPF_F_REPLACE gives us EINVAL
		// we know that the feature isn't present.
		err = link.RawAttachProgram(link.RawAttachProgramOptions{
			// We rely on this fd being checked after attachFlags.
			Target: int(devnull.Fd()),
			// Attempt to "replace" bad fds with this program.
			Program: prog,
			Attach:  ebpf.AttachCGroupDevice,
			Flags:   unix.BPF_F_ALLOW_MULTI | unix.BPF_F_REPLACE,
		})
		if errors.Is(err, unix.EINVAL) {
			// not supported
			return
		}
		// attach_flags test succeeded.
		if !errors.Is(err, unix.EBADF) {
			log.Printf("checking for BPF_F_REPLACE: got unexpected (not EBADF or EINVAL) error: %v\n", err)
		}
		haveBpfProgReplaceBool = true
	})
	return haveBpfProgReplaceBool
}
