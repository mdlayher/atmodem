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
			// Open a device using a simulated read/write/closer which returns
			// user input and captures command output.
			buf := bytes.NewBuffer(nil)
			d, err := atmodem.Open(&readWriteCloser{
				r:    strings.TrimSpace(tt.r),
				w:    buf,
				resC: make(chan string),
			}, 1*time.Second)
			if err != nil {
				t.Fatalf("failed to open device: %v", err)
			}
			defer d.Close()

			info, err := d.Info()
			if tt.ok && err != nil {
				t.Fatalf("failed to fetch info: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected an error, but none occurred")
			}
			if err != nil {
				t.Logf("err: %v", err)
				return
			}

			// Only expect info messages.
			if diff := cmp.Diff([]byte("ATI\r\n"), buf.Bytes()); diff != "" {
				t.Fatalf("unexpected modem info command (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tt.info, info); diff != "" {
				t.Fatalf("unexpected info (-want +got):\n%s", diff)
			}
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
