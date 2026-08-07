//go:build !darwin

package mtp

// USBOccupant is a process holding a user client on a USB device.
type USBOccupant struct {
	PID  int
	Name string
}

// SystemPTPDaemon has no equivalent outside macOS.
const SystemPTPDaemon = ""

// FindUSBOccupants is a no-op on non-macOS platforms: exclusive-access
// conflicts there are handled by the kernel driver detach mechanism instead.
func FindUSBOccupants(vendorID, productID int) []USBOccupant { return nil }

// getUSBOccupyingPIDs is a no-op on non-macOS platforms.
func (d *Device) getUSBOccupyingPIDs() string { return "" }
