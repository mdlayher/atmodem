package atmodem

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tarm/serial"
	atdevice "github.com/warthog618/modem/at"
)

// A Device is a modem which communicates using AT commands.
type Device struct {
	rw io.ReadWriter
	d  *atdevice.AT
}

// Dial dials a serial connection to a modem with the specified port, baud rate,
// and timeout.
func Dial(port string, baud int, timeout time.Duration) (*Device, error) {
	p, err := serial.OpenPort(&serial.Config{
		Name:        port,
		Baud:        baud,
		ReadTimeout: timeout,
	})
	if err != nil {
		return nil, err
	}

	return Open(p, timeout)
}

// Open opens a connection to a modem using an existing io.ReadWriter. If rw
// also implements io.Closer, its Close method will be called on Device.Close.
func Open(rw io.ReadWriter, timeout time.Duration) (*Device, error) {
	d := atdevice.New(rw, atdevice.WithTimeout(timeout))
	if err := d.Init(); err != nil {
		return nil, err
	}

	return &Device{
		rw: rw,
		d:  d,
	}, nil
}

// Close closes the underlying io.ReadWriter if it also implements io.Closer,
// or is a no-op otherwise.
func (d *Device) Close() error {
	c, ok := d.rw.(io.Closer)
	if !ok {
		return nil
	}

	return c.Close()
}

// Info contains device information about a modem.
type Info struct {
	Manufacturer, Model, Revision, IMEI, MEID, FSN string
	IMEISV                                         int
	GCAP                                           []string
}

// Info requests device information from the modem.
func (d *Device) Info() (*Info, error) {
	ss, err := d.d.Command("I")
	if err != nil {
		return nil, err
	}
	if len(ss) == 0 {
		return nil, errors.New("atmodem: empty info response from modem")
	}

	return parseInfo(ss)
}

// parseInfo unpacks an Info structure from a modem response.
func parseInfo(lines []string) (*Info, error) {
	var i Info
	for _, l := range lines {
		// Each line is prefixed with the field name and a colon.
		ss := strings.SplitN(l, ":", 2)
		if len(ss) != 2 {
			return nil, fmt.Errorf("atmodem: malformed info line: %q", l)
		}

		vp, err := newValueParser([]string{ss[1]})
		if err != nil {
			return nil, err
		}

		switch ss[0] {
		case "Manufacturer":
			i.Manufacturer = vp.String()
		case "Model":
			i.Model = vp.String()
		case "Revision":
			i.Revision = vp.String()
		case "IMEI":
			i.IMEI = vp.String()
		case "MEID":
			i.MEID = vp.String()
		case "IMEI SV":
			i.IMEISV = vp.Int()
		case "FSN":
			i.FSN = vp.String()
		case "+GCAP":
			i.GCAP = strings.Split(vp.String(), ",")
		}

		if err := vp.Err(); err != nil {
			return nil, err
		}
	}

	return &i, nil
}

// Status contains the modem's current radio status.
type Status struct {
	CurrentTime                                 time.Duration
	Temperature, ResetCounter                   int
	Mode, SystemMode, PSState, LTEBand          string
	LTEBandwidthMHz                             float64
	LTEReceiveChannel, LTETransmitChannel       int
	LTECAState, EMMState, RRCState, IMSRegState string
	PCCRXMRSSI, RSRPRXMdBm                      int
	PCCRXDRSSI, RSRPRXDdBm                      int
	TransmitPower                               int
	TAC, CellID                                 string
	RSRQdB, SINRdB                              float64

	state statusState
}

// statusState stores temporary state while parsing Status fields.
type statusState int

// Possible statusState values which indicate alternative parsing code paths.
const (
	_ statusState = iota
	rxmLast
	rxdLast
)

// Status returns the current status of the modem.
func (d *Device) Status() (*Status, error) {
	ss, err := d.d.Command("!GSTATUS?")
	if err != nil {
		return nil, err
	}
	if len(ss) == 0 {
		return nil, errors.New("atmodem: empty status response from modem")
	}

	return parseStatus(ss)
}

// parseStatus unpacks a Status structure from a modem response.
func parseStatus(lines []string) (*Status, error) {
	// TODO: consider a regex based parsing approach if it turns out the format
	// is more complex than anticipated.
	//
	// Example: https://regex101.com/r/DS6IIk/5, thanks @cockeys!
	var s Status
	for i, l := range lines {
		if i == 0 {
			// Skip the !GSTATUS: response header.
			continue
		}

		// For each line, determine the number of key/value pairs by checking
		// for colons and keeping track of their positions.
		var indices []int
		ss := strings.Fields(l)
		for i, s := range ss {
			if strings.HasSuffix(s, ":") {
				indices = append(indices, i)
			}
		}

		switch len(indices) {
		case 1:
			// Single key/value pair on one line, parse as-is.
			if err := s.parse(ss); err != nil {
				return nil, err
			}
		case 2:
			// Multiple key/value pairs on one line, assume that the format is:
			// "foo foo: bar    baz baz: qux corge"
			//
			// It seems that the first key/value pair on a line always has a
			// single value without any spaces, likely for ease of parsing
			// in another program.
			//
			// The second key/value pair can contain one or more words until
			// the end of the line.
			next := indices[0] + 2
			if err := s.parse(ss[:next]); err != nil {
				return nil, err
			}
			if err := s.parse(ss[next:]); err != nil {
				return nil, err
			}
		default:
			// We only handle lines with one or two key/value pairs.
			return nil, fmt.Errorf("atmodem: unexpected status response line with %d key/value pairs %q", len(indices), l)
		}
	}

	return &s, nil
}

// parse parses a key/value pair string slice into a field of Status.
func (s *Status) parse(ss []string) error {
	for i := range ss {
		if !strings.HasSuffix(ss[i], ":") {
			// Not a key/value pair.
			continue
		}

		// Advance the cursor and interpret the key/value pair as a string key
		// and slice of fields which may be parsed in different ways.
		i++
		k := strings.Join(ss[:i], " ")

		vp, err := newValueParser(ss[i:])
		if err != nil {
			return err
		}

		switch k {
		case "Current Time:":
			s.CurrentTime = time.Duration(vp.Int()) * time.Second
		case "Temperature:":
			s.Temperature = vp.Int()
		case "Reset Counter:":
			s.ResetCounter = vp.Int()
		case "Mode:":
			s.Mode = vp.String()
		case "System mode:":
			s.SystemMode = vp.String()
		case "PS state:":
			s.PSState = vp.String()
		case "LTE band:":
			s.LTEBand = vp.String()
		case "LTE bw:":
			s.LTEBandwidthMHz = vp.Float64()
		case "LTE Rx chan:":
			s.LTEReceiveChannel = vp.Int()
		case "LTE Tx chan:":
			s.LTETransmitChannel = vp.Int()
		case "LTE CA state:":
			s.LTECAState = vp.String()
		case "EMM state:":
			// TODO: consider parsing as state and substate fields.
			s.EMMState = vp.String()
		case "RRC state:":
			s.RRCState = vp.String()
		case "IMS reg state:":
			s.IMSRegState = vp.String()
		case "PCC RxM RSSI:":
			s.PCCRXMRSSI = vp.Int()
			s.state = rxmLast
		case "PCC RxD RSSI:":
			s.PCCRXDRSSI = vp.Int()
			s.state = rxdLast
		case "RSRP (dBm):":
			// This key is reused for multiple fields, so the value is parsed
			// into different struct fields depending on the previous parser
			// state.
			switch s.state {
			case rxmLast:
				s.RSRPRXMdBm = vp.Int()
			case rxdLast:
				s.RSRPRXDdBm = vp.Int()
			default:
				return fmt.Errorf("atmodem: cannot determine which RSRP dBm value is being parsed")
			}
		case "Tx Power:":
			s.TransmitPower = vp.Int()
		case "TAC:":
			s.TAC = vp.String()
		case "Cell ID:":
			s.CellID = vp.String()
		case "RSRQ (dB):":
			s.RSRQdB = vp.Float64()
		case "SINR (dB):":
			s.SINRdB = vp.Float64()
		default:
			// TODO!
		}

		if err := vp.Err(); err != nil {
			return err
		}
	}

	return nil
}
