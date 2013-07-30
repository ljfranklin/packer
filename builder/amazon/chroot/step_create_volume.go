package chroot

import (
	"fmt"
	"github.com/mitchellh/goamz/ec2"
	"github.com/mitchellh/multistep"
	awscommon "github.com/mitchellh/packer/builder/amazon/common"
	"github.com/mitchellh/packer/packer"
	"log"
)

// StepCreateVolume creates a new volume from the snapshot of the root
// device of the AMI.
//
// Produces:
//   volume_id string - The ID of the created volume
type StepCreateVolume struct {
	volumeId string
}

func (s *StepCreateVolume) Run(state map[string]interface{}) multistep.StepAction {
	ec2conn := state["ec2"].(*ec2.EC2)
	image := state["source_image"].(*ec2.Image)
	instance := state["instance"].(*ec2.Instance)
	ui := state["ui"].(packer.Ui)

	// Determine the root device snapshot
	log.Printf("Searching for root device of the image (%s)", image.RootDeviceName)
	var rootDevice *ec2.BlockDeviceMapping
	for _, device := range image.BlockDevices {
		if device.DeviceName == image.RootDeviceName {
			rootDevice = &device
			break
		}
	}

	if rootDevice == nil {
		err := fmt.Errorf("Couldn't find root device!")
		state["error"] = err
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Say("Creating the root volume...")
	createVolume := &ec2.CreateVolume{
		AvailZone:  instance.AvailZone,
		Size:       rootDevice.VolumeSize,
		SnapshotId: rootDevice.SnapshotId,
		VolumeType: rootDevice.VolumeType,
		IOPS:       rootDevice.IOPS,
	}

	createVolumeResp, err := ec2conn.CreateVolume(createVolume)
	if err != nil {
		err := fmt.Errorf("Error creating root volume: %s", err)
		state["error"] = err
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Set the volume ID so we remember to delete it later
	s.volumeId = createVolumeResp.VolumeId
	log.Printf("Volume ID: %s", s.volumeId)

	// Wait for the volume to become ready
	stateChange := awscommon.StateChangeConf{
		Conn:      ec2conn,
		Pending:   []string{"creating"},
		StepState: state,
		Target:    "available",
		Refresh: func() (interface{}, string, error) {
			resp, err := ec2conn.Volumes([]string{s.volumeId}, ec2.NewFilter())
			if err != nil {
				return nil, "", err
			}

			return nil, resp.Volumes[0].Status, nil
		},
	}

	_, err = awscommon.WaitForState(&stateChange)
	if err != nil {
		err := fmt.Errorf("Error waiting for volume: %s", err)
		state["error"] = err
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *StepCreateVolume) Cleanup(state map[string]interface{}) {
	if s.volumeId == "" {
		return
	}

	ec2conn := state["ec2"].(*ec2.EC2)
	ui := state["ui"].(packer.Ui)

	ui.Say("Deleting the created EBS volume...")
	_, err := ec2conn.DeleteVolume(s.volumeId)
	if err != nil {
		ui.Error(fmt.Sprintf("Error deleting EBS volume: %s", err))
	}
}
