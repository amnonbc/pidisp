// Package drm drives a display via the Linux DRM/KMS kernel subsystem using
// raw ioctls — no libdrm or CGO required.
//
// # Why DRM instead of fbdev
//
// The simpler [fb] package writes to /dev/fb0 and works on any Linux system,
// but it has two unavoidable costs: a per-pixel format conversion (RGBA →
// RGB565 or XRGB8888) and, when rotating, a per-pixel address calculation.
// On a Raspberry Pi 2 at 800×480 this takes ~53 ms per frame for 16 bpp.
//
// DRM eliminates both costs:
//
//  1. ABGR8888 pixel format (DRM_FORMAT_ABGR8888, fourcc "AB24") matches Go's
//     image.RGBA memory layout byte-for-byte, so Blit is a plain memcopy — no
//     channel swapping or bit-shifting per pixel.
//
//  2. The display controller's plane "rotation" property (DRM_MODE_ROTATE_180)
//     offloads 180° rotation to hardware, making the rotated and non-rotated
//     blit paths identical.
//
// The result on Pi 2 is ~1.66 ms per frame — a 32× improvement over the fbdev
// baseline — at the cost of a more involved setup sequence.
//
// # Setup complexity
//
// Where fbdev needs a single ioctl and an mmap, DRM requires enumerating
// connectors and CRTCs, allocating a dumb buffer, registering a framebuffer
// with ADDFB2, setting the CRTC, and configuring the plane rotation property.
// All of this is handled inside Open; Blit itself is trivial.
//
// DRM also requires DRM master, meaning the process must run on the Linux
// console with no display server active. Open returns an error if this
// condition is not met, and the caller falls back to fbdev.
//
// # Hardware support
//
// Tested on Raspberry Pi 2 with the vc4-kms-v3d overlay enabled in
// /boot/config.txt. Should work on any Linux DRM modesetting device.
//
// # References
//
//   - Linux DRM UAPI: https://www.kernel.org/doc/html/latest/gpu/drm-uapi.html
//   - Kernel headers: include/uapi/drm/drm.h, include/uapi/drm/drm_mode.h
//   - Fourcc codes: include/uapi/drm/drm_fourcc.h
package drm

import (
	"bytes"
	"fmt"
	"image"
	"log/slog"
	"os"
	"syscall"
	"unsafe"
)

// drmIoWR builds a DRM ioctl request number for a read+write ioctl.
// Encoding: bits 31-30 = direction (3=R+W), bits 29-16 = struct size,
// bits 15-8 = type ('d'=0x64 for DRM), bits 7-0 = command number.
func drmIoWR[T any](nr uintptr) uintptr {
	return (3 << 30) | (unsafe.Sizeof(*new(T)) << 16) | (0x64 << 8) | nr
}

// drmIoW builds a DRM ioctl request number for a write-only ioctl (direction=1).
func drmIoW[T any](nr uintptr) uintptr {
	return (1 << 30) | (unsafe.Sizeof(*new(T)) << 16) | (0x64 << 8) | nr
}

// The following structs mirror the Linux kernel UAPI headers in <drm/drm_mode.h>.
// All fields use fixed-width types so the layout matches on 32- and 64-bit ARM.

// drmModeRes mirrors struct drm_mode_card_res.
type drmModeRes struct {
	FbIDPtr        uint64
	CrtcIDPtr      uint64
	ConnectorIDPtr uint64
	EncoderIDPtr   uint64
	CountFbs       uint32
	CountCrtcs     uint32
	CountConnectors uint32
	CountEncoders  uint32
	MinWidth       uint32
	MaxWidth       uint32
	MinHeight      uint32
	MaxHeight      uint32
}

// drmModeGetConnector mirrors struct drm_mode_get_connector.
type drmModeGetConnector struct {
	EncodersPtr    uint64
	ModesPtr       uint64
	PropsPtr       uint64
	PropValuesPtr  uint64
	CountModes     uint32
	CountProps     uint32
	CountEncoders  uint32
	EncoderID      uint32
	ConnectorID    uint32
	ConnectorType  uint32
	ConnectorTypeID uint32
	Connection     uint32
	MmWidth        uint32
	MmHeight       uint32
	Subpixel       uint32
	Pad            uint32
}

// drmModeModeInfo mirrors struct drm_mode_modeinfo.
type drmModeModeInfo struct {
	Clock      uint32
	Hdisplay   uint16
	HsyncStart uint16
	HsyncEnd   uint16
	Htotal     uint16
	Hskew      uint16
	Vdisplay   uint16
	VsyncStart uint16
	VsyncEnd   uint16
	Vtotal     uint16
	Vscan      uint16
	VRefresh   uint32
	Flags      uint32
	Type       uint32
	Name       [32]byte
}

// drmModeGetEncoder mirrors struct drm_mode_get_encoder.
type drmModeGetEncoder struct {
	EncoderID      uint32
	EncoderType    uint32
	CrtcID         uint32
	PossibleCrtcs  uint32
	PossibleClones uint32
}

// drmModeCreateDumb mirrors struct drm_mode_create_dumb.
type drmModeCreateDumb struct {
	Height uint32
	Width  uint32
	Bpp    uint32
	Flags  uint32
	Handle uint32
	Pitch  uint32
	Size   uint64
}

// drmModeMapDumb mirrors struct drm_mode_map_dumb.
type drmModeMapDumb struct {
	Handle uint32
	Pad    uint32
	Offset uint64
}

// drmModeFBCmd2 mirrors struct drm_mode_fb_cmd2 (ADDFB2, supports explicit fourcc).
// The 4-byte padding before Modifier matches the C struct's alignment of __u64.
type drmModeFBCmd2 struct {
	FbID        uint32
	Width       uint32
	Height      uint32
	PixelFormat uint32
	Flags       uint32
	Handles     [4]uint32
	Pitches     [4]uint32
	Offsets     [4]uint32
	_           [4]byte // padding: offsets ends at byte 68, modifier needs 8-byte alignment
	Modifier    [4]uint64
}

// drmFormatABGR8888 is DRM_FORMAT_ABGR8888 = fourcc_code('A','B','2','4').
// Memory layout: byte[0]=R, byte[1]=G, byte[2]=B, byte[3]=A — matches image.RGBA.Pix.
const drmFormatABGR8888 = uint32('A') | uint32('B')<<8 | uint32('2')<<16 | uint32('4')<<24

// drmModeCrtc mirrors struct drm_mode_crtc.
type drmModeCrtc struct {
	SetConnectorsPtr uint64
	CountConnectors  uint32
	CrtcID           uint32
	FbID             uint32
	X                uint32
	Y                uint32
	GammaSize        uint32
	ModeValid        uint32
	Mode             drmModeModeInfo
}

// drmModeDestroyDumb mirrors struct drm_mode_destroy_dumb.
type drmModeDestroyDumb struct {
	Handle uint32
}

// drmSetClientCap mirrors struct drm_set_client_cap (from drm.h).
type drmSetClientCap struct {
	Capability uint64
	Value      uint64
}

// drmModeGetPlaneRes mirrors struct drm_mode_get_plane_res.
type drmModeGetPlaneRes struct {
	PlaneIDPtr  uint64
	CountPlanes uint32
}

// drmModeGetPlane mirrors struct drm_mode_get_plane.
type drmModeGetPlane struct {
	PlaneID        uint32
	CrtcID         uint32
	FbID           uint32
	PossibleCrtcs  uint32
	GammaSize      uint32
	CountFormatTypes uint32
	FormatTypePtr  uint64
}

// drmModeObjSetProperty mirrors struct drm_mode_obj_set_property.
type drmModeObjSetProperty struct {
	Value   uint64
	PropID  uint32
	ObjID   uint32
	ObjType uint32
	Pad     uint32
}

// drmModeObjGetProperties mirrors struct drm_mode_obj_get_properties.
type drmModeObjGetProperties struct {
	PropsPtr      uint64
	PropValuesPtr uint64
	CountProps    uint32
	ObjID         uint32
	ObjType       uint32
	Pad           uint32
}

// drmModeGetProperty mirrors struct drm_mode_get_property.
// Name is a fixed 32-byte field (DRM_PROP_NAME_LEN).
type drmModeGetProperty struct {
	ValuesPtr     uint64
	EnumBlobPtr   uint64
	PropID        uint32
	Flags         uint32
	Name          [32]byte
	CountValues   uint32
	CountEnumBlobs uint32
}

// drmClientCapUniversalPlanes requests visibility of primary and cursor planes
// in addition to overlay planes (from drm.h: DRM_CLIENT_CAP_UNIVERSAL_PLANES=2).
const drmClientCapUniversalPlanes = 2

// drmModeObjectPlane is the DRM object type for planes (from drm_mode.h).
const drmModeObjectPlane = 0xeeeeeeee

// drmModeRotate0 and drmModeRotate180 are values for the DRM "rotation"
// plane property (from drm_mode.h: DRM_MODE_ROTATE_0=bit0, DRM_MODE_ROTATE_180=bit2).
const (
	drmModeRotate0   = uint64(1 << 0)
	drmModeRotate180 = uint64(1 << 2)
)

// ioctl request numbers, computed from struct sizes at init time.
var (
	ioctlSetClientCap        = drmIoW[drmSetClientCap](0x0D)
	ioctlModeGetResources    = drmIoWR[drmModeRes](0xA0)
	ioctlModeGetConnector    = drmIoWR[drmModeGetConnector](0xA7)
	ioctlModeGetEncoder      = drmIoWR[drmModeGetEncoder](0xA6)
	ioctlModeCreateDumb      = drmIoWR[drmModeCreateDumb](0xB2)
	ioctlModeMapDumb         = drmIoWR[drmModeMapDumb](0xB3)
	ioctlModeAddFB2          = drmIoWR[drmModeFBCmd2](0xB8)
	ioctlModeSetCRTC         = drmIoWR[drmModeCrtc](0xA2)
	ioctlModeDestroyDumb     = drmIoWR[drmModeDestroyDumb](0xB4)
	ioctlModeGetPlaneRes     = drmIoWR[drmModeGetPlaneRes](0xB5)
	ioctlModeGetPlane        = drmIoWR[drmModeGetPlane](0xB6)
	ioctlModeObjGetProperties = drmIoWR[drmModeObjGetProperties](0xB9)
	ioctlModeObjSetProperty  = drmIoWR[drmModeObjSetProperty](0xBA)
	ioctlModeGetProperty     = drmIoWR[drmModeGetProperty](0xAA)
)

func drmIoctl[T any](fd uintptr, req uintptr, arg *T) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(arg)))
	if errno != 0 {
		return errno
	}
	return nil
}

// Device represents an open DRM display device.
type Device struct {
	file   *os.File
	fd     uintptr
	width  int
	height int
	stride int
	handle uint32
	data   []byte
}

// Width returns the display width in pixels.
func (d *Device) Width() int { return d.width }

// Height returns the display height in pixels.
func (d *Device) Height() int { return d.height }

// fourccToString converts a DRM fourcc format code to its 4-character ASCII name.
func fourccToString(f uint32) string {
	return string([]byte{byte(f), byte(f >> 8), byte(f >> 16), byte(f >> 24)})
}

// logPlaneFormats logs the fourcc pixel formats supported by each DRM plane on fd.
// It first sets DRM_CLIENT_CAP_UNIVERSAL_PLANES so that primary and cursor
// planes are included alongside overlay planes.
func logPlaneFormats(fd uintptr) {
	cap := drmSetClientCap{Capability: drmClientCapUniversalPlanes, Value: 1}
	err := drmIoctl(fd, ioctlSetClientCap, &cap)
	if err != nil {
		slog.Warn("DRM_IOCTL_SET_CLIENT_CAP universal_planes", "err", err)
	}

	var res drmModeGetPlaneRes
	err = drmIoctl(fd, ioctlModeGetPlaneRes, &res)
	if err != nil {
		slog.Warn("DRM_IOCTL_MODE_GETPLANERESOURCES", "err", err)
		return
	}
	if res.CountPlanes == 0 {
		slog.Info("DRM planes: none reported")
		return
	}

	planeIDs := make([]uint32, res.CountPlanes)
	res.PlaneIDPtr = uint64(uintptr(unsafe.Pointer(&planeIDs[0])))
	err = drmIoctl(fd, ioctlModeGetPlaneRes, &res)
	if err != nil {
		slog.Warn("DRM_IOCTL_MODE_GETPLANERESOURCES (ids)", "err", err)
		return
	}

	for _, id := range planeIDs {
		plane := drmModeGetPlane{PlaneID: id}
		err = drmIoctl(fd, ioctlModeGetPlane, &plane)
		if err != nil || plane.CountFormatTypes == 0 {
			continue
		}
		formats := make([]uint32, plane.CountFormatTypes)
		plane.FormatTypePtr = uint64(uintptr(unsafe.Pointer(&formats[0])))
		err = drmIoctl(fd, ioctlModeGetPlane, &plane)
		if err != nil {
			continue
		}
		names := make([]string, len(formats))
		for i, f := range formats {
			names[i] = fourccToString(f)
		}
		rotation := planeRotationProperty(fd, id)
		slog.Info("DRM plane", "id", id, "possible_crtcs", fmt.Sprintf("0x%x", plane.PossibleCrtcs), "formats", names, "rotation_prop", rotation)
	}
}

// LogPlaneFormats opens dev and logs the fourcc pixel formats supported by
// each DRM plane. Useful for probing what hardware pixel formats are available.
func LogPlaneFormats(dev string) {
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		slog.Warn("LogPlaneFormats: open", "dev", dev, "err", err)
		return
	}
	defer f.Close()
	logPlaneFormats(f.Fd())
}

// findPropID returns the property ID of the named property on the given plane,
// its current value, and whether it was found.
func findPropID(fd uintptr, planeID uint32, name string) (propID uint32, value uint64, found bool) {
	obj := drmModeObjGetProperties{ObjID: planeID, ObjType: drmModeObjectPlane}
	err := drmIoctl(fd, ioctlModeObjGetProperties, &obj)
	if err != nil || obj.CountProps == 0 {
		return 0, 0, false
	}

	propIDs := make([]uint32, obj.CountProps)
	propVals := make([]uint64, obj.CountProps)
	obj.PropsPtr = uint64(uintptr(unsafe.Pointer(&propIDs[0])))
	obj.PropValuesPtr = uint64(uintptr(unsafe.Pointer(&propVals[0])))
	err = drmIoctl(fd, ioctlModeObjGetProperties, &obj)
	if err != nil {
		return 0, 0, false
	}

	for i, pid := range propIDs {
		prop := drmModeGetProperty{PropID: pid}
		err = drmIoctl(fd, ioctlModeGetProperty, &prop)
		if err != nil {
			continue
		}
		if nullTermString(prop.Name[:]) == name {
			return pid, propVals[i], true
		}
	}
	return 0, 0, false
}

// planeRotationProperty returns a description of the "rotation" property on
// the given plane, or "none" if the driver does not expose one.
func planeRotationProperty(fd uintptr, planeID uint32) string {
	pid, value, ok := findPropID(fd, planeID, "rotation")
	if !ok {
		return "none"
	}
	return fmt.Sprintf("prop_id=%d value=0x%x", pid, value)
}

// nullTermString returns the string up to the first null byte in b.
func nullTermString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// setPlaneRotation finds the primary plane for crtcID and sets its DRM
// "rotation" property to DRM_MODE_ROTATE_180 or DRM_MODE_ROTATE_0.
// crtcIDs is the ordered list from DRM_IOCTL_MODE_GETRESOURCES; the index
// of crtcID in that list determines which bit to match in PossibleCrtcs.
func setPlaneRotation(fd uintptr, crtcID uint32, crtcIDs []uint32, rotate bool) {
	crtcBit := uint32(0)
	for i, id := range crtcIDs {
		if id == crtcID {
			crtcBit = 1 << uint(i)
			break
		}
	}
	if crtcBit == 0 {
		slog.Warn("DRM setPlaneRotation: crtcID not in resource list", "crtcID", crtcID)
		return
	}

	var planeRes drmModeGetPlaneRes
	err := drmIoctl(fd, ioctlModeGetPlaneRes, &planeRes)
	if err != nil || planeRes.CountPlanes == 0 {
		return
	}

	planeIDs := make([]uint32, planeRes.CountPlanes)
	planeRes.PlaneIDPtr = uint64(uintptr(unsafe.Pointer(&planeIDs[0])))
	err = drmIoctl(fd, ioctlModeGetPlaneRes, &planeRes)
	if err != nil {
		return
	}

	planeID := uint32(0)
	for _, id := range planeIDs {
		p := drmModeGetPlane{PlaneID: id}
		err = drmIoctl(fd, ioctlModeGetPlane, &p)
		if err != nil {
			continue
		}
		if p.PossibleCrtcs&crtcBit != 0 {
			planeID = id
			break
		}
	}
	if planeID == 0 {
		slog.Warn("DRM setPlaneRotation: no plane found for CRTC", "crtcID", crtcID)
		return
	}

	rotPropID, _, ok := findPropID(fd, planeID, "rotation")
	if !ok {
		slog.Warn("DRM setPlaneRotation: rotation property not found", "planeID", planeID)
		return
	}

	value := drmModeRotate0
	if rotate {
		value = drmModeRotate180
	}
	set := drmModeObjSetProperty{Value: value, PropID: rotPropID, ObjID: planeID, ObjType: drmModeObjectPlane}
	err = drmIoctl(fd, ioctlModeObjSetProperty, &set)
	if err != nil {
		slog.Warn("DRM_IOCTL_MODE_OBJ_SETPROPERTY rotation", "err", err)
		return
	}
	slog.Info("DRM plane rotation set", "planeID", planeID, "rotate180", rotate)
}

// Open opens the DRM device at dev, finds the first connected connector,
// creates a 32 bpp dumb buffer in ABGR8888 format, registers it as a
// framebuffer, sets the CRTC, and configures hardware rotation if requested.
func Open(dev string, rotate bool) (*Device, error) {
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dev, err)
	}
	fd := f.Fd()

	// Enable universal planes so that setPlaneRotation can enumerate the
	// primary plane. Without this, only overlay planes are visible and
	// rotation would be applied to the wrong plane.
	cap := drmSetClientCap{Capability: drmClientCapUniversalPlanes, Value: 1}
	err = drmIoctl(fd, ioctlSetClientCap, &cap)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("DRM_CLIENT_CAP_UNIVERSAL_PLANES: %w", err)
	}

	// First call: discover how many connectors exist.
	var res drmModeRes
	err = drmIoctl(fd, ioctlModeGetResources, &res)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("DRM_IOCTL_MODE_GETRESOURCES: %w", err)
	}
	if res.CountConnectors == 0 {
		f.Close()
		return nil, fmt.Errorf("no DRM connectors found")
	}

	// Second call: fetch connector and CRTC ID lists. Zero out fbs and encoders
	// so the kernel doesn't write them to address 0 (which would return EFAULT).
	connectorIDs := make([]uint32, res.CountConnectors)
	crtcIDs := make([]uint32, res.CountCrtcs)
	res.ConnectorIDPtr = uint64(uintptr(unsafe.Pointer(&connectorIDs[0])))
	res.CrtcIDPtr = uint64(uintptr(unsafe.Pointer(&crtcIDs[0])))
	res.CountFbs = 0
	res.FbIDPtr = 0
	res.CountEncoders = 0
	res.EncoderIDPtr = 0
	err = drmIoctl(fd, ioctlModeGetResources, &res)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("DRM_IOCTL_MODE_GETRESOURCES (ids): %w", err)
	}

	// Find the first connector that has at least one mode.
	// We check CountModes rather than the Connection field because some drivers
	// (including VC4 on the Pi) report DRM_MODE_UNKNOWNCONNECTION even when a
	// display is physically attached; CountModes > 0 is the reliable indicator.
	var connID uint32
	var encoderID uint32
	var mode drmModeModeInfo
	found := false
	for _, id := range connectorIDs {
		conn := drmModeGetConnector{ConnectorID: id}
		err = drmIoctl(fd, ioctlModeGetConnector, &conn)
		if err != nil || conn.CountModes == 0 {
			continue
		}

		// Fetch only the mode list. Zero out props and encoders so the kernel
		// doesn't attempt to write them to address 0 (EFAULT).
		modes := make([]drmModeModeInfo, conn.CountModes)
		conn.ModesPtr = uint64(uintptr(unsafe.Pointer(&modes[0])))
		conn.CountProps = 0
		conn.PropsPtr = 0
		conn.PropValuesPtr = 0
		conn.CountEncoders = 0
		conn.EncodersPtr = 0
		err = drmIoctl(fd, ioctlModeGetConnector, &conn)
		if err != nil {
			continue
		}
		connID = id
		encoderID = conn.EncoderID
		mode = modes[0]
		found = true
		break
	}
	if !found {
		f.Close()
		return nil, fmt.Errorf("no DRM connector with modes found")
	}

	// Get the CRTC currently bound to this connector's encoder.
	var enc drmModeGetEncoder
	enc.EncoderID = encoderID
	err = drmIoctl(fd, ioctlModeGetEncoder, &enc)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("DRM_IOCTL_MODE_GETENCODER: %w", err)
	}
	crtcID := enc.CrtcID

	width := int(mode.Hdisplay)
	height := int(mode.Vdisplay)

	// Create a 32 bpp dumb buffer. The kernel fills in Handle, Pitch, and Size.
	var dumb drmModeCreateDumb
	dumb.Width = uint32(width)
	dumb.Height = uint32(height)
	dumb.Bpp = 32
	err = drmIoctl(fd, ioctlModeCreateDumb, &dumb)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("DRM_IOCTL_MODE_CREATE_DUMB: %w", err)
	}

	// Register it as a DRM framebuffer using ADDFB2 with explicit ABGR8888 fourcc.
	// ABGR8888 matches Go's image.RGBA memory layout so no pixel conversion is needed.
	var fb drmModeFBCmd2
	fb.Width = uint32(width)
	fb.Height = uint32(height)
	fb.PixelFormat = drmFormatABGR8888
	fb.Handles[0] = dumb.Handle
	fb.Pitches[0] = dumb.Pitch
	err = drmIoctl(fd, ioctlModeAddFB2, &fb)
	if err != nil {
		destroy := drmModeDestroyDumb{Handle: dumb.Handle}
		drmIoctl(fd, ioctlModeDestroyDumb, &destroy)
		f.Close()
		return nil, fmt.Errorf("DRM_IOCTL_MODE_ADDFB2: %w", err)
	}

	// Get the mmap offset for the dumb buffer.
	var mapDumb drmModeMapDumb
	mapDumb.Handle = dumb.Handle
	err = drmIoctl(fd, ioctlModeMapDumb, &mapDumb)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("DRM_IOCTL_MODE_MAP_DUMB: %w", err)
	}

	data, err := syscall.Mmap(
		int(fd),
		int64(mapDumb.Offset),
		int(dumb.Size),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("mmap dumb buffer: %w", err)
	}

	// Point the CRTC at our new framebuffer.
	crtc := drmModeCrtc{
		CrtcID:           crtcID,
		FbID:             fb.FbID,
		ModeValid:        1,
		Mode:             mode,
		SetConnectorsPtr: uint64(uintptr(unsafe.Pointer(&connID))),
		CountConnectors:  1,
	}
	err = drmIoctl(fd, ioctlModeSetCRTC, &crtc)
	if err != nil {
		syscall.Munmap(data)
		f.Close()
		return nil, fmt.Errorf("DRM_IOCTL_MODE_SETCRTC: %w", err)
	}

	setPlaneRotation(fd, crtcID, crtcIDs, rotate)

	slog.Info("DRM display", "width", width, "height", height, "stride", dumb.Pitch, "crtc", crtcID)
	return &Device{
		file:   f,
		fd:     fd,
		width:  width,
		height: height,
		stride: int(dumb.Pitch),
		handle: dumb.Handle,
		data:   data,
	}, nil
}

// Close unmaps the dumb buffer and releases the DRM device file.
func (d *Device) Close() {
	syscall.Munmap(d.data)
	destroy := drmModeDestroyDumb{Handle: d.handle}
	drmIoctl(d.fd, ioctlModeDestroyDumb, &destroy)
	d.file.Close()
}

// Blit copies img to the DRM dumb buffer.
//
// ABGR8888 matches image.RGBA.Pix exactly, so no pixel conversion is needed.
// When strides match the whole frame is a single copy. Rotation is handled
// by the hardware via the plane "rotation" property set at Open time.
func (d *Device) Blit(img *image.RGBA) {
	if img.Stride == d.stride {
		copy(d.data, img.Pix[:d.height*d.stride])
		return
	}
	rowBytes := d.width * 4
	for y := 0; y < d.height; y++ {
		src := img.Pix[y*img.Stride : y*img.Stride+rowBytes]
		dst := d.data[y*d.stride : y*d.stride+rowBytes]
		copy(dst, src)
	}
}
