package ebpf

import (
	"fmt"
	"testing"

	"github.com/cilium/ebpf/asm"
	"github.com/opencontainers/runc/libcontainer/cgroups/ebpf/devicefilter"
	"github.com/opencontainers/runc/libcontainer/devices"
	_ "github.com/opencontainers/runc/libcontainer/userns"
)

var deviceRules = []*devices.Rule{
	{
		Type:        devices.CharDevice,
		Major:       devices.Wildcard,
		Minor:       devices.Wildcard,
		Permissions: "m",
		Allow:       true,
	},
	{
		Type:        devices.BlockDevice,
		Major:       devices.Wildcard,
		Minor:       devices.Wildcard,
		Permissions: "m",
		Allow:       true,
	},
	{
		Type:        devices.CharDevice,
		Major:       1,
		Minor:       3,
		Permissions: "rwm",
		Allow:       true,
	},
	{
		Type:        devices.CharDevice,
		Major:       1,
		Minor:       8,
		Permissions: "rwm",
		Allow:       true,
	},
	{
		Type:        devices.CharDevice,
		Major:       1,
		Minor:       7,
		Permissions: "rwm",
		Allow:       true,
	},
	{
		Type:        devices.CharDevice,
		Major:       5,
		Minor:       0,
		Permissions: "rwm",
		Allow:       true,
	},
	{
		Type:        devices.CharDevice,
		Major:       1,
		Minor:       5,
		Permissions: "rwm",
		Allow:       true,
	},
	{
		Type:        devices.CharDevice,
		Major:       1,
		Minor:       9,
		Permissions: "rwm",
		Allow:       true,
	},
	{
		Type:        devices.CharDevice,
		Major:       136,
		Minor:       devices.Wildcard,
		Permissions: "rwm",
		Allow:       true,
	},
	{
		Type:        devices.CharDevice,
		Major:       5,
		Minor:       2,
		Permissions: "rwm",
		Allow:       true,
	},
	{
		Type:        devices.CharDevice,
		Major:       10,
		Minor:       200,
		Permissions: "rwm",
		Allow:       true,
	},
}

func Test_AppendRule(t *testing.T) {
	insts, _, _ := devicefilter.DeviceFilter(deviceRules)
	fmt.Println("before", insts)
	instructions, err := LoadInstructions(insts)
	if err != nil {
		t.Error(err)
	}
	fmt.Println("load", asm.Instructions(instructions))
	instructions.AppendRule(&devices.Rule{
		Type:        devices.CharDevice,
		Major:       195,
		Minor:       0,
		Permissions: "rw",
		Allow:       true,
	})
	instructions.AppendRule(&devices.Rule{
		Type:        devices.CharDevice,
		Major:       195,
		Minor:       0,
		Permissions: "rwm",
		Allow:       false,
	})
	instructions.Finalize()
	fmt.Println("after", asm.Instructions(instructions))
}

func Test_Groups(t *testing.T) {
	insts, _, _ := devicefilter.DeviceFilter(deviceRules)
	fmt.Println("before", insts)
	instructions, err := LoadInstructions(insts)
	if err != nil {
		t.Error(err)
	}
	fmt.Println("after", asm.Instructions(instructions))
	for _, inst := range instructions.groups() {
		fmt.Println(inst)
	}

}
