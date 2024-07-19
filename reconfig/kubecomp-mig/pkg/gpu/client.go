package gpu

import (
	"fmt"
	"strings"

	nvlibdevice "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	nvlibNvml "github.com/NVIDIA/go-nvml/pkg/nvml"
)

type ClientImpl struct {
	nvmlClient  nvlibNvml.Interface
	nvlibClient nvlibdevice.Interface
}

type MigInfo struct {
	DeviceID string
	GpuID    int
	Profile  string
}

func NewClient() ClientImpl {
	nvmlClient := nvlibNvml.New()
	return ClientImpl{
		nvmlClient:  nvmlClient,
		nvlibClient: nvlibdevice.New(nvlibdevice.WithNvml(nvmlClient), nvlibdevice.WithVerifySymbols(false)),
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

func extract_profile(name string) string {
	// extract the last part
	parts := strings.Split(name, " ")

	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	fmt.Printf("Failed to extract the profile. Device Name: %v\n", name)
	return ""
}

func (c *ClientImpl) GetAllMigs() (map[string]MigInfo, error) {
	mig_info := make(map[string]MigInfo)

	if err := c.init(); err != nil {
		return mig_info, err
	}
	defer c.shutdown()

	err := c.nvlibClient.VisitDevices(func(i int, d nvlibdevice.Device) error {
		migs, err := d.GetMigDevices()
		if err != nil {
			fmt.Printf("error: %v\n", err)
		} else {
			for _, m := range migs {
				name, _ := m.GetName()
				uuid, _ := m.GetUUID()
				mig_info[uuid] = MigInfo{
					DeviceID: uuid,
					GpuID:    i,
					Profile:  extract_profile(name),
				}
			}
		}
		return nil
	})
	if err != nil {
		return mig_info, err
	}
	return mig_info, nil
}
