module github.com/fratei/SwitchMTP/backend

go 1.21

require github.com/ganeshrvel/go-mtpfs v0.0.0

require github.com/ganeshrvel/usb v0.0.0-20210103155855-14d96f5ae403 // indirect

// Vendored New BSD sources; see ../THIRD_PARTY.md
replace github.com/ganeshrvel/go-mtpfs => ../third_party/go-mtpfs

replace github.com/ganeshrvel/usb => ../third_party/usb
