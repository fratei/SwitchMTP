

// Bridging header for exposing our C shim to Swift.
// This keeps Swift code independent from `module.modulemap`.

#include "../Shim/NxmtpShim.h"
#include <IOKit/IOKitLib.h>
#include <IOKit/usb/IOUSBLib.h>
