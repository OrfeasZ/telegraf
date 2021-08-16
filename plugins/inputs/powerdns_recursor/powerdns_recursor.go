package powerdns_recursor

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

type PowerdnsRecursor struct {
	UnixSockets        []string `toml:"unix_sockets"`
	SocketDir          string   `toml:"socket_dir"`
	SocketMode         string   `toml:"socket_mode"`
	NewControlProtocol bool     `toml:"new_control_protocol"`

	mode uint32
}

var defaultTimeout = 5 * time.Second

var sampleConfig = `
  ## Path to the Recursor control socket.
  unix_sockets = ["/var/run/pdns_recursor.controlsocket"]

  ## Directory to create receive socket.  This default is likely not writable,
  ## please reference the full plugin documentation for a recommended setup.
  # socket_dir = "/var/run/"

  ## Socket permissions for the receive socket.
  # socket_mode = "0666"

  ## IMPORTANT: Set this to true if you're running PowerDNS 4.5.0 or newer.
  # new_control_protocol = false
`

func (p *PowerdnsRecursor) SampleConfig() string {
	return sampleConfig
}

func (p *PowerdnsRecursor) Description() string {
	return "Read metrics from one or many PowerDNS Recursor servers"
}

func (p *PowerdnsRecursor) Init() error {
	if p.SocketMode != "" {
		mode, err := strconv.ParseUint(p.SocketMode, 8, 32)
		if err != nil {
			return fmt.Errorf("could not parse socket_mode: %v", err)
		}

		p.mode = uint32(mode)
	}
	return nil
}

func (p *PowerdnsRecursor) Gather(acc telegraf.Accumulator) error {
	if len(p.UnixSockets) == 0 {
		return p.gatherServer("/var/run/pdns_recursor.controlsocket", p.NewControlProtocol, acc)
	}

	for _, serverSocket := range p.UnixSockets {
		if err := p.gatherServer(serverSocket, p.NewControlProtocol, acc); err != nil {
			acc.AddError(err)
		}
	}

	return nil
}

func (p *PowerdnsRecursor) gatherServer(address string, newProtocol bool, acc telegraf.Accumulator) error {
	randomNumber := rand.Int63()
	recvSocket := filepath.Join("/", "var", "run", fmt.Sprintf("pdns_recursor_telegraf%d", randomNumber))
	if p.SocketDir != "" {
		recvSocket = filepath.Join(p.SocketDir, fmt.Sprintf("pdns_recursor_telegraf%d", randomNumber))
	}

	laddr, err := net.ResolveUnixAddr("unixgram", recvSocket)

	if err != nil {
		return err
	}

	defer os.Remove(recvSocket)

	raddr, err := net.ResolveUnixAddr("unixgram", address)

	if err != nil {
		return err
	}
	conn, err := net.DialUnix("unixgram", laddr, raddr)
	if err != nil {
		return err
	}
	if err := os.Chmod(recvSocket, os.FileMode(p.mode)); err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(defaultTimeout)); err != nil {
		return err
	}

	if newProtocol {
		// First send a 0 status code.
		_, err = conn.Write([]byte{0, 0, 0, 0})

		if err != nil {
			return err
		}
	}

	// Then send the get-all command.
	command := "get-all\n"

	if newProtocol {
		command = "get-all"
	}

	_, err = conn.Write([]byte(command))

	if err != nil {
		return err
	}

	if newProtocol {
		// Read the response status code.
		status := make([]byte, 4)
		n, err := conn.Read(status)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("no status code received")
		}
	}

	// Read the response data.
	buf := make([]byte, 16384)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("no data received")
	}

	metrics := string(buf)

	// Process data
	fields := parseResponse(metrics)

	// Add server socket as a tag
	tags := map[string]string{"server": address}

	acc.AddFields("powerdns_recursor", fields, tags)

	return conn.Close()
}

func parseResponse(metrics string) map[string]interface{} {
	values := make(map[string]interface{})

	s := strings.Split(metrics, "\n")

	for _, metric := range s[:len(s)-1] {
		m := strings.Split(metric, "\t")
		if len(m) < 2 {
			continue
		}

		i, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			log.Printf("E! [inputs.powerdns_recursor] error parsing integer for metric %q: %s",
				metric, err.Error())
			continue
		}
		values[m[0]] = i
	}

	return values
}

func init() {
	inputs.Add("powerdns_recursor", func() telegraf.Input {
		return &PowerdnsRecursor{
			mode: uint32(0666),
		}
	})
}
