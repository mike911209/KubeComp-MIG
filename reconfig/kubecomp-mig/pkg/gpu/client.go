package gpu

import (
	"fmt"
	nvlibNvml "github.com/NVIDIA/go-nvml/pkg/nvml"
	nvlibdevice "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
)

type ClientImpl struct {
	nvmlClient  nvlibNvml.Interface
	nvlibClient nvlibdevice.Interface
}

func NewClient() ClientImpl {
	nvmlClient := nvlibNvml.New()
	return ClientImpl{
		nvmlClient:  nvmlClient,
		nvlibClient: nvlibdevice.New(nvlibdevice.WithNvml(nvmlClient), nvlibdevice.WithVerifySymbols(false),),
	}
}

func (c *ClientImpl) init() error {
	if ret := c.nvmlClient.Init(); ret != nvlibNvml.SUCCESS {
		return fmt.Errorf(ret.Error())
	}
	return nil
}

func (c *ClientImpl) shutdown() {
	if ret := c.nvmlClient.Shutdown(); ret != nvlibNvml.SUCCESS {
		fmt.Printf("unable to shut down NVML: %v", ret.Error())
	}
}

// GetMigDeviceGpuIndex returns the index of the GPU associated to the
// MIG device provided as arg. Returns err if the device
// is not found or any error occurs while retrieving it.
func (c *ClientImpl) GetMigDeviceGpuIndex(migDeviceId string) (int, error) {
	if err := c.init(); err != nil {
		return 0, err
	}
	defer c.shutdown()

	// fmt.Printf("retrieving GPU index of MIG device: MIGDeviceUUID %v\n", migDeviceId)
	var result int
	var err error
	var found bool
	err = c.nvlibClient.VisitMigDevices(func(gpuIndex int, _ nvlibdevice.Device, migIndex int, m nvlibdevice.MigDevice) error {
		if found {
			return nil
		}
		uuid, ret := m.GetUUID()
		if ret != nvlibNvml.SUCCESS {
			return fmt.Errorf(
				"error getting UUID of MIG device with index %d on GPU %v: %s",
				migIndex,
				gpuIndex,
				ret.Error(),
			)
		}
		// fmt.Printf("visiting MIG device: GPUIndex %v, migIndex %v, MIGDeviceUUID %v\n", gpuIndex, migIndex, uuid)
		if uuid == migDeviceId {
			result = gpuIndex
			found = true
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("GPU index of MIG device %s not found", migDeviceId)
	}
	return result, nil
}
