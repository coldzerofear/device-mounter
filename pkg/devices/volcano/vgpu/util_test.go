package vgpu

import (
	"fmt"
	"testing"

	uuid2 "github.com/google/uuid"
)

func Test_GetExportEnvCmd(t *testing.T) {
	uuid1 := "GPU-" + uuid2.New().String()
	uuid2 := "GPU-" + uuid2.New().String()
	devMap := map[string]Device{
		uuid1: {
			CtrIdx:    0,
			UUID:      uuid1,
			Type:      "NVIDIA",
			Usedmem:   2000,
			Usedcores: 20,
		},
		uuid2: {
			CtrIdx:    0,
			UUID:      uuid2,
			Type:      "NVIDIA",
			Usedmem:   2000,
			Usedcores: 20,
		},
	}
	envs := GetVGPUEnvs(devMap)
	fmt.Println(envs)
}
