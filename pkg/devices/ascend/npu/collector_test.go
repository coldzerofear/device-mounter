package npu

import (
	"testing"

	"Ascend-device-plugin/pkg/common"
	"github.com/stretchr/testify/assert"
	_ "huawei.com/npu-exporter/v6/devmanager"
	_ "huawei.com/npu-exporter/v6/devmanager/dcmi"
)

func Test_GetDeviceListID(t *testing.T) {
	tests := []struct {
		Name           string
		Devices        []string
		RuntimeOptions string
		DevMap         map[int]int // virtDevId: phyDevId
		VisibleDevs    []int
	}{
		{
			Name: "Example 1",
			Devices: []string{
				"Ascend910-4c-116-1_4294967295",
			},
			RuntimeOptions: common.VirtualDev,
			DevMap: map[int]int{
				116: 1,
			},
			VisibleDevs: []int{116},
		},
		{
			Name: "Example 2",
			Devices: []string{
				"Ascend910-0", "Ascend910-1",
			},
			RuntimeOptions: "",
			DevMap:         map[int]int{},
			VisibleDevs:    []int{0, 1},
		},
		{
			Name: "Example 3",
			Devices: []string{
				"Ascend910-4c-100-0_4294967295",
				"Ascend910-4c-116-1_4294967295",
			},
			RuntimeOptions: common.VirtualDev,
			DevMap: map[int]int{
				100: 0, 116: 1,
			},
			VisibleDevs: []int{100, 116},
		},
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			phyDevMapVirtualDev, ascendVisibleDevices, err := common.GetDeviceListID(test.Devices, test.RuntimeOptions)
			if err != nil {
				t.Error(err)
			}
			assert.Equal(t, test.DevMap, phyDevMapVirtualDev)
			assert.Equal(t, test.VisibleDevs, ascendVisibleDevices)
		})
	}
}
