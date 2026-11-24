package ipmi

import (
	"context"
	"errors"
	"fmt"

	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/command/sensor"
	"github.com/bougou/go-ipmi/pkg/types"
)

// Bridged sensor readings: a sensor whose record names an owner other than
// the BMC (another controller on the IPMB, such as a management engine)
// is read through the BMC with Send Message, as ipmitool 1.8.19 does
// (BRIDGE_TO_SENSOR in include/ipmitool/ipmi_sdr.h; the single-bridge
// encapsulation in src/plugins/lanplus/lanplus.c, ipmi_lanplus_build_v2x_msg):
//
//   Send Message, track request, the owner's channel, carrying
//   [owner][netFn SE<<2 | LUN][checksum][0x81][seq<<2][0x2d][sensor][checksum]
//
// ipmitool takes the owner's reply either embedded in the Send Message
// response or as a second packet that follows it. go-ipmi matches one reply
// to each request, so only the embedded form can be received here; a reply
// that arrives the other way, or not at all, leaves the sensor with no
// reading, which is what fyrtaarn showed before bridging.
//
// Not yet checked on hardware: none of the BMCs fyrtaarn has met so far has
// a sensor owned by another controller (the Quanta has none; the captures
// do not record owners).

const (
	ipmbRemoteSWID = 0x81 // rqSA of a remote console, IPMI 2.0 table 5-4
	netFnSensor    = 0x04
	cmdGetReading  = 0x2d
)

func ipmbChecksum(b []byte) byte {
	var s byte
	for _, x := range b {
		s += x
	}
	return -s
}

// bridgedReadingRequest builds the IPMB message for Get Sensor Reading.
func bridgedReadingRequest(owner, lun, num, seq uint8) []byte {
	h := []byte{owner, netFnSensor<<2 | lun&3}
	m := append(h, ipmbChecksum(h))
	body := []byte{ipmbRemoteSWID, seq << 2, cmdGetReading, num}
	return append(append(m, body...), ipmbChecksum(body))
}

var errNoEmbeddedReply = errors.New("no embedded bridged reply")

// parseBridgedReply checks an embedded IPMB response
// [rqSA][netFn|rqLUN][cs1][rsSA][seq|rsLUN][cmd][ccode][data...][cs2] and
// returns its data.
func parseBridgedReply(b []byte) ([]byte, error) {
	if len(b) < 8 {
		return nil, errNoEmbeddedReply
	}
	if ipmbChecksum(b[:2]) != b[2] || ipmbChecksum(b[3:len(b)-1]) != b[len(b)-1] {
		return nil, fmt.Errorf("bridged reply checksum mismatch")
	}
	if b[1]>>2 != netFnSensor+1 || b[5] != cmdGetReading {
		return nil, fmt.Errorf("bridged reply is not a Get Sensor Reading response")
	}
	if b[6] != 0 {
		return nil, fmt.Errorf("bridged reading completion code %#02x", b[6])
	}
	return b[7 : len(b)-1], nil
}

func bridgedReading(ctx context.Context, c *client.Client, owner types.GeneratorID, num uint8) *sdrReading {
	msg := bridgedReadingRequest(owner.OwnerID(), owner.LUN(), num, 1)
	res, err := c.SendMessage(ctx, owner.ChannelNumber(), false, false, 0x01, msg)
	if err != nil {
		return nil
	}
	data, err := parseBridgedReply(res.Data)
	if err != nil {
		return nil
	}
	r := &sensor.GetSensorReadingResponse{}
	if err := r.Unpack(data); err != nil {
		return nil
	}
	return readingFrom(r)
}
