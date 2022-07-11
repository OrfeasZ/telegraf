package powerdns_recursor

import (
	"fmt"
	"github.com/influxdata/telegraf"
	"net"
	"time"
)

// V3 (4.6.0+) Protocol:
// Standard unix stream socket
// Synchronous request / response
// Data structure:
// status: uint32
// dataLength: size_t
// data: byte[dataLength]
func (p *PowerdnsRecursor) gatherFromV3Server(address string, acc telegraf.Accumulator) error {
	conn, err := net.Dial("unix", address)
	if err != nil {
		return err
	}

	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(defaultTimeout)); err != nil {
		return err
	}

	// Write 4-byte response code.
	if _, err = conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return err
	}

	command := []byte("get-all")

	if _, err = writeNativeUIntToConn(conn, uint(len(command))); err != nil {
		return err
	}

	if _, err = conn.Write(command); err != nil {
		return err
	}

	// Now read the response.
	status := make([]byte, 4)
	n, err := conn.Read(status)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no status code received")
	}

	responseLength, err := readNativeUIntFromConn(conn)
	if err != nil {
		return err
	}
	if responseLength == 0 {
		return fmt.Errorf("received data length was 0")
	}

	data := make([]byte, responseLength)
	n, err = conn.Read(data)
	if err != nil {
		return err
	}
	if uint(n) != responseLength {
		return fmt.Errorf("no data received, expected '%v' bytes but got '%v'", responseLength, n)
	}

	// Process data
	metrics := string(data)
	fields := parseResponse(metrics)

	// Add server socket as a tag
	tags := map[string]string{"server": address}

	acc.AddFields("powerdns_recursor", fields, tags)

	return conn.Close()
}
