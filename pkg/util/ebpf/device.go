package ebpf

import (
	"fmt"
	"math"

	"github.com/cilium/ebpf/asm"
	"github.com/opencontainers/runc/libcontainer/devices"
	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

type Instructions asm.Instructions

func LoadInstructions(inst asm.Instructions) (Instructions, error) {
	if inst == nil && len(inst) < 2 {
		return nil, fmt.Errorf("asm.Instructions cannot be empty")
	}
	op := asm.OpCode(asm.JumpClass).SetJumpOp(asm.Exit)
	length := len(inst)
	if inst[len(inst)-1].OpCode == op {
		length = len(inst) - 2
	}
	var instructions Instructions
	return append(instructions, inst[0:length]...), nil
}

func (insts *Instructions) AppendRule(rule *devices.Rule) error {
	var bpfType int32
	switch rule.Type {
	case devices.CharDevice:
		bpfType = int32(unix.BPF_DEVCG_DEV_CHAR)
	case devices.BlockDevice:
		bpfType = int32(unix.BPF_DEVCG_DEV_BLOCK)
	default:
		// We do not permit 'a', nor any other types we don't know about.
		return fmt.Errorf("invalid type %q", string(rule.Type))
	}
	if rule.Major > math.MaxUint32 {
		return fmt.Errorf("invalid major %d", rule.Major)
	}
	if rule.Minor > math.MaxUint32 {
		return fmt.Errorf("invalid minor %d", rule.Major)
	}
	hasMajor := rule.Major >= 0 // if not specified in OCI json, major is set to -1
	hasMinor := rule.Minor >= 0
	bpfAccess := int32(0)
	for _, r := range rule.Permissions {
		switch r {
		case 'r':
			bpfAccess |= unix.BPF_DEVCG_ACC_READ
		case 'w':
			bpfAccess |= unix.BPF_DEVCG_ACC_WRITE
		case 'm':
			bpfAccess |= unix.BPF_DEVCG_ACC_MKNOD
		default:
			return fmt.Errorf("unknown device access %v", r)
		}
	}
	// If the access is rwm, skip the check.
	hasAccess := bpfAccess != (unix.BPF_DEVCG_ACC_READ | unix.BPF_DEVCG_ACC_WRITE | unix.BPF_DEVCG_ACC_MKNOD)

	groups := insts.groups()
	index := -1
	for i, group := range groups {
		var (
			foundType  bool
			foundMajor bool
			foundMinor bool
		)
		for _, instruction := range group {
			if !foundType && instruction.OpCode == asm.JNE.Op(asm.ImmSource) &&
				instruction.Dst == asm.R2 && instruction.Constant == int64(bpfType) {
				foundType = true
				continue
			}
			if !foundMajor && instruction.OpCode == asm.JNE.Op(asm.ImmSource) &&
				instruction.Dst == asm.R4 && instruction.Constant == rule.Major {
				foundMajor = true
				continue
			}
			if !foundMinor && instruction.OpCode == asm.JNE.Op(asm.ImmSource) &&
				instruction.Dst == asm.R5 && instruction.Constant == rule.Minor {
				foundMinor = true
				continue
			}
		}
		if foundType && foundMajor && foundMinor {
			index = i
			break
		}
	}

	//var (
	//	blockSym         = "block-" + strconv.Itoa(blockID)
	//	nextBlockSym     = "block-" + strconv.Itoa(blockID+1)
	//	prevBlockLastIdx = len(*insts) - 1
	//)

	//5: JNEImm dst: r2 off: 7 imm: 2
	//6: Mov32Reg dst: r6 src: r3
	//7: And32Imm dst: r6 imm: 6
	//8: JNEReg dst: r6 off: 4 src: r3
	//9: JNEImm dst: r4 off: 3 imm: 195
	//10: JNEImm dst: r5 off: 2 imm: 0
	//11: Mov32Imm dst: r0 imm: 1

	var insert Instructions
	insert = append(insert,
		// if (R2 != bpfType) goto next
		func() asm.Instruction {
			imm := asm.JNE.Imm(asm.R2, bpfType, "")
			imm.Offset = 7
			return imm
		}(),
	)
	if hasAccess {
		insert = append(insert,
			// if (R3 & bpfAccess != R3 /* use R1 as a temp var */) goto next
			asm.Mov.Reg32(asm.R6, asm.R3),
			asm.And.Imm32(asm.R6, bpfAccess),
			func() asm.Instruction {
				reg := asm.JNE.Reg(asm.R6, asm.R3, "")
				reg.Offset = 4
				return reg
			}(),
			//asm.Mov.Reg32(asm.R1, asm.R3),
			//asm.And.Imm32(asm.R1, bpfAccess),
			//func() asm.Instruction {
			//	reg := asm.JNE.Reg(asm.R1, asm.R3, "")
			//	reg.Offset = 4
			//	return reg
			//}(),
		)
	}
	if hasMajor {
		insert = append(insert,
			// if (R4 != major) goto next
			func() asm.Instruction {
				imm := asm.JNE.Imm(asm.R4, int32(rule.Major), "")
				imm.Offset = 3
				return imm
			}(),
		)
	}
	if hasMinor {
		insert = append(insert,
			// if (R5 != minor) goto next
			func() asm.Instruction {
				imm := asm.JNE.Imm(asm.R5, int32(rule.Minor), "")
				imm.Offset = 2
				return imm
			}(),
		)
	}
	insert = append(insert, acceptBlock(rule.Allow)...)

	// TODO index 0 包含了其他的一些指令，直接替换将报错：load program: permission denied: 0: (55) if r2 != 0x2 goto pc+7: R2 !read_ok (1 line(s) omitted)
	// 所以如果规则处于index 0上则直接追加，epf 字节指令规则之后的将覆盖之前的
	if index <= 0 {
		klog.V(5).Infoln("insert asm.Instructions index", len(groups))
		groups = append(groups, insert)
	} else {
		klog.V(5).Infoln("update asm.Instructions index", index)
		groups[index] = insert
	}
	*insts = Instructions{}
	for i, _ := range groups {
		*insts = append(*insts, groups[i]...)
	}
	// set blockSym to the first instruction we added in this iteration
	//(*insts)[prevBlockLastIdx+1] = (*insts)[prevBlockLastIdx+1].Sym(blockSym)

	return nil
}

func (insts *Instructions) groups() []Instructions {
	var instGroups []Instructions
	var tmp Instructions
	op := asm.OpCode(asm.JumpClass).SetJumpOp(asm.Exit)
	for _, inst := range *insts {
		tmp = append(tmp, inst)
		if inst.OpCode == op {
			instGroups = append(instGroups, tmp)
			tmp = nil
		}
	}
	if tmp != nil {
		instGroups = append(instGroups, tmp)
	}
	return instGroups
}

func (insts *Instructions) Finalize() {
	*insts = append(*insts,
		// R0 <- v
		asm.Mov.Imm32(asm.R0, 0).Sym(""),
		asm.Return(),
	)
}

func acceptBlock(accept bool) asm.Instructions {
	var v int32
	if accept {
		v = 1
	}
	return []asm.Instruction{
		// R0 <- v
		asm.Mov.Imm32(asm.R0, v),
		asm.Return(),
	}
}
