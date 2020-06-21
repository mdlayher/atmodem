package atmodem_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mdlayher/atmodem"
)

func TestDeviceInfo(t *testing.T) {
	tests := []struct {
		name string
		r    string
		info *atmodem.Info
		ok   bool
	}{
		{
			name: "empty",
			r:    "OK",
		},
		{
			name: "malformed",
			r: `
Manufacturer

OK`,
		},
		{
			name: "OK E173",
			// Example from: https://github.com/warthog618/modem#usage.
			r: `
Manufacturer: huawei
Model: E173
Revision: 21.017.09.00.314
IMEI: 1234567
+GCAP: +CGSM,+DS,+ES

OK`,
			info: &atmodem.Info{
				Manufacturer: "huawei",
				Model:        "E173",
				Revision:     "21.017.09.00.314",
				IMEI:         "1234567",
				GCAP:         []string{"+CGSM", "+DS", "+ES"},
			},
			ok: true,
		},
		{
			name: "OK MC7455",
			// Example from mdlayher's modem.
			r: `
Manufacturer: Sierra Wireless, Incorporated
Model: MC7455
Revision: SWI9X30C_02.33.03.00 r8209 CARMD-EV-FRMWR2 2019/08/28 20:59:30
MEID: 11111111111111
IMEI: 111111111111110
IMEI SV: 20
FSN: ABCDEF12345678
+GCAP: +CGSM

OK`,
			info: &atmodem.Info{
				Manufacturer: "Sierra Wireless, Incorporated",
				Model:        "MC7455",
				Revision:     "SWI9X30C_02.33.03.00 r8209 CARMD-EV-FRMWR2 2019/08/28 20:59:30",
				MEID:         "11111111111111",
				IMEI:         "111111111111110",
				IMEISV:       20,
				FSN:          "ABCDEF12345678",
				GCAP:         []string{"+CGSM"},
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withDevice(t, tt.r, tt.ok, []byte("ATI\r\n"), func(d *atmodem.Device) error {
				info, err := d.Info()
				if err != nil {
					return err
				}

				if diff := cmp.Diff(tt.info, info); diff != "" {
					t.Fatalf("unexpected info (-want +got):\n%s", diff)
				}

				return nil
			})
		})
	}
}

func withDevice(t *testing.T, res string, ok bool, commands []byte, fn func(d *atmodem.Device) error) {
	t.Helper()

	// Open a device using a simulated read/write/closer which returns
	// user input and captures command output.
	buf := bytes.NewBuffer(nil)
	d, err := atmodem.Open(&readWriteCloser{
		r:    strings.TrimSpace(res),
		w:    buf,
		resC: make(chan string),
	}, 1*time.Second)
	if err != nil {
		t.Fatalf("failed to open device: %v", err)
	}
	defer d.Close()

	err = fn(d)
	if ok && err != nil {
		t.Fatalf("failed to fetch info: %v", err)
	}
	if !ok && err == nil {
		t.Fatal("expected an error, but none occurred")
	}
	if err != nil {
		t.Logf("err: %v", err)
		return
	}

	if diff := cmp.Diff(commands, buf.Bytes()); diff != "" {
		t.Fatalf("unexpected modem commands (-want +got):\n%s", diff)
	}
}

func TestDeviceStatus(t *testing.T) {
	tests := []struct {
		name   string
		r      string
		status *atmodem.Status
		ok     bool
	}{
		{
			name: "empty",
			r:    "OK",
		},
		{
			name: "malformed no key/values",
			r: `
!GSTATUS:
foo

OK`,
		},
		{
			name: "malformed too many key/values",
			r: `
!GSTATUS:
foo: bar bar: baz baz: qux

OK`,
		},
		{
			name: "OK MC7455",
			r: `
!GSTATUS:
Current Time:  71465            Temperature: 41
Reset Counter: 8                Mode:        ONLINE
System mode:   LTE              PS state:    Attached
LTE band:      B12              LTE bw:      5 MHz
LTE Rx chan:   5035             LTE Tx chan: 23035
LTE CA state:  NOT ASSIGNED
EMM state:     Registered       Normal Service
RRC state:     RRC Idle
IMS reg state: No Srv

PCC RxM RSSI:  -84              RSRP (dBm):  -113
PCC RxD RSSI:  -84              RSRP (dBm):  -111
Tx Power:      --               TAC:         BEEF (12345)
RSRQ (dB):     -13.5            Cell ID:     DEADBEEF (1234567)
SINR (dB):      0.6


OK`,
			status: &atmodem.Status{
				CurrentTime: 19*time.Hour + 51*time.Minute + 5*time.Second,
				Temperature: 41,
				// TODO!
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withDevice(t, tt.r, tt.ok, []byte("AT!GSTATUS?\r\n"), func(d *atmodem.Device) error {
				status, err := d.Status()
				if err != nil {
					return err
				}

				if diff := cmp.Diff(tt.status, status); diff != "" {
					t.Fatalf("unexpected status (-want +got):\n%s", diff)
				}

				return nil
			})
		})
	}
}

var _ io.ReadWriteCloser = &readWriteCloser{}

type readWriteCloser struct {
	r      string
	w      *bytes.Buffer
	writes int

	resC chan string
}

func (rw *readWriteCloser) Read(b []byte) (int, error) {
	// The at package reads continuously so block until a response is sent due
	// to an incoming write.
	n := copy(b, []byte(<-rw.resC+"\r\n"))
	return n, nil
}

func (rw *readWriteCloser) Write(b []byte) (int, error) {
	defer func() { rw.writes++ }()

	// Consume the modem init messages and return an appropriate response if
	// necessary.
	switch rw.writes {
	case 0:
		if !bytes.Equal(b, []byte("\x1b\r\n\r\n")) {
			panicf("bad SMS escape command: %v", b)
		}

		return len(b), nil
	case 1:
		if !bytes.Equal(b, []byte("ATZ\r\n")) {
			panicf("bad AT clear command: %v", b)
		}

		rw.resC <- "OK"
		return len(b), nil
	case 2:
		if !bytes.Equal(b, []byte("ATE0\r\n")) {
			panicf("bad AT echo off command: %v", b)
		}

		rw.resC <- "OK"
		return len(b), nil
	default:
		// Otherwise capture the user's input and provide output.
		rw.resC <- rw.r
		return rw.w.Write(b)
	}

}

func (rw *readWriteCloser) Close() error { return nil }

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}
